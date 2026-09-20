package httpapi

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/nhatbui/pp-hub/apps/server/internal/auth"
	"github.com/nhatbui/pp-hub/apps/server/internal/blob"
	"github.com/nhatbui/pp-hub/apps/server/internal/fs"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

// authedNodeRequest drives one /api/v1/nodes/{nodeID}/… call through a real
// chi router with the session the authenticated middleware would have put in
// the context, so requireNodeCaps resolves capabilities exactly as in prod.
func authedNodeRequest(t *testing.T, h http.HandlerFunc, method, pattern, nodeID string,
	u store.User, body io.Reader, contentType string) *httptest.ResponseRecorder {
	t.Helper()
	r := chi.NewRouter()
	r.Method(method, pattern, h)
	req := httptest.NewRequest(method, strings.Replace(pattern, "{nodeID}", nodeID, 1), body)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	req = req.WithContext(context.WithValue(req.Context(), sessionCtxKey,
		&auth.Session{ID: u.ID, User: u}))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

// multipartFile builds a one-part "file" multipart body, the shape
// handleUploadVersion expects.
func multipartFile(t *testing.T, field, content string) (io.Reader, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	fw, err := w.CreateFormFile(field, "version.txt")
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := fw.Write([]byte(content)); err != nil {
		t.Fatalf("write form file: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}
	return &buf, w.FormDataContentType()
}

// TestRestVersionUploadRefusesALockedFile proves the REST version-upload path
// asks file_locks before overwriting: a file open in OnlyOffice must refuse a
// concurrent REST upload rather than silently discarding the editor's work.
func TestRestVersionUploadRefusesALockedFile(t *testing.T) {
	srv, q := wopiEnv(t)
	ctx := context.Background()

	blobStore, err := blob.NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatalf("new local store: %v", err)
	}
	srv.fs = fs.NewService(q, blobStore)
	srv.blobs = blobStore

	fx := newWopiFixture(t, srv, q, "lock")

	if err := srv.wopi.Lock(ctx, fx.file.ID, "editor-tok"); err != nil {
		t.Fatalf("lock: %v", err)
	}

	body, ct := multipartFile(t, "file", "v2 while locked")
	rec := authedNodeRequest(t, srv.handleUploadVersion, http.MethodPost,
		"/api/v1/nodes/{nodeID}/versions", nodeIDString(t, fx.file.ID), fx.editor, body, ct)
	if rec.Code != http.StatusConflict {
		t.Fatalf("locked upload: got %d, want 409; body=%s", rec.Code, rec.Body.String())
	}
	versions, err := q.ListVersions(ctx, fx.file.ID)
	if err != nil {
		t.Fatalf("list versions: %v", err)
	}
	if len(versions) != 1 {
		t.Fatalf("locked upload must not add a version, got %d", len(versions))
	}

	if err := srv.wopi.Unlock(ctx, fx.file.ID, "editor-tok"); err != nil {
		t.Fatalf("unlock: %v", err)
	}

	body, ct = multipartFile(t, "file", "v2 after unlock")
	rec = authedNodeRequest(t, srv.handleUploadVersion, http.MethodPost,
		"/api/v1/nodes/{nodeID}/versions", nodeIDString(t, fx.file.ID), fx.editor, body, ct)
	if rec.Code != http.StatusOK {
		t.Fatalf("unlocked upload: got %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	versions, err = q.ListVersions(ctx, fx.file.ID)
	if err != nil {
		t.Fatalf("list versions: %v", err)
	}
	if len(versions) != 2 {
		t.Fatalf("unlocked upload should add a version, got %d", len(versions))
	}
}
