package httpapi

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/nhatbui/pp-hub/apps/server/internal/blob"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
	"github.com/nhatbui/pp-hub/apps/server/internal/users"
)

// onePixelPNG encodes a 1x1 opaque PNG — small, real, and decodable, so
// http.DetectContentType actually reports image/png rather than a caller
// having to trust a hand-rolled byte string.
func onePixelPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.RGBA{R: 200, G: 100, B: 50, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

// avatarGetRequest drives GET /api/v1/users/{userID}/avatar through the real
// router chain so the {userID} path param resolves exactly as in production.
func avatarGetRequest(t *testing.T, srv *Server, u store.User) *httptest.ResponseRecorder {
	t.Helper()
	r := chi.NewRouter()
	r.Get("/api/v1/users/{userID}/avatar", srv.handleGetAvatar)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/users/"+nodeIDString(t, u.ID)+"/avatar", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

// TestAvatarRoundTripsThroughTheBlobStore proves an uploaded avatar survives:
// the bytes land in the CAS, the profile points at them, and GET streams them
// back byte-for-byte with the sniffed content type. A non-image upload must
// be refused rather than stored.
func TestAvatarRoundTripsThroughTheBlobStore(t *testing.T) {
	srv, q := feedEnv(t)
	blobStore, err := blob.NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatalf("new local store: %v", err)
	}
	srv.blobs = blobStore
	srv.users = users.NewService(q)

	u := feedUser(t, q, "avatar")
	pngBytes := onePixelPNG(t)

	// Upload
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("avatar", "avatar.png")
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := fw.Write(pngBytes); err != nil {
		t.Fatalf("write form file: %v", err)
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}
	uploadRec := authedNodeRequest(t, srv.handleUploadAvatar, http.MethodPost,
		"/api/v1/users/me/avatar", "", u, &buf, mw.FormDataContentType())
	if uploadRec.Code != http.StatusOK {
		t.Fatalf("upload: got %d, want 200; body=%s", uploadRec.Code, uploadRec.Body.String())
	}
	var uploadResp struct {
		AvatarURL string `json:"avatar_url"`
	}
	if err := json.Unmarshal(uploadRec.Body.Bytes(), &uploadResp); err != nil {
		t.Fatalf("decode upload response: %v", err)
	}
	wantPrefix := "/api/v1/users/" + nodeIDString(t, u.ID) + "/avatar?v="
	if !strings.HasPrefix(uploadResp.AvatarURL, wantPrefix) {
		t.Fatalf("avatar_url %q does not carry the expected blob-prefixed path %q", uploadResp.AvatarURL, wantPrefix)
	}

	// GET it back
	getRec := avatarGetRequest(t, srv, u)
	if getRec.Code != http.StatusOK {
		t.Fatalf("get: got %d, want 200; body=%s", getRec.Code, getRec.Body.String())
	}
	if ct := getRec.Header().Get("Content-Type"); ct != "image/png" {
		t.Fatalf("Content-Type = %q, want image/png", ct)
	}
	if !bytes.Equal(getRec.Body.Bytes(), pngBytes) {
		t.Fatal("returned avatar bytes do not match the uploaded PNG")
	}

	// A non-image upload must be refused, not stored.
	var badBuf bytes.Buffer
	badMW := multipart.NewWriter(&badBuf)
	badFW, err := badMW.CreateFormFile("avatar", "notes.txt")
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := badFW.Write([]byte("just some text, not an image")); err != nil {
		t.Fatalf("write form file: %v", err)
	}
	if err := badMW.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}
	badRec := authedNodeRequest(t, srv.handleUploadAvatar, http.MethodPost,
		"/api/v1/users/me/avatar", "", u, &badBuf, badMW.FormDataContentType())
	if badRec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("text upload: got %d, want 415; body=%s", badRec.Code, badRec.Body.String())
	}
	stillGetRec := avatarGetRequest(t, srv, u)
	if !bytes.Equal(stillGetRec.Body.Bytes(), pngBytes) {
		t.Fatal("a refused upload must not change the stored avatar")
	}
}
