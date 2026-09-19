package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nhatbui/pp-hub/apps/server/internal/store"
	"github.com/nhatbui/pp-hub/apps/server/internal/wopi"
)

// A WOPI access token is a bearer credential with a ten-hour life that
// OnlyOffice holds and replays, and the rank inside it was resolved when the
// editor opened. Whether revoking access actually reaches an open editor is
// therefore a question about live grant resolution, which is a recursive CTE —
// it cannot be asserted against a fake. Same contract as the rest of this
// package's database tests: a real Postgres or a loud skip.
//
//	PP_TEST_DATABASE_URL=postgres://pphub:pphub@localhost:5433/pphub go test ./internal/httpapi/
func wopiEnv(t *testing.T) (*Server, *store.Queries) {
	t.Helper()
	srv, q := feedEnv(t)
	srv.wopi = wopi.NewService(q, []byte("wopi-test-key-32-bytes-long!!!!!"))
	return srv, q
}

// wopiRequest drives one WOPI call through the real router chain, so the
// {nodeID} path param resolves exactly as it does in production.
func wopiRequest(t *testing.T, h http.HandlerFunc, method, nodeID, token string) *httptest.ResponseRecorder {
	t.Helper()
	r := chi.NewRouter()
	r.Method(method, "/wopi/files/{nodeID}", h)
	req := httptest.NewRequest(method, "/wopi/files/"+nodeID+"?access_token="+token, nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

// wopiFixture: a file owned by one person, shared for editing with another,
// who holds a rank-3 token for it.
type wopiFixture struct {
	space  store.Space
	file   store.Node
	editor store.User
	token  string
}

func newWopiFixture(t *testing.T, srv *Server, q *store.Queries, label string) wopiFixture {
	t.Helper()
	ctx := context.Background()

	owner := feedUser(t, q, label+"-owner")
	editor := feedUser(t, q, label+"-editor")

	sp, err := srv.spaces.Create(ctx, fmt.Sprintf("wopi-%s-%d", label, os.Getpid()), owner.ID)
	if err != nil {
		t.Fatalf("create space: %v", err)
	}
	t.Cleanup(func() { _ = q.DeleteSpace(ctx, sp.ID) })
	if err := srv.spaces.AddMember(ctx, sp.ID, editor.ID, false); err != nil {
		t.Fatalf("add member: %v", err)
	}

	file, err := q.CreateNode(ctx, store.CreateNodeParams{
		SpaceID: sp.ID, Kind: store.NodeKindFile, Name: label + ".docx", CreatedBy: owner.ID,
	})
	if err != nil {
		t.Fatalf("create file: %v", err)
	}
	// CheckFileInfo reports the current version, so the fixture needs one.
	// No blob is written: nothing here reads the bytes.
	ver, err := q.AddVersion(ctx, store.AddVersionParams{
		NodeID: file.ID, BlobHash: fmt.Sprintf("test-%s-%d", label, os.Getpid()), Size: 11,
		Mime:      "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		CreatedBy: owner.ID,
	})
	if err != nil {
		t.Fatalf("add version: %v", err)
	}
	if err := q.SetCurrentVersion(ctx, store.SetCurrentVersionParams{ID: file.ID, CurrentVersionID: ver.ID}); err != nil {
		t.Fatalf("set current version: %v", err)
	}
	if _, err := q.CreateShare(ctx, store.CreateShareParams{
		NodeID: file.ID, Kind: store.ShareKindDirect, GranteeUserID: editor.ID,
		Level: store.ShareLevelEdit, CanDownload: true, CreatedBy: owner.ID,
	}); err != nil {
		t.Fatalf("grant edit: %v", err)
	}

	token, err := srv.wopi.SignToken(editor.ID, file.ID, 3)
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return wopiFixture{space: sp, file: file, editor: editor, token: token}
}

func nodeIDString(t *testing.T, id pgtype.UUID) string {
	t.Helper()
	v, err := id.Value()
	if err != nil {
		t.Fatal(err)
	}
	return v.(string)
}

func revokeShares(t *testing.T, q *store.Queries, nodeID pgtype.UUID) {
	t.Helper()
	ctx := context.Background()
	rows, err := q.ListSharesForNode(ctx, nodeID)
	if err != nil {
		t.Fatalf("list shares: %v", err)
	}
	for _, sh := range rows {
		if err := q.DeleteShare(ctx, sh.ID); err != nil {
			t.Fatalf("delete share: %v", err)
		}
	}
}

// The whole point of re-resolving: revoking the grant has to stop the save,
// not wait out the token's ten hours.
func TestWopiWriteStopsWhenTheGrantIsRevoked(t *testing.T) {
	srv, q := wopiEnv(t)
	f := newWopiFixture(t, srv, q, "revoke")
	id := nodeIDString(t, f.file.ID)

	if rec := wopiRequest(t, srv.handleWopiCheckFileInfo, "GET", id, f.token); rec.Code != http.StatusOK {
		t.Fatalf("editor could not open the file while granted: %d %s", rec.Code, rec.Body)
	}

	revokeShares(t, q, f.file.ID)

	rec := wopiRequest(t, srv.handleWopiLockOps, "POST", id, f.token)
	if rec.Code != http.StatusForbidden {
		t.Errorf("lock after revoke = %d, want 403 (token rank was trusted over live access)", rec.Code)
	}
	rec = wopiRequest(t, srv.handleWopiCheckFileInfo, "GET", id, f.token)
	if rec.Code != http.StatusForbidden {
		t.Errorf("CheckFileInfo after revoke = %d, want 403", rec.Code)
	}
}

// Leaving the space ends the reach, grant or no grant — M15's rule has to hold
// on this path too, not only on /api/v1.
func TestWopiAccessEndsWithSpaceMembership(t *testing.T) {
	srv, q := wopiEnv(t)
	ctx := context.Background()
	f := newWopiFixture(t, srv, q, "departed")
	id := nodeIDString(t, f.file.ID)

	if err := q.RemoveSpaceMember(ctx, store.RemoveSpaceMemberParams{
		SpaceID: f.space.ID, UserID: f.editor.ID,
	}); err != nil {
		t.Fatalf("remove member: %v", err)
	}

	if rec := wopiRequest(t, srv.handleWopiGetFile, "GET", id, f.token); rec.Code != http.StatusForbidden {
		t.Errorf("read after leaving the space = %d, want 403", rec.Code)
	}
}

// A suspended account is refused everywhere else on the next request; an open
// editor must not be the exception.
func TestWopiRefusesSuspendedAccount(t *testing.T) {
	srv, q := wopiEnv(t)
	ctx := context.Background()
	f := newWopiFixture(t, srv, q, "suspended")
	id := nodeIDString(t, f.file.ID)

	if err := q.SetUserSuspended(ctx, store.SetUserSuspendedParams{
		ID:          f.editor.ID,
		SuspendedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true},
	}); err != nil {
		t.Fatalf("suspend: %v", err)
	}

	rec := wopiRequest(t, srv.handleWopiCheckFileInfo, "GET", id, f.token)
	if rec.Code != http.StatusForbidden {
		t.Errorf("suspended account opened the editor: %d %s", rec.Code, rec.Body)
	}
}

// Downgrading edit to view must downgrade the session rather than end it: the
// reader keeps the document open, read-only, which is the whole reason view
// access is enough to mint a token.
func TestWopiDowngradeKeepsReadAccess(t *testing.T) {
	srv, q := wopiEnv(t)
	ctx := context.Background()
	f := newWopiFixture(t, srv, q, "downgrade")
	id := nodeIDString(t, f.file.ID)

	revokeShares(t, q, f.file.ID)
	if _, err := q.CreateShare(ctx, store.CreateShareParams{
		NodeID: f.file.ID, Kind: store.ShareKindDirect, GranteeUserID: f.editor.ID,
		Level: store.ShareLevelView, CanDownload: true, CreatedBy: f.editor.ID,
	}); err != nil {
		t.Fatalf("regrant view: %v", err)
	}

	rec := wopiRequest(t, srv.handleWopiCheckFileInfo, "GET", id, f.token)
	if rec.Code != http.StatusOK {
		t.Fatalf("view access lost the session entirely: %d %s", rec.Code, rec.Body)
	}
	if body := rec.Body.String(); !strings.Contains(body, `"UserCanWrite":false`) {
		t.Errorf("CheckFileInfo still advertises write on a view-only grant: %s", body)
	}

	if rec := wopiRequest(t, srv.handleWopiLockOps, "POST", id, f.token); rec.Code != http.StatusForbidden {
		t.Errorf("lock on a view-only grant = %d, want 403", rec.Code)
	}
}

// A token minted for one file must not reach another, even when both are
// readable by its subject.
func TestWopiTokenIsBoundToItsFile(t *testing.T) {
	srv, q := wopiEnv(t)
	f := newWopiFixture(t, srv, q, "bound")

	other, err := q.CreateNode(context.Background(), store.CreateNodeParams{
		SpaceID: f.space.ID, Kind: store.NodeKindFile, Name: "other.docx", CreatedBy: f.editor.ID,
	})
	if err != nil {
		t.Fatalf("create other file: %v", err)
	}

	rec := wopiRequest(t, srv.handleWopiGetFile, "GET", nodeIDString(t, other.ID), f.token)
	if rec.Code != http.StatusForbidden {
		t.Errorf("token for one file read another: %d", rec.Code)
	}
}
