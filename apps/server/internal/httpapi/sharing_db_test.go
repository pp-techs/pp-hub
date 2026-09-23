package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nhatbui/pp-hub/apps/server/internal/audit"
	"github.com/nhatbui/pp-hub/apps/server/internal/auth"
	"github.com/nhatbui/pp-hub/apps/server/internal/groups"
	"github.com/nhatbui/pp-hub/apps/server/internal/share"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

// Who may move, re-share and revoke is decided in the handlers, on top of a
// resolver that is a recursive CTE — so these run the real handlers against a
// real Postgres, or skip loudly like the rest of this package's database
// tests:
//
//	PP_TEST_DATABASE_URL=postgres://pphub:pphub@localhost:5433/pphub go test ./internal/httpapi/

func sharingEnv(t *testing.T) (*Server, *store.Queries) {
	t.Helper()
	srv, q := feedEnv(t)
	srv.groups = groups.NewService(q)
	srv.audit = audit.New(q)
	return srv, q
}

// call drives one request through a chi router carrying pattern, so path
// params resolve as in production. u nil means an anonymous request.
func call(t *testing.T, h http.HandlerFunc, method, pattern string, params map[string]string,
	u *store.User, body string) *httptest.ResponseRecorder {
	t.Helper()
	path := pattern
	for k, v := range params {
		path = strings.Replace(path, "{"+k+"}", v, 1)
	}
	r := chi.NewRouter()
	r.Method(method, pattern, h)
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if u != nil {
		req = req.WithContext(context.WithValue(req.Context(), sessionCtxKey, &auth.Session{ID: u.ID, User: *u}))
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

type sharingFixture struct {
	space store.Space
	owner store.User
	other store.User
	dir   store.Node // owned by owner
	file  store.Node // owned by owner, inside dir
}

func newSharingFixture(t *testing.T, srv *Server, q *store.Queries, label string) sharingFixture {
	t.Helper()
	ctx := context.Background()
	owner := feedUser(t, q, label+"-owner")
	other := feedUser(t, q, label+"-other")
	sp, err := srv.spaces.Create(ctx, fmt.Sprintf("sharing-%s-%d", label, os.Getpid()), owner.ID)
	if err != nil {
		t.Fatalf("create space: %v", err)
	}
	t.Cleanup(func() { _ = q.DeleteSpace(ctx, sp.ID) })
	if err := srv.spaces.AddMember(ctx, sp.ID, other.ID, false); err != nil {
		t.Fatalf("add member: %v", err)
	}
	dir := mkNode(t, q, sp, pgtype.UUID{}, store.NodeKindDir, label+"-dir", owner)
	file := mkNode(t, q, sp, dir.ID, store.NodeKindFile, label+".txt", owner)
	return sharingFixture{space: sp, owner: owner, other: other, dir: dir, file: file}
}

func mkNode(t *testing.T, q *store.Queries, sp store.Space, parent pgtype.UUID, kind store.NodeKind, name string, by store.User) store.Node {
	t.Helper()
	n, err := q.CreateNode(context.Background(), store.CreateNodeParams{
		SpaceID: sp.ID, ParentID: parent, Kind: kind, Name: name, CreatedBy: by.ID,
	})
	if err != nil {
		t.Fatalf("create %s: %v", name, err)
	}
	return n
}

func grantDirect(t *testing.T, srv *Server, node store.Node, to, by store.User, caps share.GrantCaps) store.Share {
	t.Helper()
	sh, err := srv.share.UpsertDirect(context.Background(), node.ID, to.ID, caps, pgtype.Timestamptz{}, by.ID)
	if err != nil {
		t.Fatalf("grant: %v", err)
	}
	return sh
}

func moveTo(t *testing.T, srv *Server, u store.User, node store.Node, parent string) int {
	t.Helper()
	return call(t, srv.handleMoveNode, "PATCH", "/nodes/{nodeID}/move",
		map[string]string{"nodeID": node.ID.String()}, &u, `{"parent_id":"`+parent+`"}`).Code
}

// An editor denied re-sharing must not be able to become the owner by
// dropping the file into a folder they own: ownership inherits down the tree,
// so that move handed them every capability, public links included.
func TestMoveCannotWidenTheMoversOwnAccess(t *testing.T) {
	srv, q := sharingEnv(t)
	f := newSharingFixture(t, srv, q, "widen")
	grantDirect(t, srv, f.dir, f.other, f.owner, share.GrantCaps{Level: share.LevelEdit})
	mine := mkNode(t, q, f.space, pgtype.UUID{}, store.NodeKindDir, "mine", f.other)
	sub := mkNode(t, q, f.space, f.dir.ID, store.NodeKindDir, "sub", f.owner)

	if code := moveTo(t, srv, f.other, f.file, mine.ID.String()); code != http.StatusForbidden {
		t.Errorf("editor moving the owner's file into their own folder = %d, want 403", code)
	}
	if got, _ := srv.share.EffectiveCapsForNode(context.Background(), f.file, f.other.ID); got.Owner || got.Share {
		t.Errorf("a refused move must leave the editor's caps alone, got %+v", got)
	}
	// Taking it out to the top level changes who reaches it (the owner's
	// folder no longer does), which an editor without share may not decide.
	if code := moveTo(t, srv, f.other, f.file, ""); code != http.StatusForbidden {
		t.Errorf("editor moving the owner's file out of the shared folder = %d, want 403", code)
	}
	// Reorganising inside the folder changes nobody's access: allowed.
	if code := moveTo(t, srv, f.other, f.file, sub.ID.String()); code != http.StatusOK {
		t.Errorf("editor moving within the shared folder = %d, want 200", code)
	}
	// The owner takes their own file wherever they like.
	moved, _ := srv.fs.Get(context.Background(), f.file.ID)
	if code := moveTo(t, srv, f.owner, moved, ""); code != http.StatusOK {
		t.Errorf("owner moving their own file = %d, want 200", code)
	}
}

// Inside a team folder `created_by` grants nothing, so moving a personal file
// in takes it away from whoever made it. Only its creator may do that.
func TestMoveIntoTeamFolderNeedsTheCreator(t *testing.T) {
	srv, q := sharingEnv(t)
	f := newSharingFixture(t, srv, q, "donate")
	grantDirect(t, srv, f.dir, f.other, f.owner, share.GrantCaps{Level: share.LevelEdit, CanShare: true})
	team, err := srv.fs.CreateTeamFolder(context.Background(), f.space.ID, "donate-team", f.other.ID)
	if err != nil {
		t.Fatalf("team folder: %v", err)
	}

	if code := moveTo(t, srv, f.other, f.file, team.ID.String()); code != http.StatusForbidden {
		t.Errorf("moving someone else's file into a team folder = %d, want 403", code)
	}
	if got, _ := srv.share.EffectiveCapsForNode(context.Background(), f.file, f.owner.ID); !got.Owner {
		t.Errorf("the owner must keep their file, got %+v", got)
	}
	// A folder holding a colleague's work cannot be donated by its owner
	// either: the colleague would lose what they made.
	mkNode(t, q, f.space, f.dir.ID, store.NodeKindFile, "theirs.txt", f.other)
	grantDirect(t, srv, team, f.owner, f.other, share.GrantCaps{Level: share.LevelEdit})
	if code := moveTo(t, srv, f.owner, f.dir, team.ID.String()); code != http.StatusForbidden {
		t.Errorf("donating a folder with foreign content = %d, want 403", code)
	}
	if code := moveTo(t, srv, f.owner, f.file, team.ID.String()); code != http.StatusOK {
		t.Errorf("the creator donating their own file = %d, want 200", code)
	}
}

// Monotonicity bounds taking away as well as handing out: a viewer allowed to
// re-share must not strip or narrow an editor the owner appointed.
func TestGrantChangesAreBoundedByTheCallersOwnAccess(t *testing.T) {
	srv, q := sharingEnv(t)
	f := newSharingFixture(t, srv, q, "bound")
	editor := feedUser(t, q, "bound-editor")
	if err := srv.spaces.AddMember(context.Background(), f.space.ID, editor.ID, false); err != nil {
		t.Fatalf("add member: %v", err)
	}
	grantDirect(t, srv, f.dir, f.other, f.owner, share.GrantCaps{Level: share.LevelView, CanShare: true})
	edit := grantDirect(t, srv, f.dir, editor, f.owner, share.GrantCaps{Level: share.LevelEdit})
	params := map[string]string{"nodeID": f.dir.ID.String(), "shareID": edit.ID.String()}

	if rec := call(t, srv.handleRevokeShare, "DELETE", "/nodes/{nodeID}/shares/{shareID}", params, &f.other, ""); rec.Code != http.StatusForbidden {
		t.Errorf("viewer revoking an editor's grant = %d, want 403", rec.Code)
	}
	body := `{"kind":"direct","user_id":"` + editor.ID.String() + `","level":"view"}`
	if rec := call(t, srv.handleCreateShare, "POST", "/nodes/{nodeID}/shares", params, &f.other, body); rec.Code != http.StatusForbidden {
		t.Errorf("viewer narrowing an editor's grant = %d, want 403", rec.Code)
	}

	// Re-sharing is an update, not a second row: the owner narrows the
	// editor to view and exactly one grant remains, at view.
	rec := call(t, srv.handleCreateShare, "POST", "/nodes/{nodeID}/shares", params, &f.owner, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("owner re-sharing = %d (%s), want 200", rec.Code, rec.Body)
	}
	rows, _ := srv.share.ListForNode(context.Background(), f.dir.ID)
	count := 0
	for _, sh := range rows {
		if sh.GranteeUserID == editor.ID {
			count++
			if sh.Level != share.LevelView {
				t.Errorf("re-sharing must replace the grant, got level %s", sh.Level)
			}
		}
	}
	if count != 1 {
		t.Errorf("re-sharing must leave one row per person, got %d", count)
	}

	// Same bound on group grants — a group the viewer is not in, or the grant
	// would widen the viewer too and they would cover it.
	editors, err := srv.groups.Create(context.Background(), f.space.ID, "Editors", "", f.owner.ID)
	if err != nil {
		t.Fatalf("group: %v", err)
	}
	if _, err := srv.groups.Grant(context.Background(), f.dir, editors.ID, share.GrantCaps{Level: share.LevelEdit}, f.owner.ID); err != nil {
		t.Fatalf("group grant: %v", err)
	}
	gp := map[string]string{"nodeID": f.dir.ID.String(), "groupID": editors.ID.String()}
	if rec := call(t, srv.handleRevokeGrant, "DELETE", "/nodes/{nodeID}/grants/{groupID}", gp, &f.other, ""); rec.Code != http.StatusForbidden {
		t.Errorf("viewer revoking a group's edit grant = %d, want 403", rec.Code)
	}
	if rec := call(t, srv.handleRevokeGrant, "DELETE", "/nodes/{nodeID}/grants/{groupID}", gp, &f.owner, ""); rec.Code != http.StatusNoContent {
		t.Errorf("owner revoking the group grant = %d, want 204", rec.Code)
	}
	if rec := call(t, srv.handleRevokeGrant, "DELETE", "/nodes/{nodeID}/grants/{groupID}", gp, &f.owner, ""); rec.Code != http.StatusNotFound {
		t.Errorf("revoking a grant that is not there = %d, want 404", rec.Code)
	}
}

// A team folder with nobody holding can_share is a dead end: no person owns
// team content, so nobody could appoint anyone.
func TestTeamFolderAlwaysKeepsAManager(t *testing.T) {
	srv, q := sharingEnv(t)
	f := newSharingFixture(t, srv, q, "manager")
	team, err := srv.fs.CreateTeamFolder(context.Background(), f.space.ID, "manager-team", f.owner.ID)
	if err != nil {
		t.Fatalf("team folder: %v", err)
	}
	seat, _, err := srv.share.DirectFor(context.Background(), team.ID, f.owner.ID)
	if err != nil {
		t.Fatalf("seed grant: %v", err)
	}
	params := map[string]string{"nodeID": team.ID.String(), "shareID": seat.ID.String()}

	if rec := call(t, srv.handleRevokeShare, "DELETE", "/nodes/{nodeID}/shares/{shareID}", params, &f.owner, ""); rec.Code != http.StatusConflict {
		t.Errorf("removing the only manager = %d, want 409", rec.Code)
	}
	grantDirect(t, srv, team, f.other, f.owner, share.GrantCaps{Level: share.LevelEdit, CanShare: true})
	if rec := call(t, srv.handleRevokeShare, "DELETE", "/nodes/{nodeID}/shares/{shareID}", params, &f.owner, ""); rec.Code != http.StatusNoContent {
		t.Errorf("removing a manager when another remains = %d, want 204", rec.Code)
	}
}

// A link is never wider than its creator's access today: narrowed to
// view-only, they must not keep handing the bytes out anonymously.
func TestLinkDownloadFollowsItsCreatorsAccess(t *testing.T) {
	srv, q := sharingEnv(t)
	f := newSharingFixture(t, srv, q, "link")
	grantDirect(t, srv, f.dir, f.other, f.owner, share.GrantCaps{Level: share.LevelView, CanDownload: true, CanShare: true})
	_, token, err := srv.share.CreateLink(context.Background(), f.file.ID, true, pgtype.Timestamptz{}, f.other.ID)
	if err != nil {
		t.Fatalf("link: %v", err)
	}
	info := func() (int, bool) {
		rec := call(t, srv.handleShareInfo, "GET", "/s/{token}", map[string]string{"token": token}, nil, "")
		var out struct {
			CanDownload bool `json:"can_download"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return rec.Code, out.CanDownload
	}
	if code, dl := info(); code != http.StatusOK || !dl {
		t.Fatalf("fresh link = (%d, download %v), want (200, true)", code, dl)
	}
	grantDirect(t, srv, f.dir, f.other, f.owner, share.GrantCaps{Level: share.LevelView, CanShare: true})
	if code, dl := info(); code != http.StatusOK || dl {
		t.Errorf("after the creator lost download = (%d, download %v), want (200, false)", code, dl)
	}
	rec := call(t, srv.handleShareDownload, "GET", "/s/{token}/download", map[string]string{"token": token}, nil, "")
	if rec.Code != http.StatusForbidden {
		t.Errorf("download through the clamped link = %d, want 403", rec.Code)
	}
}

// Being an admin never grants file access, and group membership is admin-only
// — so an admin must not seat themselves in a group that opens files.
func TestAdminCannotJoinAGroupThatOpensFiles(t *testing.T) {
	srv, q := sharingEnv(t)
	f := newSharingFixture(t, srv, q, "selfadd")
	ctx := context.Background()
	g, err := srv.groups.Create(ctx, f.space.ID, "Finance", "", f.owner.ID)
	if err != nil {
		t.Fatalf("group: %v", err)
	}
	private := mkNode(t, q, f.space, pgtype.UUID{}, store.NodeKindDir, "payroll", f.other)
	if _, err := srv.groups.Grant(ctx, private, g.ID, share.GrantCaps{Level: share.LevelView}, f.other.ID); err != nil {
		t.Fatalf("grant: %v", err)
	}
	params := map[string]string{"spaceID": f.space.ID.String(), "groupID": g.ID.String()}
	add := func(actor, who store.User) int {
		return call(t, srv.handleAddGroupMember, "POST", "/spaces/{spaceID}/groups/{groupID}/members",
			params, &actor, `{"user_id":"`+who.ID.String()+`"}`).Code
	}
	if code := add(f.owner, f.owner); code != http.StatusForbidden {
		t.Errorf("admin adding themselves to a group with grants = %d, want 403", code)
	}
	if got, _ := srv.share.EffectiveCapsForNode(ctx, private, f.owner.ID); got.View {
		t.Errorf("the admin must still reach nothing, got %+v", got)
	}
	if code := add(f.owner, f.other); code != http.StatusCreated {
		t.Errorf("admin adding somebody else = %d, want 201", code)
	}
}
