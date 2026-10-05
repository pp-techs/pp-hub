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

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nhatbui/pp-hub/apps/server/internal/share"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

// Sharing by email and the routes an outsider uses, through the real handlers
// against a real Postgres (docs/architecture/personal-spaces.md).

type externalShareFixture struct {
	sharingFixture
	outsider store.User
	instance *bool
}

func newExternalShareFixture(t *testing.T, label string) (*Server, *store.Queries, externalShareFixture) {
	t.Helper()
	srv, q := sharingEnv(t)
	on := false
	srv.share.SetExternalSharing(func() bool { return on })
	f := externalShareFixture{
		sharingFixture: newSharingFixture(t, srv, q, label),
		outsider:       feedUser(t, q, label+"-outsider"),
		instance:       &on,
	}
	return srv, q, f
}

func setSpaceExternal(t *testing.T, q *store.Queries, sp store.Space, on bool) {
	t.Helper()
	if _, err := q.UpdateSpaceSettings(context.Background(), store.UpdateSpaceSettingsParams{
		ID: sp.ID, Name: sp.Name, Slug: sp.Slug, Description: sp.Description,
		DefaultAsAdmin: sp.DefaultAsAdmin, QuotaBytes: sp.QuotaBytes,
		TrashRetentionDays: sp.TrashRetentionDays, AllowPublicLinks: sp.AllowPublicLinks,
		ExternalSharing: on,
	}); err != nil {
		t.Fatalf("set external_sharing: %v", err)
	}
}

func shareBody(email, level string, canShare bool) string {
	return fmt.Sprintf(`{"kind":"direct","email":%q,"level":%q,"can_download":true,"can_share":%t}`, email, level, canShare)
}

func TestShareByEmail(t *testing.T) {
	srv, q, f := newExternalShareFixture(t, "byemail")
	post := func(body string) (int, map[string]any) {
		rec := call(t, srv.handleCreateShare, http.MethodPost, "/nodes/{nodeID}/shares",
			map[string]string{"nodeID": f.dir.ID.String()}, &f.owner, body)
		var out map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return rec.Code, out
	}

	// A member's address is an ordinary grant, whatever the switches say.
	if code, out := post(shareBody(strings.ToUpper(f.other.Email), "view", true)); code != http.StatusCreated || out["is_external"] == true {
		t.Fatalf("member by email: want 201 ordinary, got %d %v", code, out)
	}

	// Outside the space: both switches gate it.
	if code, _ := post(shareBody(f.outsider.Email, "view", false)); code != http.StatusForbidden {
		t.Fatalf("instance switch off: want 403, got %d", code)
	}
	*f.instance = true
	if code, _ := post(shareBody(f.outsider.Email, "view", false)); code != http.StatusForbidden {
		t.Fatalf("space switch off: want 403, got %d", code)
	}
	setSpaceExternal(t, q, f.space, true)
	if code, _ := post(shareBody(f.outsider.Email, "edit", true)); code != http.StatusForbidden {
		t.Fatalf("re-share for an outsider: want 403, got %d", code)
	}
	code, out := post(shareBody(f.outsider.Email, "edit", false))
	if code != http.StatusCreated || out["is_external"] != true {
		t.Fatalf("outsider by email: want 201 external, got %d %v", code, out)
	}
	if got, _ := srv.share.EffectiveCapsForNode(context.Background(), f.file, f.outsider.ID); !got.Edit || got.Share {
		t.Errorf("the outsider must edit and not re-share, got %+v", got)
	}

	// No account: an invitation, listed on the node and withdrawable.
	invitee := fmt.Sprintf("nobody-%d@example.test", os.Getpid())
	code, out = post(shareBody(invitee, "view", false))
	if code != http.StatusAccepted || out["email"] != invitee || out["mail_sent"] != false {
		t.Fatalf("unknown address: want 202 invitation, mail_sent false, got %d %v", code, out)
	}
	rec := call(t, srv.handleListPendingShares, http.MethodGet, "/nodes/{nodeID}/pending-shares",
		map[string]string{"nodeID": f.dir.ID.String()}, &f.owner, "")
	var pending []pendingShareDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &pending); err != nil || len(pending) != 1 {
		t.Fatalf("pending list: %d %s", rec.Code, rec.Body.String())
	}
	rec = call(t, srv.handleRevokePendingShare, http.MethodDelete, "/nodes/{nodeID}/pending-shares/{pendingID}",
		map[string]string{"nodeID": f.dir.ID.String(), "pendingID": pending[0].ID}, &f.owner, "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("revoke invitation: want 204, got %d %s", rec.Code, rec.Body.String())
	}

	if code, _ := post(shareBody("not an address", "view", false)); code != http.StatusBadRequest {
		t.Errorf("garbage address: want 400, got %d", code)
	}
	if code, _ := post(shareBody(f.owner.Email, "view", false)); code != http.StatusBadRequest {
		t.Errorf("own address: want 400, got %d", code)
	}
}

// The budget counts every new outsider handed out today, revoked or not:
// revoking does not refund it. Changing an existing invitation is not a new
// one.
func TestInvitationBudget(t *testing.T) {
	srv, q, f := newExternalShareFixture(t, "budget")
	*f.instance = true
	setSpaceExternal(t, q, f.space, true)
	nodeParam := map[string]string{"nodeID": f.dir.ID.String()}
	invite := func(email, level string) *httptest.ResponseRecorder {
		return call(t, srv.handleCreateShare, http.MethodPost, "/nodes/{nodeID}/shares", nodeParam, &f.owner,
			shareBody(email, level, false))
	}
	addr := func(i int) string { return fmt.Sprintf("budget-%d-%d@example.test", os.Getpid(), i) }
	var ids []string
	for i := range share.InvitationsPerDay {
		rec := invite(addr(i), "view")
		var p pendingShareDTO
		if rec.Code != http.StatusAccepted || json.Unmarshal(rec.Body.Bytes(), &p) != nil {
			t.Fatalf("invitation %d: %d %s", i, rec.Code, rec.Body.String())
		}
		ids = append(ids, p.ID)
	}
	// Withdraw all but the first: the budget must not come back.
	for _, id := range ids[1:] {
		rec := call(t, srv.handleRevokePendingShare, http.MethodDelete, "/nodes/{nodeID}/pending-shares/{pendingID}",
			map[string]string{"nodeID": f.dir.ID.String(), "pendingID": id}, &f.owner, "")
		if rec.Code != http.StatusNoContent {
			t.Fatalf("revoke: %d", rec.Code)
		}
	}
	if rec := invite(addr(1), "view"); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("re-inviting a revoked address over budget: want 429, got %d %s", rec.Code, rec.Body.String())
	}
	if rec := invite(f.outsider.Email, "view"); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("an external grant over budget: want 429, got %d %s", rec.Code, rec.Body.String())
	}
	if rec := invite(addr(0), "edit"); rec.Code != http.StatusAccepted {
		t.Fatalf("changing a live invitation over budget: want 202, got %d %s", rec.Code, rec.Body.String())
	}
}

// Changing an invitation takes covering it, like changing a grant: a sharer
// who may only hand out view cannot rewrite an owner's edit invitation.
func TestReinviteNeedsToCoverTheInvitation(t *testing.T) {
	srv, q, f := newExternalShareFixture(t, "cover")
	*f.instance = true
	setSpaceExternal(t, q, f.space, true)
	grantDirect(t, srv, f.dir, f.other, f.owner, share.GrantCaps{Level: share.LevelView, CanShare: true})
	email := fmt.Sprintf("cover-%d@example.test", os.Getpid())
	nodeParam := map[string]string{"nodeID": f.dir.ID.String()}
	if rec := call(t, srv.handleCreateShare, http.MethodPost, "/nodes/{nodeID}/shares", nodeParam, &f.owner,
		shareBody(email, "edit", false)); rec.Code != http.StatusAccepted {
		t.Fatalf("owner invites: %d %s", rec.Code, rec.Body.String())
	}
	rec := call(t, srv.handleCreateShare, http.MethodPost, "/nodes/{nodeID}/shares", nodeParam, &f.other,
		`{"kind":"direct","email":"`+email+`","level":"view","can_download":false,"can_share":false,"expires_in":1}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("a narrower sharer rewriting the invitation: want 403, got %d %s", rec.Code, rec.Body.String())
	}
}

// An account whose address the identity provider never verified is not who
// an email names: sharing with that address makes an invitation, adopted by
// whoever signs in with it verified.
func TestShareByEmailIgnoresUnverifiedAccounts(t *testing.T) {
	srv, q, f := newExternalShareFixture(t, "unverified")
	*f.instance = true
	setSpaceExternal(t, q, f.space, true)
	squatter, err := q.CreateUser(context.Background(), store.CreateUserParams{
		OidcSub: fmt.Sprintf("squat-%d", os.Getpid()), OidcIss: "test",
		Email: fmt.Sprintf("victim-%d@example.test", os.Getpid()), Name: "squatter",
	})
	if err != nil {
		t.Fatalf("create squatter: %v", err)
	}
	rec := call(t, srv.handleCreateShare, http.MethodPost, "/nodes/{nodeID}/shares",
		map[string]string{"nodeID": f.dir.ID.String()}, &f.owner, shareBody(squatter.Email, "view", false))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("unverified address: want 202 invitation, got %d %s", rec.Code, rec.Body.String())
	}
	if got, _ := srv.share.EffectiveCapsForNode(context.Background(), f.file, squatter.ID); got.View {
		t.Errorf("the unverified account must reach nothing, got %+v", got)
	}
}

// Shared one file deep in a tree, an outsider learns nothing about what sits
// above it: not who was granted the folder, not the folder's name.
func TestOutsiderLearnsNothingAboveTheShare(t *testing.T) {
	srv, q, f := newExternalShareFixture(t, "above")
	*f.instance = true
	setSpaceExternal(t, q, f.space, true)
	ctx := context.Background()
	grantDirect(t, srv, f.dir, f.other, f.owner, share.GrantCaps{Level: share.LevelView})
	if _, err := srv.share.UpsertDirect(ctx, f.file.ID, f.outsider.ID,
		share.GrantCaps{Level: share.LevelView}, pgtype.Timestamptz{}, f.owner.ID, true); err != nil {
		t.Fatalf("grant: %v", err)
	}

	rec := call(t, srv.handleNodePeople, http.MethodGet, "/nodes/{nodeID}/people",
		map[string]string{"nodeID": f.file.ID.String()}, &f.outsider, "")
	var people []nodePersonDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &people); err != nil {
		t.Fatalf("people: %d %s", rec.Code, rec.Body.String())
	}
	for _, p := range people {
		if p.UserID == f.other.ID.String() {
			t.Errorf("a grantee of the folder above the share must not be listed: %+v", people)
		}
	}

	search := func(w http.ResponseWriter, r *http.Request) {
		r.URL.RawQuery = "q=" + f.file.Name
		srv.handleSearch(w, r)
	}
	rec = call(t, search, http.MethodGet, "/search", nil, &f.outsider, "")
	var hits []searchHitDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &hits); err != nil || len(hits) != 1 {
		t.Fatalf("search: %d %s", rec.Code, rec.Body.String())
	}
	if hits[0].ParentName != "" {
		t.Errorf("the folder around the share must not be named, got %q", hits[0].ParentName)
	}
}

// An outsider browses a shared folder by node, sees a breadcrumb that starts
// at the share and lists it among "shared from outside" — and sees the people
// the document was opened to, never the company's roster.
func TestOutsiderRoutes(t *testing.T) {
	srv, q, f := newExternalShareFixture(t, "outsider")
	*f.instance = true
	setSpaceExternal(t, q, f.space, true)
	if _, err := srv.share.UpsertDirect(context.Background(), f.dir.ID, f.outsider.ID,
		share.GrantCaps{Level: share.LevelView}, pgtype.Timestamptz{}, f.owner.ID, true); err != nil {
		t.Fatalf("grant: %v", err)
	}
	nodeParam := func(n store.Node) map[string]string { return map[string]string{"nodeID": n.ID.String()} }

	rec := call(t, srv.handleListNodeChildren, http.MethodGet, "/nodes/{nodeID}/children", nodeParam(f.dir), &f.outsider, "")
	var kids []nodeDTO
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &kids) != nil || len(kids) != 1 || kids[0].ID != f.file.ID.String() {
		t.Fatalf("children by node: %d %s", rec.Code, rec.Body.String())
	}

	rec = call(t, srv.handleNodePath, http.MethodGet, "/nodes/{nodeID}/path", nodeParam(f.file), &f.outsider, "")
	var path []pathEntryDTO
	if json.Unmarshal(rec.Body.Bytes(), &path) != nil || len(path) != 2 || path[0].ID != f.dir.ID.String() || path[1].ID != f.file.ID.String() {
		t.Fatalf("path must start at the share root: %d %s", rec.Code, rec.Body.String())
	}

	external := func(w http.ResponseWriter, r *http.Request) {
		r.URL.RawQuery = "scope=external"
		srv.handleListShared(w, r)
	}
	rec = call(t, external, http.MethodGet, "/shared", nil, &f.outsider, "")
	var shared []nodeDTO
	if json.Unmarshal(rec.Body.Bytes(), &shared) != nil || len(shared) != 1 || shared[0].ID != f.dir.ID.String() || shared[0].SpaceName != f.space.Name {
		t.Fatalf("shared from outside: %d %s", rec.Code, rec.Body.String())
	}

	rec = call(t, srv.handleNodePeople, http.MethodGet, "/nodes/{nodeID}/people", nodeParam(f.file), &f.outsider, "")
	var people []nodePersonDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &people); err != nil {
		t.Fatalf("people: %d %s", rec.Code, rec.Body.String())
	}
	seen := map[string]bool{}
	for _, p := range people {
		seen[p.UserID] = true
	}
	if !seen[f.owner.ID.String()] || !seen[f.outsider.ID.String()] || seen[f.other.ID.String()] {
		t.Errorf("an outsider sees the document's people, not the roster: %+v", people)
	}
	rec = call(t, srv.handleNodePeople, http.MethodGet, "/nodes/{nodeID}/people", nodeParam(f.file), &f.owner, "")
	people = nil
	_ = json.Unmarshal(rec.Body.Bytes(), &people)
	seen = map[string]bool{}
	for _, p := range people {
		seen[p.UserID] = true
	}
	if !seen[f.other.ID.String()] || !seen[f.outsider.ID.String()] {
		t.Errorf("a member sees the roster plus the outsider: %+v", people)
	}

	// The space's own listing stays a member's route.
	rec = call(t, srv.handleListChildren, http.MethodGet, "/spaces/{spaceID}/children",
		map[string]string{"spaceID": f.space.ID.String()}, &f.outsider, "")
	if rec.Code != http.StatusForbidden {
		t.Errorf("space listing for an outsider: want 403, got %d", rec.Code)
	}
}

// A personal space has no members, groups, organization or deletion to
// administer, and its owner does not set its quota.
func TestPersonalSpaceRefusesAdministration(t *testing.T) {
	srv, q := sharingEnv(t)
	ctx := context.Background()
	owner := feedUser(t, q, "personal-admin")
	sp, _, err := srv.spaces.EnsurePersonal(ctx, owner, 1<<20)
	if err != nil {
		t.Fatalf("ensure personal: %v", err)
	}
	t.Cleanup(func() { _ = q.DeleteSpace(ctx, sp.ID) })
	params := map[string]string{"spaceID": sp.ID.String()}

	for name, h := range map[string]struct {
		fn      http.HandlerFunc
		method  string
		pattern string
		body    string
	}{
		"create group":  {srv.handleCreateGroup, http.MethodPost, "/spaces/{spaceID}/groups", `{"name":"x"}`},
		"delete space":  {srv.handleDeleteSpace, http.MethodDelete, "/spaces/{spaceID}", ""},
		"team folder":   {srv.handleCreateTeamFolder, http.MethodPost, "/spaces/{spaceID}/team-folders", `{"name":"x"}`},
		"departures":    {srv.handleListDepartures, http.MethodGet, "/spaces/{spaceID}/departures", ""},
		"raise quota":   {srv.handleUpdateSpace, http.MethodPatch, "/spaces/{spaceID}", `{"quota_bytes":0}`},
		"close sharing": {srv.handleUpdateSpace, http.MethodPatch, "/spaces/{spaceID}", `{"external_sharing":false}`},
	} {
		if rec := call(t, h.fn, h.method, h.pattern, params, &owner, h.body); rec.Code != http.StatusConflict {
			t.Errorf("%s: want 409, got %d %s", name, rec.Code, rec.Body.String())
		}
	}
	// What the owner does decide still works, and re-sending a field unchanged
	// is not a change.
	rec := call(t, srv.handleUpdateSpace, http.MethodPatch, "/spaces/{spaceID}", params, &owner,
		fmt.Sprintf(`{"description":"mine","quota_bytes":%d}`, 1<<20))
	if rec.Code != http.StatusOK {
		t.Errorf("description: want 200, got %d %s", rec.Code, rec.Body.String())
	}
}
