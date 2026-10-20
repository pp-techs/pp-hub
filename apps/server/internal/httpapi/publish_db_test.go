package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nhatbui/pp-hub/apps/server/internal/collab"
	"github.com/nhatbui/pp-hub/apps/server/internal/publish"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
	"github.com/nhatbui/pp-hub/apps/server/internal/sysconfig"
)

// Publishing (M50) end to end against a real Postgres and blob store: the
// management routes, the /p viewer API and the artifact host, all through
// the production router. Skips without PP_TEST_DATABASE_URL.

const testArtifactHost = "art.localhost"

type publishRig struct {
	*collabRig
}

func newPublishRig(t *testing.T) publishRig {
	t.Helper()
	return newPublishRigWith(t, true)
}

// newPublishRigWith builds the rig with an artifact origin of its own
// (isolated) or with sites served under /a/ on the app's origin.
func newPublishRigWith(t *testing.T, isolated bool) publishRig {
	t.Helper()
	rig := newCollabRigWith(t, collab.Config{}, func(s *Server) {
		s.sysconfig = sysconfig.NewService(s.store, nil)
		art := ""
		if isolated {
			art = "http://" + testArtifactHost
			s.artifactHost = testArtifactHost
		}
		s.publish = publish.New(publish.Config{
			Store: s.store, Blobs: s.blobs,
			TokenKey: make([]byte, 32), GrantKey: []byte("0123456789abcdef0123456789abcdef"),
			ArtifactURL: art, ExternalURL: s.externalURL, MaxBytes: 1 << 20,
			Enabled: s.sysconfig.ArtifactsEnabled,
		})
	})
	pr := publishRig{rig}
	pr.setSetting(t, sysconfig.KeyArtifactsEnabled, true)
	return pr
}

func (rig publishRig) setSetting(t *testing.T, key string, v any) {
	t.Helper()
	raw, _ := json.Marshal(v)
	if _, err := rig.srv.sysconfig.UpdateMany(context.Background(), sysconfig.DefaultTenantID,
		map[string]json.RawMessage{key: raw}, pgtype.UUID{}); err != nil {
		t.Fatalf("set %s: %v", key, err)
	}
	t.Cleanup(func() { _ = rig.srv.sysconfig.Delete(context.Background(), sysconfig.DefaultTenantID, key) })
}

type publishFixture struct {
	space store.Space
	owner store.User
	site  store.Node
	app   store.Node
}

// fixture is a space with a site folder: index.html, app.js, docs/index.html.
func (rig publishRig) fixture(t *testing.T, label string) publishFixture {
	t.Helper()
	ctx := context.Background()
	owner := feedUser(t, rig.q, label+"-owner")
	sp, err := rig.srv.spaces.Create(ctx, fmt.Sprintf("publish-%s-%d", label, os.Getpid()), owner.ID)
	if err != nil {
		t.Fatalf("create space: %v", err)
	}
	t.Cleanup(func() { _ = rig.q.DeleteSpace(ctx, sp.ID) })
	rig.login(t, owner)
	site, err := rig.srv.fs.Mkdir(ctx, sp.ID, pgtype.UUID{}, "site", owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	docs, err := rig.srv.fs.Mkdir(ctx, sp.ID, site.ID, "docs", owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	put := func(parent pgtype.UUID, name, content string) store.Node {
		n, _, err := rig.srv.fs.PutFile(ctx, sp.ID, parent, pgtype.UUID{}, name, strings.NewReader(content), owner.ID)
		if err != nil {
			t.Fatalf("put %s: %v", name, err)
		}
		return n
	}
	put(site.ID, "index.html", `<script src="app.js"></script>`)
	app := put(site.ID, "app.js", "console.log(1)")
	put(docs.ID, "index.html", "<h1>docs</h1>")
	return publishFixture{space: sp, owner: owner, site: site, app: app}
}

func (rig publishRig) publish(t *testing.T, u store.User, node store.Node, body map[string]any) (publicationDTO, rigResponse) {
	t.Helper()
	if body == nil {
		body = map[string]any{}
	}
	res := rig.do(t, u, http.MethodPost, "/api/v1/nodes/"+uuidString(node.ID)+"/publication", body)
	var dto publicationDTO
	if res.code == http.StatusOK || res.code == http.StatusCreated {
		if err := json.Unmarshal(res.body, &dto); err != nil {
			t.Fatalf("decode publication: %v; %s", err, res.body)
		}
	}
	return dto, res
}

func tokenOf(t *testing.T, dto publicationDTO) string {
	t.Helper()
	i := strings.LastIndex(dto.URL, "/p/")
	if i < 0 {
		t.Fatalf("url %q has no /p/", dto.URL)
	}
	return dto.URL[i+3:]
}

// artifact is an anonymous request to the artifact host, redirects not
// followed.
func (rig publishRig) artifact(t *testing.T, method, path string, hdr map[string]string) rigResponse {
	t.Helper()
	req, err := http.NewRequest(method, rig.url+"/", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.URL.Opaque = path // sent verbatim, escapes and dots included
	req.Host = testArtifactHost
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return rigResponse{code: resp.StatusCode, header: resp.Header, body: b}
}

// viewer is an anonymous request to the main origin's /p API.
func (rig publishRig) viewer(t *testing.T, method, path string, body any) rigResponse {
	t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = strings.NewReader(string(b))
	}
	req, err := http.NewRequest(method, rig.url+path, rd)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", rig.url)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return rigResponse{code: resp.StatusCode, header: resp.Header, body: b}
}

func (rig publishRig) usedBytes(t *testing.T, sp store.Space) int64 {
	t.Helper()
	got, err := rig.q.GetSpace(context.Background(), sp.ID)
	if err != nil {
		t.Fatal(err)
	}
	return got.UsedBytes
}

func (rig publishRig) refCount(t *testing.T, node store.Node) int32 {
	t.Helper()
	ver, err := rig.q.GetCurrentVersion(context.Background(), node.ID)
	if err != nil {
		t.Fatal(err)
	}
	b, err := rig.q.GetBlob(context.Background(), ver.BlobHash)
	if err != nil {
		t.Fatal(err)
	}
	return b.RefCount
}

func assertSandboxed(t *testing.T, res rigResponse, what string) {
	t.Helper()
	csp := res.header.Get("Content-Security-Policy")
	if !strings.HasPrefix(csp, "sandbox allow-scripts allow-popups allow-popups-to-escape-sandbox allow-modals allow-downloads;") ||
		!strings.Contains(csp, "connect-src 'none'") || !strings.Contains(csp, "script-src http://"+testArtifactHost+" ") {
		t.Errorf("%s: CSP %q is not the artifact sandbox", what, csp)
	}
	if res.header.Get("X-Frame-Options") != "" {
		t.Errorf("%s: X-Frame-Options %q sent on the artifact host", what, res.header.Get("X-Frame-Options"))
	}
	if res.header.Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("%s: no nosniff", what)
	}
	if res.header.Get("Set-Cookie") != "" {
		t.Errorf("%s: a cookie was set on the artifact host", what)
	}
}

// Publish, serve, republish and unpublish: the snapshot is what is served,
// the blobs are referenced and the space is charged while it exists, and an
// unpublish gives both back.
func TestPublishLifecycle(t *testing.T) {
	rig := newPublishRig(t)
	f := rig.fixture(t, "publife")
	baseUsed := rig.usedBytes(t, f.space)
	baseRefs := rig.refCount(t, f.app)

	pub, res := rig.publish(t, f.owner, f.site, nil)
	if res.code != http.StatusCreated {
		t.Fatalf("publish: %d %s", res.code, res.body)
	}
	if pub.EntryPath != "index.html" || pub.Rev != 1 || pub.FileCount != 3 || pub.Stale || pub.HasPassword {
		t.Fatalf("publication = %+v", pub)
	}
	snapshot := int64(len(`<script src="app.js"></script>`) + len("console.log(1)") + len("<h1>docs</h1>"))
	if pub.TotalBytes != snapshot {
		t.Fatalf("total_bytes %d, want %d", pub.TotalBytes, snapshot)
	}
	if got := rig.usedBytes(t, f.space); got != baseUsed+snapshot {
		t.Fatalf("used_bytes after publish %d, want %d", got, baseUsed+snapshot)
	}
	if got := rig.refCount(t, f.app); got != baseRefs+1 {
		t.Fatalf("app.js ref_count after publish %d, want %d", got, baseRefs+1)
	}
	token := tokenOf(t, pub)

	info := rig.viewer(t, http.MethodGet, "/p/"+token, nil)
	var vi publicationInfoDTO
	_ = json.Unmarshal(info.body, &vi)
	if info.code != http.StatusOK || vi.NeedsPassword || vi.ContentURL != "http://"+testArtifactHost+"/"+token+"/index.html" || vi.PublishedBy == "" {
		t.Fatalf("viewer info: %d %s", info.code, info.body)
	}

	page := rig.artifact(t, http.MethodGet, "/"+token+"/", nil)
	if page.code != http.StatusOK || string(page.body) != `<script src="app.js"></script>` {
		t.Fatalf("entry: %d %q", page.code, page.body)
	}
	assertSandboxed(t, page, "entry page")
	if page.header.Get("Cache-Control") != "private, no-cache" || page.header.Get("ETag") == "" ||
		!strings.HasPrefix(page.header.Get("Content-Type"), "text/html") {
		t.Fatalf("entry headers: %v", page.header)
	}
	if again := rig.artifact(t, http.MethodGet, "/"+token+"/index.html",
		map[string]string{"If-None-Match": page.header.Get("ETag")}); again.code != http.StatusNotModified {
		t.Fatalf("revalidation: %d", again.code)
	}
	if rng := rig.artifact(t, http.MethodGet, "/"+token+"/app.js", map[string]string{"Range": "bytes=0-6"}); rng.code != http.StatusPartialContent || string(rng.body) != "console" {
		t.Fatalf("range: %d %q", rng.code, rng.body)
	}
	if gz := rig.artifact(t, http.MethodGet, "/"+token+"/app.js", map[string]string{"Accept-Encoding": "gzip"}); gz.header.Get("Content-Encoding") != "gzip" {
		t.Fatalf("a script was not gzipped: %v", gz.header)
	}
	for path, want := range map[string]int{
		"/" + token:                       http.StatusFound,
		"/" + token + "/docs":             http.StatusFound,
		"/" + token + "/docs/":            http.StatusOK,
		"/" + token + "/missing.html":     http.StatusNotFound,
		"/" + token + "/../" + token:      http.StatusNotFound,
		"/" + token + "/%2e%2e/x":         http.StatusNotFound,
		"/" + token + "/docs//index.html": http.StatusNotFound,
		"/nosuchtoken12345/index.html":    http.StatusNotFound,
	} {
		got := rig.artifact(t, http.MethodGet, path, nil)
		if got.code != want {
			t.Errorf("GET %s = %d, want %d", path, got.code, want)
		}
		assertSandboxed(t, got, path)
	}
	if post := rig.artifact(t, http.MethodPost, "/"+token+"/", nil); post.code != http.StatusMethodNotAllowed {
		t.Errorf("POST on the artifact host = %d", post.code)
	}

	// Editing the source changes nothing public until a republish.
	if _, _, err := rig.srv.fs.PutFile(context.Background(), f.space.ID, f.site.ID, f.app.ID, "app.js",
		strings.NewReader("console.log(22)"), f.owner.ID); err != nil {
		t.Fatal(err)
	}
	got := rig.do(t, f.owner, http.MethodGet, "/api/v1/nodes/"+uuidString(f.site.ID)+"/publication", nil)
	var cur publicationDTO
	_ = json.Unmarshal(got.body, &cur)
	if got.code != http.StatusOK || !cur.Stale || cur.URL != pub.URL {
		t.Fatalf("after an edit: %d %s", got.code, got.body)
	}
	if js := rig.artifact(t, http.MethodGet, "/"+token+"/app.js", nil); string(js.body) != "console.log(1)" {
		t.Fatalf("served %q before republish", js.body)
	}
	usedBeforeRepublish := rig.usedBytes(t, f.space)
	re, res := rig.publish(t, f.owner, f.site, nil)
	if res.code != http.StatusOK || re.Rev != 2 || re.URL != pub.URL || re.Stale {
		t.Fatalf("republish: %d %s", res.code, res.body)
	}
	if js := rig.artifact(t, http.MethodGet, "/"+token+"/app.js", nil); string(js.body) != "console.log(22)" {
		t.Fatalf("served %q after republish", js.body)
	}
	if got := rig.usedBytes(t, f.space); got != usedBeforeRepublish+1 {
		t.Fatalf("used_bytes after republish %d, want %d (the snapshot grew by a byte)", got, usedBeforeRepublish+1)
	}

	usedPublished := rig.usedBytes(t, f.space)
	if del := rig.do(t, f.owner, http.MethodDelete, "/api/v1/nodes/"+uuidString(f.site.ID)+"/publication", nil); del.code != http.StatusNoContent {
		t.Fatalf("unpublish: %d %s", del.code, del.body)
	}
	if got := rig.usedBytes(t, f.space); got != usedPublished-re.TotalBytes {
		t.Fatalf("used_bytes after unpublish %d, want %d", got, usedPublished-re.TotalBytes)
	}
	if got := rig.refCount(t, f.app); got != 1 {
		t.Fatalf("app.js ref_count after unpublish %d, want 1 (its version)", got)
	}
	if gone := rig.artifact(t, http.MethodGet, "/"+token+"/", nil); gone.code != http.StatusNotFound {
		t.Fatalf("artifact after unpublish: %d", gone.code)
	}
	if gone := rig.viewer(t, http.MethodGet, "/p/"+token, nil); gone.code != http.StatusNotFound {
		t.Fatalf("viewer after unpublish: %d", gone.code)
	}
	drift, err := rig.q.RecountBlobRefs(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range drift {
		t.Errorf("ref_count drift on %s", d.Hash)
	}
}

// Without PP_ARTIFACT_URL a site is served under /a/ on the app's origin:
// the same bytes under the same sandbox, the prefix kept by the CSP and by
// every redirect, and nothing under /a/ reaches the app.
func TestPublishOnTheAppOrigin(t *testing.T) {
	rig := newPublishRigWith(t, false)
	f := rig.fixture(t, "pubsame")
	pub, res := rig.publish(t, f.owner, f.site, nil)
	if res.code != http.StatusCreated {
		t.Fatalf("publish: %d %s", res.code, res.body)
	}
	token := tokenOf(t, pub)
	base := rig.srv.externalURL + "/a/"

	info := rig.viewer(t, http.MethodGet, "/p/"+token, nil)
	var vi publicationInfoDTO
	_ = json.Unmarshal(info.body, &vi)
	if info.code != http.StatusOK || vi.ContentURL != base+token+"/index.html" {
		t.Fatalf("viewer info: %d %s", info.code, info.body)
	}

	get := func(path string) rigResponse {
		t.Helper()
		client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		req, err := http.NewRequest(http.MethodGet, rig.url+"/", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.URL.Opaque = path
		req.Header.Set("Accept", "text/html")
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return rigResponse{code: resp.StatusCode, header: resp.Header, body: b}
	}
	sandboxed := func(r rigResponse, what string) {
		t.Helper()
		csp := r.header.Get("Content-Security-Policy")
		if !strings.HasPrefix(csp, "sandbox allow-scripts ") || !strings.Contains(csp, "script-src "+base+" ") ||
			!strings.Contains(csp, "connect-src 'none'") || r.header.Get("X-Frame-Options") != "" {
			t.Errorf("%s: headers %v are not the artifact sandbox", what, r.header)
		}
	}

	page := get("/a/" + token + "/")
	if page.code != http.StatusOK || string(page.body) != `<script src="app.js"></script>` {
		t.Fatalf("entry: %d %q", page.code, page.body)
	}
	sandboxed(page, "entry page")
	for path, loc := range map[string]string{
		"/a/" + token:           token + "/",
		"/a/" + token + "/docs": "docs/",
	} {
		r := get(path)
		if r.code != http.StatusFound || r.header.Get("Location") != loc {
			t.Errorf("GET %s = %d Location %q, want 302 %q", path, r.code, r.header.Get("Location"), loc)
		}
		sandboxed(r, path)
	}
	// /a/ is published bytes only: the app's routes beneath it are
	// unknown tokens, answered under the sandbox.
	for _, path := range []string{"/a/api/v1/users/me", "/a/" + token + "/../../api/v1/users/me", "/a/s/x"} {
		r := get(path)
		if r.code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", path, r.code)
		}
		sandboxed(r, path)
	}
	// Outside /a/ the app answers, not the artifact router.
	if r := get("/healthz"); r.code != http.StatusOK || strings.HasPrefix(r.header.Get("Content-Security-Policy"), "sandbox") {
		t.Errorf("/healthz = %d, CSP %q", r.code, r.header.Get("Content-Security-Policy"))
	}
}

// A password puts the content behind a grant; a wrong password is 422; a
// republish ends every grant.
func TestPublicationPasswordAndGrant(t *testing.T) {
	rig := newPublishRig(t)
	f := rig.fixture(t, "pubpw")
	pub, res := rig.publish(t, f.owner, f.site, map[string]any{"password": "hunter2"})
	if res.code != http.StatusCreated || !pub.HasPassword {
		t.Fatalf("publish: %d %s", res.code, res.body)
	}
	token := tokenOf(t, pub)
	info := rig.viewer(t, http.MethodGet, "/p/"+token, nil)
	var vi publicationInfoDTO
	_ = json.Unmarshal(info.body, &vi)
	if !vi.NeedsPassword || vi.ContentURL != "" {
		t.Fatalf("viewer info with a password: %s", info.body)
	}
	if bare := rig.artifact(t, http.MethodGet, "/"+token+"/", nil); bare.code != http.StatusNotFound {
		t.Fatalf("content without a grant: %d", bare.code)
	}
	if wrong := rig.viewer(t, http.MethodPost, "/p/"+token+"/unlock", map[string]string{"password": "nope"}); wrong.code != http.StatusUnprocessableEntity {
		t.Fatalf("wrong password: %d %s", wrong.code, wrong.body)
	}
	ok := rig.viewer(t, http.MethodPost, "/p/"+token+"/unlock", map[string]string{"password": "hunter2"})
	var unlocked struct {
		ContentURL string `json:"content_url"`
	}
	_ = json.Unmarshal(ok.body, &unlocked)
	prefix := "http://" + testArtifactHost
	if ok.code != http.StatusOK || !strings.HasPrefix(unlocked.ContentURL, prefix+"/"+token+"/~") {
		t.Fatalf("unlock: %d %s", ok.code, ok.body)
	}
	path := strings.TrimPrefix(unlocked.ContentURL, prefix)
	if got := rig.artifact(t, http.MethodGet, path, nil); got.code != http.StatusOK {
		t.Fatalf("content with the grant: %d", got.code)
	}
	grantDir := path[:strings.LastIndex(path, "/")+1]
	if got := rig.artifact(t, http.MethodGet, grantDir+"app.js", nil); got.code != http.StatusOK {
		t.Fatalf("relative asset with the grant: %d", got.code)
	}

	if _, res := rig.publish(t, f.owner, f.site, nil); res.code != http.StatusOK {
		t.Fatalf("republish: %d %s", res.code, res.body)
	}
	if got := rig.artifact(t, http.MethodGet, path, nil); got.code != http.StatusNotFound {
		t.Fatalf("a grant for the previous rev still served: %d", got.code)
	}

	// PATCH null clears the password: the content is public again.
	clear := rig.do(t, f.owner, http.MethodPatch, "/api/v1/nodes/"+uuidString(f.site.ID)+"/publication", map[string]any{"password": nil})
	if clear.code != http.StatusOK {
		t.Fatalf("clear password: %d %s", clear.code, clear.body)
	}
	if got := rig.artifact(t, http.MethodGet, "/"+token+"/", nil); got.code != http.StatusOK {
		t.Fatalf("content after clearing the password: %d", got.code)
	}
}

// Every serve-time check answers 404 on both origins: expiry, the instance
// switch, the space's public links.
func TestPublicationStopsServing(t *testing.T) {
	rig := newPublishRig(t)
	f := rig.fixture(t, "pubstop")
	pub, res := rig.publish(t, f.owner, f.site, nil)
	if res.code != http.StatusCreated {
		t.Fatalf("publish: %d %s", res.code, res.body)
	}
	token := tokenOf(t, pub)
	serving := func() bool {
		t.Helper()
		a := rig.artifact(t, http.MethodGet, "/"+token+"/", nil)
		v := rig.viewer(t, http.MethodGet, "/p/"+token, nil)
		if (a.code == http.StatusOK) != (v.code == http.StatusOK) {
			t.Fatalf("origins disagree: artifact %d, viewer %d", a.code, v.code)
		}
		return a.code == http.StatusOK
	}
	if !serving() {
		t.Fatal("not serving after publish")
	}

	rig.setSetting(t, sysconfig.KeyArtifactsEnabled, false)
	if serving() {
		t.Fatal("served with artifacts_enabled off")
	}
	rig.setSetting(t, sysconfig.KeyArtifactsEnabled, true)

	sp, _ := rig.q.GetSpace(context.Background(), f.space.ID)
	setLinks := func(on bool) {
		if _, err := rig.q.UpdateSpaceSettings(context.Background(), store.UpdateSpaceSettingsParams{
			ID: sp.ID, Name: sp.Name, Slug: sp.Slug, Description: sp.Description, DefaultAsAdmin: sp.DefaultAsAdmin,
			QuotaBytes: sp.QuotaBytes, TrashRetentionDays: sp.TrashRetentionDays, AllowPublicLinks: on,
			ExternalSharing: sp.ExternalSharing,
		}); err != nil {
			t.Fatal(err)
		}
	}
	setLinks(false)
	if serving() {
		t.Fatal("served with public links off for the space")
	}
	setLinks(true)

	past := time.Now().Add(-time.Minute)
	row, _ := rig.q.GetPublicationByNode(context.Background(), f.site.ID)
	if _, err := rig.q.UpdatePublicationSettings(context.Background(), store.UpdatePublicationSettingsParams{
		ID: row.ID, ExpiresAt: pgtype.Timestamptz{Time: past, Valid: true},
	}); err != nil {
		t.Fatal(err)
	}
	if serving() {
		t.Fatal("served past its expiry")
	}
	n, err := rig.srv.publish.SweepExpired(context.Background())
	if err != nil || n < 1 {
		t.Fatalf("sweep: %d %v", n, err)
	}
}

// A folder with no index.html and no entry_path cannot be published; with
// an entry_path that names a file it can.
func TestPublishFolderNeedsAnEntry(t *testing.T) {
	rig := newPublishRig(t)
	f := rig.fixture(t, "pubentry")
	ctx := context.Background()
	bare, err := rig.srv.fs.Mkdir(ctx, f.space.ID, pgtype.UUID{}, "bare", f.owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := rig.srv.fs.PutFile(ctx, f.space.ID, bare.ID, pgtype.UUID{}, "page.html", strings.NewReader("<p>hi"), f.owner.ID); err != nil {
		t.Fatal(err)
	}
	if _, res := rig.publish(t, f.owner, bare, nil); res.code != http.StatusUnprocessableEntity ||
		!strings.Contains(string(res.body), `"detail"`) {
		t.Fatalf("folder without index.html: %d %s", res.code, res.body)
	}
	if _, res := rig.publish(t, f.owner, bare, map[string]any{"entry_path": "nope.html"}); res.code != http.StatusUnprocessableEntity {
		t.Fatalf("entry_path naming nothing: %d %s", res.code, res.body)
	}
	pub, res := rig.publish(t, f.owner, bare, map[string]any{"entry_path": "page.html"})
	if res.code != http.StatusCreated || pub.EntryPath != "page.html" {
		t.Fatalf("with entry_path: %d %s", res.code, res.body)
	}
}

// publish_artifact end to end: offered only with both switches, it writes
// the files into a new folder and publishes it; called again with the same
// name it replaces the files in place (trashing what it no longer names) and
// republishes at the same URL. It never writes into a folder that holds
// files and was not published.
func TestMcpPublishArtifact(t *testing.T) {
	rig := newMCPRig(t)
	rig.srv.publish = publish.New(publish.Config{
		Store: rig.q, Blobs: rig.srv.blobs, TokenKey: make([]byte, 32), GrantKey: make([]byte, 32),
		ArtifactURL: "http://" + testArtifactHost, MaxBytes: 1 << 20, Enabled: rig.srv.sysconfig.ArtifactsEnabled,
	})
	f := rig.collabRig.fixture(t, "mcppub", "# Doc\n")
	offered := func() bool {
		t.Helper()
		res, err := rig.agent(t, f.owner).ListTools(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, tool := range res.Tools {
			if tool.Name == "publish_artifact" {
				return true
			}
		}
		return false
	}
	rig.setSwitch(t, sysconfig.KeyArtifactsEnabled, true)
	if offered() {
		t.Fatal("publish_artifact offered without mcp_public_links")
	}
	rig.setSwitch(t, sysconfig.KeyMCPPublicLinks, true)
	rig.setSwitch(t, sysconfig.KeyArtifactsEnabled, false)
	if offered() {
		t.Fatal("publish_artifact offered without artifacts_enabled")
	}
	rig.setSwitch(t, sysconfig.KeyArtifactsEnabled, true)
	if !offered() {
		t.Fatal("publish_artifact missing with both switches on")
	}

	agent := rig.agent(t, f.owner)
	space := uuidString(f.space.ID)
	var first publicationDTO
	mustTool(t, agent, "publish_artifact", map[string]any{
		"space_id": space, "name": "demo",
		"files": []map[string]any{
			{"path": "index.html", "content": "<h1>v1</h1>"},
			{"path": "js/app.js", "content": "Y29uc29sZS5sb2coMSk=", "encoding": "base64"},
			{"path": "old.txt", "content": "bye"},
		},
	}, &first)
	if first.FileCount != 3 || first.Rev != 1 || first.EntryPath != "index.html" {
		t.Fatalf("first publish = %+v", first)
	}
	var second publicationDTO
	mustTool(t, agent, "publish_artifact", map[string]any{
		"space_id": space, "name": "demo", "password": "pw",
		"files": []map[string]any{
			{"path": "index.html", "content": "<h1>v2</h1>"},
			{"path": "js/app.js", "content": "console.log(2)"},
		},
	}, &second)
	if second.URL != first.URL || second.Rev != 2 || second.FileCount != 2 || !second.HasPassword || second.NodeID != first.NodeID {
		t.Fatalf("republish = %+v (first %+v)", second, first)
	}

	// A folder that already holds files and was never published is not
	// the tool's to overwrite.
	notes, err := rig.srv.fs.Mkdir(context.Background(), f.space.ID, pgtype.UUID{}, "notes", f.owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := rig.srv.fs.PutFile(context.Background(), f.space.ID, notes.ID, pgtype.UUID{}, "keep.txt",
		strings.NewReader("mine"), f.owner.ID); err != nil {
		t.Fatal(err)
	}
	if text, isErr := callTool(t, agent, "publish_artifact", map[string]any{
		"space_id": space, "name": "notes",
		"files": []map[string]any{{"path": "index.html", "content": "x"}},
	}); !isErr || !strings.Contains(text, "not published") {
		t.Fatalf("an unpublished folder with files was written into: %s", text)
	}
	if text, isErr := callTool(t, agent, "publish_artifact", map[string]any{
		"space_id": space, "name": "../x", "files": []map[string]any{{"path": "index.html", "content": "x"}},
	}); !isErr {
		t.Fatalf("a name with a slash was accepted: %s", text)
	}

	var done map[string]any
	mustTool(t, agent, "unpublish_artifact", map[string]any{"node_id": second.NodeID}, &done)
	var node pgtype.UUID
	_ = node.Scan(second.NodeID)
	if _, err := rig.q.GetPublicationByNode(context.Background(), node); err == nil {
		t.Fatal("still published after unpublish_artifact")
	}
}
