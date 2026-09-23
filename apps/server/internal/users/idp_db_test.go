package users_test

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nhatbui/pp-hub/apps/server/internal/blob"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
	"github.com/nhatbui/pp-hub/apps/server/internal/users"
)

// uniquePNG is a 1×1 PNG whose bytes no earlier run produced, so the blob's
// ref_count starts from zero and can be asserted absolutely. The uniqueness is
// trailing bytes after IEND, which decoders and the sniffer both ignore.
func uniquePNG(t *testing.T, seed int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewNRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	n := time.Now().UnixNano() + int64(seed)
	for i := range 8 {
		buf.WriteByte(byte(n >> (8 * i)))
	}
	return buf.Bytes()
}

// pictureHost serves images by path and counts how often each was fetched.
type pictureHost struct {
	srv  *httptest.Server
	hits atomic.Int32
	body map[string][]byte
}

func newPictureHost(t *testing.T, body map[string][]byte) *pictureHost {
	h := &pictureHost{body: body}
	h.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.hits.Add(1)
		b, ok := h.body[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(b)
	}))
	t.Cleanup(h.srv.Close)
	return h
}

func (h *pictureHost) url(path string) string { return h.srv.URL + path }

func idpEnv(t *testing.T) (*users.Service, *store.Queries) {
	t.Helper()
	_, q := searchEnv(t)
	blobs, err := blob.NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return users.NewService(q, blobs), q
}

func refCount(t *testing.T, q *store.Queries, hash string) int32 {
	t.Helper()
	b, err := q.GetBlob(context.Background(), hash)
	if err != nil {
		t.Fatalf("get blob %s: %v", hash, err)
	}
	return b.RefCount
}

func reload(t *testing.T, q *store.Queries, u store.User) store.User {
	t.Helper()
	got, err := q.GetUser(context.Background(), u.ID)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// The avatar follows whichever side changed last, and each replaced blob
// loses exactly the one reference the account held on it.
func TestIdPPictureFollowsTheLatestChange(t *testing.T) {
	svc, q := idpEnv(t)
	ctx := context.Background()
	idp1, upload, idp2 := uniquePNG(t, 1), uniquePNG(t, 2), uniquePNG(t, 3)
	host := newPictureHost(t, map[string][]byte{"/a.png": idp1, "/b.png": idp2})
	u := searchUser(t, q, time.Now().Format("150405.000000000"), "follow")

	u, err := svc.SyncIdPProfile(ctx, u, users.IdPProfile{Picture: host.url("/a.png")})
	if err != nil {
		t.Fatalf("first sync: %v", err)
	}
	if !u.AvatarFromIdp || u.AvatarBlob == "" || u.AvatarUrl == "" {
		t.Fatalf("idp picture not mirrored: %+v", u)
	}
	fromIdP := u.AvatarBlob
	if n := refCount(t, q, fromIdP); n != 1 {
		t.Fatalf("idp blob ref_count = %d, want 1", n)
	}

	// Unchanged at the IdP: no second download.
	if u, err = svc.SyncIdPProfile(ctx, u, users.IdPProfile{Picture: host.url("/a.png")}); err != nil {
		t.Fatal(err)
	}
	if n := host.hits.Load(); n != 1 {
		t.Fatalf("picture fetched %d times, want 1", n)
	}

	// An upload here wins over an IdP picture that has not changed since.
	if _, err := svc.UploadAvatar(ctx, u.ID, bytes.NewReader(upload)); err != nil {
		t.Fatalf("upload: %v", err)
	}
	u, err = svc.SyncIdPProfile(ctx, reload(t, q, u), users.IdPProfile{Picture: host.url("/a.png")})
	if err != nil {
		t.Fatal(err)
	}
	if u.AvatarFromIdp || u.AvatarBlob == fromIdP {
		t.Fatalf("upload was overwritten by an unchanged idp picture")
	}
	uploaded := u.AvatarBlob
	if n := refCount(t, q, fromIdP); n != 0 {
		t.Fatalf("replaced idp blob ref_count = %d, want 0", n)
	}

	// A new picture at the IdP wins over the upload.
	if u, err = svc.SyncIdPProfile(ctx, u, users.IdPProfile{Picture: host.url("/b.png")}); err != nil {
		t.Fatal(err)
	}
	if !u.AvatarFromIdp || u.AvatarBlob == uploaded {
		t.Fatalf("changed idp picture did not replace the upload")
	}
	if n := refCount(t, q, uploaded); n != 0 {
		t.Fatalf("replaced upload ref_count = %d, want 0", n)
	}

	// The IdP dropping its picture takes away the avatar it supplied.
	dropped := u.AvatarBlob
	if u, err = svc.SyncIdPProfile(ctx, u, users.IdPProfile{}); err != nil {
		t.Fatal(err)
	}
	if u.AvatarBlob != "" || u.AvatarUrl != "" {
		t.Fatalf("idp-supplied avatar survived its removal upstream: %+v", u)
	}
	if n := refCount(t, q, dropped); n != 0 {
		t.Fatalf("cleared blob ref_count = %d, want 0", n)
	}
}

// An account that uploaded before the IdP was ever consulted keeps its
// upload on the first sign-in, and on the IdP having no picture at all.
func TestUploadPredatingTheSyncSurvivesIt(t *testing.T) {
	svc, q := idpEnv(t)
	ctx := context.Background()
	host := newPictureHost(t, map[string][]byte{"/a.png": uniquePNG(t, 4), "/b.png": uniquePNG(t, 5)})
	u := searchUser(t, q, time.Now().Format("150405.000000000"), "predates")

	if _, err := svc.UploadAvatar(ctx, u.ID, bytes.NewReader(uniquePNG(t, 6))); err != nil {
		t.Fatalf("upload: %v", err)
	}
	u = reload(t, q, u)
	uploaded := u.AvatarBlob

	u, err := svc.SyncIdPProfile(ctx, u, users.IdPProfile{Picture: host.url("/a.png")})
	if err != nil {
		t.Fatal(err)
	}
	if u.AvatarBlob != uploaded || host.hits.Load() != 0 {
		t.Fatalf("first-seen idp picture replaced an earlier upload")
	}
	if u, err = svc.SyncIdPProfile(ctx, u, users.IdPProfile{}); err != nil {
		t.Fatal(err)
	}
	if u.AvatarBlob != uploaded {
		t.Fatalf("idp dropping its picture cleared an upload")
	}
}

// A picture that is not a raster image is refused once and not re-fetched.
func TestNonImagePictureIsRefusedOnce(t *testing.T) {
	svc, q := idpEnv(t)
	ctx := context.Background()
	host := newPictureHost(t, map[string][]byte{
		"/a.svg": []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`),
	})
	u := searchUser(t, q, time.Now().Format("150405.000000000"), "svg")

	u, err := svc.SyncIdPProfile(ctx, u, users.IdPProfile{Picture: host.url("/a.svg")})
	if err == nil {
		t.Fatal("svg picture accepted")
	}
	if u.AvatarBlob != "" {
		t.Fatalf("svg stored as avatar")
	}
	if _, err := svc.SyncIdPProfile(ctx, u, users.IdPProfile{Picture: host.url("/a.svg")}); err != nil {
		t.Fatalf("second sync: %v", err)
	}
	if n := host.hits.Load(); n != 1 {
		t.Fatalf("refused picture fetched %d times, want 1", n)
	}
}

func TestPhoneAndGenderMirrored(t *testing.T) {
	svc, q := idpEnv(t)
	u := searchUser(t, q, time.Now().Format("150405.000000000"), "details")

	u, err := svc.SyncIdPProfile(context.Background(), u, users.IdPProfile{Phone: "84901234567", Gender: "female"})
	if err != nil {
		t.Fatal(err)
	}
	if u.Phone != "+84901234567" || u.Gender != "female" {
		t.Fatalf("phone=%q gender=%q", u.Phone, u.Gender)
	}
}
