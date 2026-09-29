package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/nhatbui/pp-hub/apps/server/internal/auth"
	"github.com/nhatbui/pp-hub/apps/server/internal/blob"
	"github.com/nhatbui/pp-hub/apps/server/internal/spaces"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

// spaceAvatarRequest drives one /spaces/{spaceID}/avatar call through a real
// chi router so {spaceID} resolves exactly as in production.
func spaceAvatarRequest(t *testing.T, h http.HandlerFunc, method, spaceID string,
	u store.User, body io.Reader, contentType string) *httptest.ResponseRecorder {
	t.Helper()
	r := chi.NewRouter()
	r.Method(method, "/api/v1/spaces/{spaceID}/avatar", h)
	req := httptest.NewRequest(method, "/api/v1/spaces/"+spaceID+"/avatar", body)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	req = req.WithContext(context.WithValue(req.Context(), sessionCtxKey,
		&auth.Session{ID: u.ID, User: u}))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

// multipartAvatar builds the upload body the handler parses.
func multipartAvatar(t *testing.T, field, filename string, content []byte) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile(field, filename)
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := fw.Write(content); err != nil {
		t.Fatalf("write form file: %v", err)
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}
	return &buf, mw.FormDataContentType()
}

// TestSpaceAvatarRoundTripsThroughTheBlobStore proves an uploaded space
// picture survives end to end: the bytes land in the CAS, the space points at
// them, GET streams them back byte-for-byte with the sniffed type, a clear
// removes them, and the gates hold — an admin writes, members read, outsiders
// reach nothing, and a non-image is refused rather than stored.
func TestSpaceAvatarRoundTripsThroughTheBlobStore(t *testing.T) {
	srv, q := feedEnv(t)
	blobStore, err := blob.NewLocalStore(t.TempDir(), q.BlobClaims())
	if err != nil {
		t.Fatalf("new local store: %v", err)
	}
	srv.blobs = blobStore
	srv.spaces = spaces.NewService(q, blobStore)

	ctx := context.Background()
	admin := feedUser(t, q, "space-admin")
	sp, err := srv.spaces.Create(ctx, "Avatar Space", admin.ID)
	if err != nil {
		t.Fatalf("create space: %v", err)
	}
	spaceID := nodeIDString(t, sp.ID)
	member := feedUser(t, q, "space-member")
	if err := srv.spaces.AddMember(ctx, sp.ID, member.ID, false); err != nil {
		t.Fatalf("add member: %v", err)
	}
	stranger := feedUser(t, q, "space-stranger")

	pngBytes := onePixelPNG(t)

	// Upload as the admin.
	body, contentType := multipartAvatar(t, "avatar", "avatar.png", pngBytes)
	uploadRec := spaceAvatarRequest(t, srv.handleUploadSpaceAvatar, http.MethodPost,
		spaceID, admin, body, contentType)
	if uploadRec.Code != http.StatusOK {
		t.Fatalf("upload: got %d, want 200; body=%s", uploadRec.Code, uploadRec.Body.String())
	}
	var uploadResp struct {
		AvatarURL string `json:"avatar_url"`
	}
	if err := json.Unmarshal(uploadRec.Body.Bytes(), &uploadResp); err != nil {
		t.Fatalf("decode upload response: %v", err)
	}
	wantPrefix := "/api/v1/spaces/" + spaceID + "/avatar?v="
	if !strings.HasPrefix(uploadResp.AvatarURL, wantPrefix) {
		t.Fatalf("avatar_url %q does not carry the expected blob-prefixed path %q",
			uploadResp.AvatarURL, wantPrefix)
	}

	// The DTO is what the client renders: it must derive the same URL.
	gotSp, err := srv.spaces.Get(ctx, sp.ID)
	if err != nil {
		t.Fatalf("get space: %v", err)
	}
	if dto := spaceToDTO(gotSp, true, 1); dto.AvatarURL != uploadResp.AvatarURL {
		t.Fatalf("spaceToDTO avatar_url = %q, want %q", dto.AvatarURL, uploadResp.AvatarURL)
	}

	// A member can read it, byte-for-byte.
	for _, u := range []store.User{admin, member} {
		getRec := spaceAvatarRequest(t, srv.handleGetSpaceAvatar, http.MethodGet, spaceID, u, nil, "")
		if getRec.Code != http.StatusOK {
			t.Fatalf("get as %s: got %d, want 200; body=%s", u.Name, getRec.Code, getRec.Body.String())
		}
		if ct := getRec.Header().Get("Content-Type"); ct != "image/png" {
			t.Fatalf("Content-Type = %q, want image/png", ct)
		}
		if !bytes.Equal(getRec.Body.Bytes(), pngBytes) {
			t.Fatalf("get as %s: returned bytes do not match the uploaded PNG", u.Name)
		}
	}

	// An outsider reaches nothing — a space is a company.
	outRec := spaceAvatarRequest(t, srv.handleGetSpaceAvatar, http.MethodGet, spaceID, stranger, nil, "")
	if outRec.Code != http.StatusForbidden {
		t.Fatalf("get as stranger: got %d, want 403; body=%s", outRec.Code, outRec.Body.String())
	}

	// A non-image is refused and changes nothing.
	badBody, badContentType := multipartAvatar(t, "avatar", "notes.txt",
		[]byte("just some text, not an image"))
	badRec := spaceAvatarRequest(t, srv.handleUploadSpaceAvatar, http.MethodPost,
		spaceID, admin, badBody, badContentType)
	if badRec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("text upload: got %d, want 415; body=%s", badRec.Code, badRec.Body.String())
	}
	againRec := spaceAvatarRequest(t, srv.handleGetSpaceAvatar, http.MethodGet, spaceID, admin, nil, "")
	if !bytes.Equal(againRec.Body.Bytes(), pngBytes) {
		t.Fatal("a refused upload must not change the stored avatar")
	}

	// Writing is admin-only: a member is refused.
	memberBody, memberContentType := multipartAvatar(t, "avatar", "avatar.png", pngBytes)
	memberRec := spaceAvatarRequest(t, srv.handleUploadSpaceAvatar, http.MethodPost,
		spaceID, member, memberBody, memberContentType)
	if memberRec.Code != http.StatusForbidden {
		t.Fatalf("member upload: got %d, want 403; body=%s", memberRec.Code, memberRec.Body.String())
	}

	// Clear removes the picture and GET says so.
	clearRec := spaceAvatarRequest(t, srv.handleClearSpaceAvatar, http.MethodDelete,
		spaceID, admin, nil, "")
	if clearRec.Code != http.StatusNoContent {
		t.Fatalf("clear: got %d, want 204; body=%s", clearRec.Code, clearRec.Body.String())
	}
	goneRec := spaceAvatarRequest(t, srv.handleGetSpaceAvatar, http.MethodGet, spaceID, admin, nil, "")
	if goneRec.Code != http.StatusNotFound {
		t.Fatalf("get after clear: got %d, want 404; body=%s", goneRec.Code, goneRec.Body.String())
	}
	clearedSp, err := srv.spaces.Get(ctx, sp.ID)
	if err != nil {
		t.Fatalf("get space after clear: %v", err)
	}
	if dto := spaceToDTO(clearedSp, true, 1); dto.AvatarURL != "" {
		t.Fatalf("avatar_url after clear = %q, want empty", dto.AvatarURL)
	}
}
