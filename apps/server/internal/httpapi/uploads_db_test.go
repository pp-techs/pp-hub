package httpapi

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nhatbui/pp-hub/apps/server/internal/auth"
	"github.com/nhatbui/pp-hub/apps/server/internal/blob"
	"github.com/nhatbui/pp-hub/apps/server/internal/fs"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
	"github.com/nhatbui/pp-hub/apps/server/internal/uploads"
)

// The upload routes are a state machine over a row and a staging file, and
// what they promise — an interrupted chunk keeps what arrived, a commit
// re-checks the editor lock, quota counts what is already promised — only
// shows against a real Postgres. Same contract as the rest of this package's
// database tests:
//
//	PP_TEST_DATABASE_URL=postgres://pphub:pphub@localhost:5433/pphub go test ./internal/httpapi/
func uploadEnv(t *testing.T) (*Server, *store.Queries, string) {
	t.Helper()
	srv, q := wopiEnv(t)
	blobStore, err := blob.NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatalf("blob store: %v", err)
	}
	srv.fs = fs.NewService(q, blobStore)
	srv.blobs = blobStore
	staging := t.TempDir()
	srv.uploads, err = uploads.NewService(q, staging)
	if err != nil {
		t.Fatalf("uploads: %v", err)
	}
	return srv, q, staging
}

// tusCall drives one request through the upload routes as mounted in
// Router(), with the session the authenticated middleware would have set.
func tusCall(t *testing.T, srv *Server, u store.User, method, path string, hdr map[string]string, body io.Reader) *httptest.ResponseRecorder {
	t.Helper()
	r := chi.NewRouter()
	r.Route("/api/v1/uploads", func(r chi.Router) {
		r.Post("/", srv.handleCreateUpload)
		r.Route("/{uploadID}", func(r chi.Router) {
			r.Head("/", srv.handleUploadOffset)
			r.Patch("/", srv.handleUploadChunk)
			r.Delete("/", srv.handleTerminateUpload)
		})
	})
	req := httptest.NewRequest(method, path, body)
	req.Header.Set("Tus-Resumable", "1.0.0")
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	req = req.WithContext(context.WithValue(req.Context(), sessionCtxKey, &auth.Session{ID: u.ID, User: u}))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func tusMeta(kv ...string) string {
	parts := make([]string, 0, len(kv)/2)
	for i := 0; i+1 < len(kv); i += 2 {
		parts = append(parts, kv[i]+" "+base64.StdEncoding.EncodeToString([]byte(kv[i+1])))
	}
	return strings.Join(parts, ",")
}

func createUpload(t *testing.T, srv *Server, u store.User, length int, meta string) *httptest.ResponseRecorder {
	t.Helper()
	return tusCall(t, srv, u, http.MethodPost, "/api/v1/uploads", map[string]string{
		"Upload-Length":   strconv.Itoa(length),
		"Upload-Metadata": meta,
	}, nil)
}

func patchChunk(t *testing.T, srv *Server, u store.User, loc string, offset int, body io.Reader) *httptest.ResponseRecorder {
	t.Helper()
	return tusCall(t, srv, u, http.MethodPatch, loc, map[string]string{
		"Content-Type":  "application/offset+octet-stream",
		"Upload-Offset": strconv.Itoa(offset),
	}, body)
}

// brokenBody yields its bytes and then fails, the way a request body does when
// a proxy cuts the connection mid-chunk.
type brokenBody struct{ r io.Reader }

func (b *brokenBody) Read(p []byte) (int, error) {
	n, err := b.r.Read(p)
	if errors.Is(err, io.EOF) {
		return n, io.ErrUnexpectedEOF
	}
	return n, err
}

func randomBytes(t *testing.T, n int) []byte {
	t.Helper()
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return b
}

func memberSpace(t *testing.T, srv *Server, q *store.Queries, label string) (store.Space, store.User) {
	t.Helper()
	ctx := context.Background()
	owner := feedUser(t, q, label)
	sp, err := srv.spaces.Create(ctx, fmt.Sprintf("upload-%s-%d", label, os.Getpid()), owner.ID)
	if err != nil {
		t.Fatalf("create space: %v", err)
	}
	t.Cleanup(func() { _ = q.DeleteSpace(ctx, sp.ID) })
	return sp, owner
}

func TestInterruptedChunkResumesFromTheBytesThatArrived(t *testing.T) {
	srv, q, staging := uploadEnv(t)
	ctx := context.Background()
	sp, owner := memberSpace(t, srv, q, "resume")
	content := randomBytes(t, 96<<10)

	rec := createUpload(t, srv, owner, len(content), tusMeta("filename", "report.pdf", "space_id", uuidString(sp.ID)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: got %d; body=%s", rec.Code, rec.Body.String())
	}
	loc := rec.Header().Get("Location")

	// The first chunk dies 40 KiB in. Those bytes must be kept.
	cut := 40 << 10
	rec = patchChunk(t, srv, owner, loc, 0, &brokenBody{r: bytes.NewReader(content[:cut])})
	if got := rec.Header().Get("Upload-Offset"); got != strconv.Itoa(cut) {
		t.Fatalf("interrupted chunk: Upload-Offset %q, want %d (status %d)", got, cut, rec.Code)
	}
	rec = tusCall(t, srv, owner, http.MethodHead, loc, nil, nil)
	if rec.Code != http.StatusOK || rec.Header().Get("Upload-Offset") != strconv.Itoa(cut) {
		t.Fatalf("HEAD after interruption: %d offset %q, want 200 offset %d", rec.Code, rec.Header().Get("Upload-Offset"), cut)
	}

	// Re-sending from zero is a conflict that names where to resume.
	rec = patchChunk(t, srv, owner, loc, 0, bytes.NewReader(content))
	if rec.Code != http.StatusConflict || rec.Header().Get("Upload-Offset") != strconv.Itoa(cut) {
		t.Fatalf("stale offset: %d offset %q, want 409 offset %d", rec.Code, rec.Header().Get("Upload-Offset"), cut)
	}

	rec = patchChunk(t, srv, owner, loc, cut, bytes.NewReader(content[cut:]))
	if rec.Code != http.StatusNoContent || rec.Header().Get("Upload-Offset") != strconv.Itoa(len(content)) {
		t.Fatalf("final chunk: %d offset %q; body=%s", rec.Code, rec.Header().Get("Upload-Offset"), rec.Body.String())
	}

	files := spaceFiles(t, srv, sp.ID)
	if len(files) != 1 || files[0].Name != "report.pdf" {
		t.Fatalf("want exactly report.pdf in the space, got %+v", files)
	}
	_, _, rc, err := srv.fs.OpenFile(ctx, files[0].ID)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	stored, _ := io.ReadAll(rc)
	_ = rc.Close()
	if !bytes.Equal(stored, content) {
		t.Fatalf("stored %d bytes differ from the %d uploaded", len(stored), len(content))
	}

	// A client that lost the final response asks, and re-sends nothing new.
	rec = tusCall(t, srv, owner, http.MethodHead, loc, nil, nil)
	if rec.Header().Get("Upload-Offset") != strconv.Itoa(len(content)) {
		t.Fatalf("HEAD after commit: offset %q, want %d", rec.Header().Get("Upload-Offset"), len(content))
	}
	rec = patchChunk(t, srv, owner, loc, len(content), bytes.NewReader(nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("repeated final PATCH: got %d, want 204", rec.Code)
	}
	if files := spaceFiles(t, srv, sp.ID); len(files) != 1 {
		t.Fatalf("a repeated final PATCH must not commit twice, got %d files", len(files))
	}
	if left, _ := os.ReadDir(staging); len(left) != 0 {
		t.Fatalf("staging dir should be empty after commit, has %d entries", len(left))
	}
}

// spaceFiles lists the files at a space's root; a new space also holds its
// archive folder, which is not what these tests count.
func spaceFiles(t *testing.T, srv *Server, spaceID pgtype.UUID) []store.Node {
	t.Helper()
	nodes, err := srv.fs.List(context.Background(), spaceID, pgtype.UUID{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var files []store.Node
	for _, n := range nodes {
		if n.Kind == store.NodeKindFile {
			files = append(files, n)
		}
	}
	return files
}

func TestUploadIsInvisibleToOtherAccounts(t *testing.T) {
	srv, q, _ := uploadEnv(t)
	sp, owner := memberSpace(t, srv, q, "private")
	other := feedUser(t, q, "private-other")

	rec := createUpload(t, srv, owner, 10, tusMeta("filename", "a.txt", "space_id", uuidString(sp.ID)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: got %d; body=%s", rec.Code, rec.Body.String())
	}
	loc := rec.Header().Get("Location")
	if rec := patchChunk(t, srv, other, loc, 0, strings.NewReader("0123456789")); rec.Code != http.StatusNotFound {
		t.Fatalf("foreign PATCH: got %d, want 404", rec.Code)
	}
	if rec := tusCall(t, srv, other, http.MethodDelete, loc, nil, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("foreign DELETE: got %d, want 404", rec.Code)
	}
}

// creation-with-upload: a file that fits in the POST lands in that one
// request, so a folder of small files costs no more requests than it did
// before uploads were resumable.
func TestSmallFileLandsWithItsCreationRequest(t *testing.T) {
	srv, q, _ := uploadEnv(t)
	sp, owner := memberSpace(t, srv, q, "oneshot")

	rec := tusCall(t, srv, owner, http.MethodPost, "/api/v1/uploads", map[string]string{
		"Upload-Length":   "11",
		"Upload-Metadata": tusMeta("filename", "note.txt", "space_id", uuidString(sp.ID)),
		"Content-Type":    "application/offset+octet-stream",
	}, strings.NewReader("hello world"))
	if rec.Code != http.StatusCreated || rec.Header().Get("Upload-Offset") != "11" {
		t.Fatalf("create with data: %d offset %q; body=%s", rec.Code, rec.Header().Get("Upload-Offset"), rec.Body.String())
	}
	files := spaceFiles(t, srv, sp.ID)
	if len(files) != 1 || files[0].Name != "note.txt" {
		t.Fatalf("want note.txt committed by the POST alone, got %+v", files)
	}
}

// A replacement lands as a new version, so it must respect the editor's lock
// both before the first byte and at commit — an editor can open the file
// while the bytes are still arriving. A lock at commit keeps the upload so
// the user can retry once the editor closes, without re-sending anything.
func TestReplacementUploadHonoursTheEditorLock(t *testing.T) {
	srv, q, _ := uploadEnv(t)
	ctx := context.Background()
	fx := newWopiFixture(t, srv, q, "tuslock")
	meta := tusMeta("filename", "ignored.docx", "node_id", uuidString(fx.file.ID))
	versions := func() int {
		vs, err := q.ListVersions(ctx, fx.file.ID)
		if err != nil {
			t.Fatalf("list versions: %v", err)
		}
		return len(vs)
	}

	if err := srv.wopi.Lock(ctx, fx.file.ID, "editor-tok"); err != nil {
		t.Fatalf("lock: %v", err)
	}
	if rec := createUpload(t, srv, fx.editor, 5, meta); rec.Code != http.StatusLocked {
		t.Fatalf("create while locked: got %d, want 423; body=%s", rec.Code, rec.Body.String())
	}
	if err := srv.wopi.Unlock(ctx, fx.file.ID, "editor-tok"); err != nil {
		t.Fatalf("unlock: %v", err)
	}

	rec := createUpload(t, srv, fx.editor, 5, meta)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: got %d; body=%s", rec.Code, rec.Body.String())
	}
	loc := rec.Header().Get("Location")
	if err := srv.wopi.Lock(ctx, fx.file.ID, "editor-tok"); err != nil {
		t.Fatalf("lock: %v", err)
	}
	if rec := patchChunk(t, srv, fx.editor, loc, 0, strings.NewReader("v2!!!")); rec.Code != http.StatusLocked {
		t.Fatalf("commit while locked: got %d, want 423; body=%s", rec.Code, rec.Body.String())
	}
	if n := versions(); n != 1 {
		t.Fatalf("a locked commit must not add a version, have %d", n)
	}
	if rec := tusCall(t, srv, fx.editor, http.MethodHead, loc, nil, nil); rec.Header().Get("Upload-Offset") != "5" {
		t.Fatalf("the bytes must survive a locked commit: HEAD %d offset %q", rec.Code, rec.Header().Get("Upload-Offset"))
	}

	if err := srv.wopi.Unlock(ctx, fx.file.ID, "editor-tok"); err != nil {
		t.Fatalf("unlock: %v", err)
	}
	if rec := patchChunk(t, srv, fx.editor, loc, 5, bytes.NewReader(nil)); rec.Code != http.StatusNoContent {
		t.Fatalf("retried commit: got %d, want 204; body=%s", rec.Code, rec.Body.String())
	}
	if n := versions(); n != 2 {
		t.Fatalf("retried commit should add one version, have %d", n)
	}
	node, err := srv.fs.Get(ctx, fx.file.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if node.Name != fx.file.Name {
		t.Fatalf("a replacement keeps the file's name: got %q, want %q", node.Name, fx.file.Name)
	}
}

// Usage counts every stored version, and creation counts what other uploads
// in flight have already promised, so neither history nor parallelism lets
// a space past its quota.
func TestUploadCreationCountsHistoryAndUploadsInFlight(t *testing.T) {
	srv, q, _ := uploadEnv(t)
	ctx := context.Background()
	sp, owner := memberSpace(t, srv, q, "quota")
	if _, err := q.UpdateSpaceSettings(ctx, store.UpdateSpaceSettingsParams{
		ID: sp.ID, Name: sp.Name, Slug: sp.Slug, Description: sp.Description,
		DefaultAsAdmin: sp.DefaultAsAdmin, QuotaBytes: 1000,
		TrashRetentionDays: sp.TrashRetentionDays, AllowPublicLinks: sp.AllowPublicLinks,
	}); err != nil {
		t.Fatalf("set quota: %v", err)
	}
	file, _, err := srv.fs.PutFile(ctx, sp.ID, pgtype.UUID{}, pgtype.UUID{}, "a.bin", bytes.NewReader(randomBytes(t, 300)), owner.ID)
	if err != nil {
		t.Fatalf("first version: %v", err)
	}
	if _, _, err := srv.fs.PutFile(ctx, sp.ID, pgtype.UUID{}, file.ID, "a.bin", bytes.NewReader(randomBytes(t, 300)), owner.ID); err != nil {
		t.Fatalf("second version: %v", err)
	}
	// 600 used: the superseded version still occupies the disk.
	meta := tusMeta("filename", "b.bin", "space_id", uuidString(sp.ID))
	if rec := createUpload(t, srv, owner, 500, meta); rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("600 used + 500: got %d, want 413", rec.Code)
	}
	if rec := createUpload(t, srv, owner, 300, meta); rec.Code != http.StatusCreated {
		t.Fatalf("600 used + 300: got %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	if rec := createUpload(t, srv, owner, 300, meta); rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("600 used + 300 in flight + 300: got %d, want 413", rec.Code)
	}
}
