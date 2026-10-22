package httpapi

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nhatbui/pp-hub/apps/server/internal/collab"
	"github.com/nhatbui/pp-hub/apps/server/internal/comments"
	"github.com/nhatbui/pp-hub/apps/server/internal/exthost"
	"github.com/nhatbui/pp-hub/apps/server/internal/notifications"
	"github.com/nhatbui/pp-hub/apps/server/internal/share"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

// Packaged apps (M51) through the production router against a real
// Postgres, or skipped like the rest of this package's database tests:
//
//	PP_TEST_DATABASE_URL=postgres://pphub:pphub@localhost:5433/pphub go test ./internal/httpapi/ -run App

const testAppID = "tasksdbtest"

func appManifest(version, backend string) map[string]any {
	m := map[string]any{
		"id": testAppID, "name": "Tasks", "version": version,
		"description": "A shared task list", "author": "Tests",
		"engines": map[string]int{"sdk": 1},
		"icon":    "icon.svg",
		"nodeKinds": []map[string]any{{
			"kind": testAppID + ".list", "label": "Task list",
			"seed": []map[string]any{{"collection": "items", "data": map[string]any{"text": "First", "done": false}}},
		}},
		"web":           map[string]any{"entry": "index.html"},
		"notifications": map[string]string{"assigned": "{actor} assigned you {excerpt}"},
	}
	if backend != "" {
		m["backend"] = map[string]string{"url": backend}
	}
	return m
}

func appZip(t *testing.T, manifest map[string]any, page string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	raw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"pp-app.json": string(raw),
		"index.html":  page,
		"app.js":      "console.log('tasks')",
		"icon.svg":    `<svg xmlns="http://www.w3.org/2000/svg"/>`,
	} {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func (f extFixture) upload(t *testing.T, u store.User, zipBytes []byte, upgrade bool) rigResponse {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, err := mw.CreateFormFile("package", "app.zip")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write(zipBytes); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/admin/apps"
	if upgrade {
		path += "?upgrade=true"
	}
	return f.rig.request(t, u, http.MethodPost, path, map[string]string{"Content-Type": mw.FormDataContentType()}, &body)
}

type appRecordView struct {
	ID         string         `json:"id"`
	Collection string         `json:"collection"`
	Data       map[string]any `json:"data"`
	Rev        int64          `json:"rev"`
	CreatedBy  *string        `json:"created_by"`
}

func appPath(node string, rest string) string {
	return "/api/v1/apps/" + testAppID + "/nodes/" + node + rest
}

func (f extFixture) records(t *testing.T, u store.User, node string) ([]appRecordView, int) {
	t.Helper()
	res := f.rig.do(t, u, http.MethodGet, appPath(node, "/records"), nil)
	if res.code != http.StatusOK {
		return nil, res.code
	}
	var page struct {
		Records []appRecordView `json:"records"`
		Next    string          `json:"next"`
	}
	if err := json.Unmarshal(res.body, &page); err != nil {
		t.Fatal(err)
	}
	return page.Records, res.code
}

type extFixture struct {
	rig      *collabRig
	space    store.Space
	admin    store.User // instance administrator and space admin
	owner    store.User // creates the items
	member   store.User // a member of the space, granted nothing
	stranger store.User // not in the space
}

func (f extFixture) spacePath(rest string) string {
	return "/api/v1/spaces/" + uuidString(f.space.ID) + rest
}

func (f extFixture) grant(t *testing.T, node nodeDTO, to store.User, level share.Level) {
	t.Helper()
	_, err := f.rig.srv.share.UpsertDirect(context.Background(), mustUUID(t, node.ID), to.ID,
		share.GrantCaps{Level: level, CanDownload: true}, pgtype.Timestamptz{}, f.owner.ID, false)
	if err != nil {
		t.Fatalf("grant: %v", err)
	}
}

func mustUUID(t *testing.T, s string) pgtype.UUID {
	t.Helper()
	var id pgtype.UUID
	if err := id.Scan(s); err != nil {
		t.Fatal(err)
	}
	return id
}

func newAppFixture(t *testing.T, label string) extFixture {
	t.Helper()
	ctx := context.Background()
	rig := newCollabRigWith(t, collab.Config{}, func(s *Server) {
		notes := notifications.NewService(s.store, s.blobs, s.share)
		s.notifications = notes
		s.comments = comments.NewService(s.store, notes)
		reg := exthost.New(exthost.Config{
			Pool: s.store.DB().(*pgxpool.Pool), Store: s.store, FS: s.fs, Share: s.share,
			Notifications: notes, StreamRecheck: 150 * time.Millisecond,
			SecretKey: bytes.Repeat([]byte{7}, 32), ExternalURL: s.externalURL,
			AssetBase: s.externalURL + exthost.AssetPath,
		})
		s.fs.SetExtHooks(reg)
		// A package left by an interrupted run must not decide this one.
		_, _ = s.store.DB().Exec(ctx, `DELETE FROM ext_packages WHERE ext_id = $1`, testAppID)
		_, _ = s.store.DB().Exec(ctx, `DELETE FROM ext_state WHERE ext_id = $1`, testAppID)
		if err := reg.Start(ctx); err != nil {
			t.Fatalf("start apps: %v", err)
		}
		s.useExt(reg)
	})
	t.Cleanup(func() {
		_, _ = rig.srv.store.DB().Exec(ctx, `DELETE FROM ext_packages WHERE ext_id = $1`, testAppID)
		_, _ = rig.srv.store.DB().Exec(ctx, `DELETE FROM ext_state WHERE ext_id = $1`, testAppID)
	})

	f := extFixture{rig: rig}
	f.admin = feedUser(t, rig.q, label+"-admin")
	if _, err := rig.srv.store.DB().Exec(ctx, `UPDATE users SET is_admin = true WHERE id = $1`, f.admin.ID); err != nil {
		t.Fatal(err)
	}
	f.admin.IsAdmin = true
	f.owner = feedUser(t, rig.q, label+"-owner")
	f.member = feedUser(t, rig.q, label+"-member")
	f.stranger = feedUser(t, rig.q, label+"-stranger")
	sp, err := rig.srv.spaces.Create(ctx, fmt.Sprintf("app-%s-%d", label, os.Getpid()), f.admin.ID)
	if err != nil {
		t.Fatalf("create space: %v", err)
	}
	t.Cleanup(func() { _ = rig.q.DeleteSpace(ctx, sp.ID) })
	f.space = sp
	for _, u := range []store.User{f.owner, f.member} {
		if err := rig.srv.spaces.AddMember(ctx, sp.ID, u.ID, false); err != nil {
			t.Fatalf("add member: %v", err)
		}
	}
	for _, u := range []store.User{f.admin, f.owner, f.member, f.stranger} {
		rig.login(t, u)
	}
	return f
}

// Upload, serve, gate, store, copy, upgrade and remove: the whole life of a
// packaged app, as an administrator and its users meet it.
func TestAppPackageLifecycle(t *testing.T) {
	f := newAppFixture(t, "applife")
	rig := f.rig
	ctx := context.Background()

	pkg := appZip(t, appManifest("1.0.0", ""), `<!doctype html><script type="module" src="./app.js"></script>`)
	if res := f.upload(t, f.owner, pkg, false); res.code != http.StatusForbidden {
		t.Fatalf("member uploads: %d %s", res.code, res.body)
	}
	if res := f.upload(t, f.admin, []byte("not a zip"), false); res.code != http.StatusBadRequest {
		t.Fatalf("garbage upload: %d %s", res.code, res.body)
	}
	res := f.upload(t, f.admin, pkg, false)
	if res.code != http.StatusCreated {
		t.Fatalf("install: %d %s", res.code, res.body)
	}
	installed := decodeJSON[adminAppDTO](t, res)
	if res := f.upload(t, f.admin, pkg, false); res.code != http.StatusConflict {
		t.Fatalf("install twice without upgrade: %d %s", res.code, res.body)
	}

	// The SPA learns the app from the listing; nothing is compiled in.
	list := decodeJSON[[]extensionDTO](t, rig.do(t, f.owner, http.MethodGet, "/api/v1/extensions", nil))
	var dto *extensionDTO
	for i := range list {
		if list[i].ID == testAppID {
			dto = &list[i]
		}
	}
	if dto == nil || dto.Package == nil || dto.Instance {
		t.Fatalf("listing: %+v", dto)
	}
	wantEntry := rig.url + "/x/" + testAppID + "/" + installed.Digest + "/index.html"
	if dto.Package.EntryURL != wantEntry || len(dto.Package.Kinds) != 1 || dto.Package.Kinds[0].NewLabel != "New Task list" {
		t.Fatalf("package dto: %+v", dto.Package)
	}

	// Its files: sandboxed, cross-origin readable, pinned by digest.
	asset := rig.request(t, f.stranger, http.MethodGet, "/x/"+testAppID+"/"+installed.Digest+"/app.js", nil, nil)
	if asset.code != http.StatusOK || !strings.HasPrefix(asset.header.Get("Content-Security-Policy"), "sandbox allow-scripts") ||
		asset.header.Get("Access-Control-Allow-Origin") != "*" || !strings.Contains(asset.header.Get("Cache-Control"), "immutable") ||
		!strings.HasPrefix(asset.header.Get("Content-Type"), "text/javascript") {
		t.Fatalf("asset: %d %v", asset.code, asset.header)
	}
	if !strings.Contains(asset.header.Get("Content-Security-Policy"), "connect-src 'none'") ||
		!strings.Contains(asset.header.Get("Content-Security-Policy"), "script-src "+rig.url+"/x/"+testAppID+"/ ") {
		t.Fatalf("asset CSP: %s", asset.header.Get("Content-Security-Policy"))
	}
	if res := rig.request(t, f.stranger, http.MethodGet, "/x/"+testAppID+"/0000/app.js", nil, nil); res.code != http.StatusNotFound {
		t.Fatalf("wrong digest: %d", res.code)
	}

	// Off until an administrator switches it on.
	kind := testAppID + ".list"
	if res := rig.do(t, f.owner, http.MethodPost, f.spacePath("/ext-nodes"), map[string]string{"ext_kind": kind, "name": "Week"}); res.code != http.StatusForbidden {
		t.Fatalf("create while off: %d %s", res.code, res.body)
	}
	if res := rig.do(t, f.admin, http.MethodPatch, "/api/v1/admin/extensions/"+testAppID, map[string]bool{"enabled": true}); res.code != http.StatusOK {
		t.Fatalf("enable: %d %s", res.code, res.body)
	}
	res = rig.do(t, f.owner, http.MethodPost, f.spacePath("/ext-nodes"), map[string]string{"ext_kind": kind, "name": "Week"})
	if res.code != http.StatusCreated {
		t.Fatalf("create: %d %s", res.code, res.body)
	}
	node := decodeJSON[nodeDTO](t, res)

	// The seed is there, written as the creator.
	recs, code := f.records(t, f.owner, node.ID)
	if code != http.StatusOK || len(recs) != 1 || recs[0].Data["text"] != "First" || recs[0].CreatedBy == nil || *recs[0].CreatedBy != uuidString(f.owner.ID) {
		t.Fatalf("seeded records: %d %+v", code, recs)
	}
	first := recs[0]

	// Access is the node's: nothing, then view (read only), then edit.
	if _, code := f.records(t, f.member, node.ID); code != http.StatusForbidden {
		t.Fatalf("ungranted member reads: %d", code)
	}
	f.grant(t, node, f.member, share.LevelView)
	if _, code := f.records(t, f.member, node.ID); code != http.StatusOK {
		t.Fatalf("viewer reads: %d", code)
	}
	if res := rig.do(t, f.member, http.MethodPost, appPath(node.ID, "/records"), map[string]any{"collection": "items", "data": map[string]any{"text": "x"}}); res.code != http.StatusForbidden {
		t.Fatalf("viewer writes: %d %s", res.code, res.body)
	}

	// Validation.
	for _, body := range []map[string]any{
		{"collection": "Items", "data": map[string]any{}},
		{"collection": "items", "data": []int{1}},
		{"collection": "items"},
	} {
		if res := rig.do(t, f.owner, http.MethodPost, appPath(node.ID, "/records"), body); res.code != http.StatusBadRequest {
			t.Fatalf("bad record %v: %d %s", body, res.code, res.body)
		}
	}
	big := map[string]any{"collection": "items", "data": map[string]any{"text": strings.Repeat("a", 70<<10)}}
	if res := rig.do(t, f.owner, http.MethodPost, appPath(node.ID, "/records"), big); res.code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized record: %d", res.code)
	}

	// Live: a viewer's stream sees an editor's write.
	blocks, stop := appStream(t, rig, f.member, appPath(node.ID, "/events"))
	defer stop()
	res = rig.do(t, f.owner, http.MethodPost, appPath(node.ID, "/records"), map[string]any{"collection": "items", "data": map[string]any{"text": "Second", "done": false}})
	if res.code != http.StatusCreated {
		t.Fatalf("create record: %d %s", res.code, res.body)
	}
	second := decodeJSON[appRecordView](t, res)
	if blk := nextBlock(blocks, 2*time.Second); !strings.Contains(blk, "event: app") || !strings.Contains(blk, `"type":"record.upsert"`) || !strings.Contains(blk, "Second") {
		t.Fatalf("event %q", blk)
	}

	// Optimistic concurrency and merge.
	if res := rig.do(t, f.owner, http.MethodPatch, appPath(node.ID, "/records/"+second.ID), map[string]any{"merge": map[string]any{"done": true}, "if_rev": 7}); res.code != http.StatusConflict {
		t.Fatalf("stale rev: %d %s", res.code, res.body)
	}
	res = rig.do(t, f.owner, http.MethodPatch, appPath(node.ID, "/records/"+second.ID), map[string]any{"merge": map[string]any{"done": true, "text": nil}, "if_rev": second.Rev})
	merged := decodeJSON[appRecordView](t, res)
	if res.code != http.StatusOK || merged.Rev != second.Rev+1 || merged.Data["done"] != true || merged.Data["text"] != nil {
		t.Fatalf("merge: %d %+v", res.code, merged)
	}
	if res := rig.do(t, f.owner, http.MethodPatch, appPath(node.ID, "/records/"+second.ID), map[string]any{"data": map[string]any{"a": 1}, "merge": map[string]any{"b": 2}}); res.code != http.StatusBadRequest {
		t.Fatalf("data and merge together: %d", res.code)
	}

	// A notification about a record, worded by the bell from the manifest.
	if res := rig.do(t, f.owner, http.MethodPost, appPath(node.ID, "/notify"), map[string]any{"to": uuidString(f.member.ID), "verb": "assigned", "excerpt": "Second", "subject": "record:00000000-0000-0000-0000-000000000000"}); res.code != http.StatusBadRequest {
		t.Fatalf("notify about a missing record: %d", res.code)
	}
	if res := rig.do(t, f.owner, http.MethodPost, appPath(node.ID, "/notify"), map[string]any{"to": uuidString(f.member.ID), "verb": "assigned", "excerpt": "Second", "subject": "record:" + second.ID}); res.code != http.StatusNoContent {
		t.Fatalf("notify: %d %s", res.code, res.body)
	}
	var notes int
	if err := rig.srv.store.DB().QueryRow(ctx, `SELECT count(*) FROM notifications WHERE user_id = $1 AND kind = $2`, f.member.ID, "ext:"+testAppID+":assigned").Scan(&notes); err != nil || notes != 1 {
		t.Fatalf("notification rows: %d %v", notes, err)
	}

	// A copy keeps every record under the same id.
	res = rig.do(t, f.owner, http.MethodPost, "/api/v1/nodes/"+node.ID+"/copy", map[string]string{})
	if res.code != http.StatusCreated && res.code != http.StatusOK {
		t.Fatalf("copy: %d %s", res.code, res.body)
	}
	cp := decodeJSON[nodeDTO](t, res)
	copied, _ := f.records(t, f.owner, cp.ID)
	if len(copied) != 2 || copied[0].ID != first.ID || copied[1].ID != second.ID {
		t.Fatalf("copied records: %+v", copied)
	}

	// An upgrade is a new digest; data and switches stay.
	res = f.upload(t, f.admin, appZip(t, appManifest("1.1.0", ""), "<p>v2</p>"), true)
	if res.code != http.StatusOK {
		t.Fatalf("upgrade: %d %s", res.code, res.body)
	}
	upgraded := decodeJSON[adminAppDTO](t, res)
	if upgraded.Digest == installed.Digest || upgraded.Version != "1.1.0" {
		t.Fatalf("upgraded: %+v", upgraded)
	}
	if res := rig.request(t, f.stranger, http.MethodGet, "/x/"+testAppID+"/"+installed.Digest+"/app.js", nil, nil); res.code != http.StatusNotFound {
		t.Fatalf("old digest after upgrade: %d", res.code)
	}
	if recs, code := f.records(t, f.owner, node.ID); code != http.StatusOK || len(recs) != 2 {
		t.Fatalf("records after upgrade: %d %d", code, len(recs))
	}

	// Removing with purge takes the records; the nodes stay, unclaimed.
	if res := rig.do(t, f.admin, http.MethodDelete, "/api/v1/admin/apps/"+testAppID+"?purge=true", nil); res.code != http.StatusNoContent {
		t.Fatalf("remove: %d %s", res.code, res.body)
	}
	if _, code := f.records(t, f.owner, node.ID); code != http.StatusNotFound {
		t.Fatalf("records after remove: %d", code)
	}
	var left int
	if err := rig.srv.store.DB().QueryRow(ctx, `SELECT count(*) FROM ext_records WHERE node_id = ANY($1::uuid[])`, []string{node.ID, cp.ID}).Scan(&left); err != nil || left != 0 {
		t.Fatalf("records left after purge: %d %v", left, err)
	}
	if res := rig.do(t, f.admin, http.MethodGet, "/api/v1/admin/apps/"+testAppID, nil); res.code != http.StatusNotFound {
		t.Fatalf("admin view after remove: %d", res.code)
	}
}

// The backend proxy carries a token naming the caller and nothing of their
// session; the backend calls back with that token and is re-authorized.
func TestAppBackendProxyAndCallback(t *testing.T) {
	type seen struct {
		path, query, auth, cookie string
	}
	got := make(chan seen, 4)
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got <- seen{r.URL.Path, r.URL.RawQuery, r.Header.Get("Authorization"), r.Header.Get("Cookie")}
		w.Header().Set("Set-Cookie", "evil=1")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer backend.Close()

	f := newAppFixture(t, "appbackend")
	rig := f.rig
	if res := f.upload(t, f.admin, appZip(t, appManifest("1.0.0", backend.URL+"/pp"), "<p>x</p>"), false); res.code != http.StatusCreated {
		t.Fatalf("install: %d %s", res.code, res.body)
	}
	if res := rig.do(t, f.admin, http.MethodPatch, "/api/v1/admin/extensions/"+testAppID, map[string]bool{"enabled": true}); res.code != http.StatusOK {
		t.Fatalf("enable: %d", res.code)
	}
	node := decodeJSON[nodeDTO](t, rig.do(t, f.owner, http.MethodPost, f.spacePath("/ext-nodes"), map[string]string{"ext_kind": testAppID + ".list", "name": "B"}))

	// No secret yet: the proxy will not call out unsigned.
	if res := rig.do(t, f.owner, http.MethodGet, appPath(node.ID, "/backend/hello"), nil); res.code != http.StatusServiceUnavailable {
		t.Fatalf("proxy without secret: %d %s", res.code, res.body)
	}
	res := rig.do(t, f.admin, http.MethodPost, "/api/v1/admin/apps/"+testAppID+"/backend-secret", nil)
	if res.code != http.StatusOK {
		t.Fatalf("rotate: %d %s", res.code, res.body)
	}
	secret := decodeJSON[struct {
		Secret string `json:"secret"`
	}](t, res).Secret
	if res := rig.do(t, f.admin, http.MethodGet, "/api/v1/admin/apps/"+testAppID, nil); !strings.Contains(string(res.body), `"has_secret":true`) || strings.Contains(string(res.body), secret) {
		t.Fatalf("admin view leaks or lacks the secret: %s", res.body)
	}

	res = rig.do(t, f.owner, http.MethodGet, appPath(node.ID, "/backend/hello?x=1"), nil)
	if res.code != http.StatusOK || string(res.body) != `{"ok":true}` || res.header.Get("Set-Cookie") != "" {
		t.Fatalf("proxy: %d %s %v", res.code, res.body, res.header)
	}
	call := <-got
	if call.path != "/pp/hello" || call.query != "x=1" || call.cookie != "" || !strings.HasPrefix(call.auth, "Bearer ") {
		t.Fatalf("backend saw %+v", call)
	}
	token := strings.TrimPrefix(call.auth, "Bearer ")
	claims, err := exthost.VerifyToken(secret, token, testAppID, time.Now())
	if err != nil || claims.Sub != uuidString(f.owner.ID) || claims.Node != node.ID || !claims.Caps.Edit || claims.Iss != rig.url {
		t.Fatalf("token: %v %+v", err, claims)
	}
	if _, err := exthost.VerifyToken("wrong", token, testAppID, time.Now()); err == nil {
		t.Fatal("token verified under the wrong secret")
	}
	if _, err := exthost.VerifyToken(secret, token, testAppID, time.Now().Add(exthost.TokenTTL+time.Second)); err == nil {
		t.Fatal("expired token verified")
	}

	// The backend calls back as the owner, with the token it was given.
	callback := func(tok, method, path string, body any) rigResponse {
		var rd io.Reader
		if body != nil {
			b, _ := json.Marshal(body)
			rd = bytes.NewReader(b)
		}
		req, _ := http.NewRequest(method, rig.url+path, rd)
		req.Header.Set("Authorization", "Bearer "+tok)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return rigResponse{code: resp.StatusCode, header: resp.Header, body: b}
	}
	cbPath := "/api/apps/" + testAppID + "/nodes/" + node.ID + "/records"
	if res := callback(token, http.MethodPost, cbPath, map[string]any{"collection": "items", "data": map[string]any{"text": "from backend"}}); res.code != http.StatusCreated {
		t.Fatalf("callback create: %d %s", res.code, res.body)
	}
	if res := callback(token+"x", http.MethodGet, cbPath, nil); res.code != http.StatusUnauthorized {
		t.Fatalf("callback with a bad token: %d", res.code)
	}
	other := decodeJSON[nodeDTO](t, rig.do(t, f.owner, http.MethodPost, f.spacePath("/ext-nodes"), map[string]string{"ext_kind": testAppID + ".list", "name": "C"}))
	if res := callback(token, http.MethodGet, "/api/apps/"+testAppID+"/nodes/"+other.ID+"/records", nil); res.code != http.StatusUnauthorized {
		t.Fatalf("callback for another node: %d", res.code)
	}

	// A viewer's token reads but cannot write: the caps are re-resolved.
	f.grant(t, node, f.member, share.LevelView)
	rig.do(t, f.member, http.MethodGet, appPath(node.ID, "/backend/x"), nil)
	viewerToken := strings.TrimPrefix((<-got).auth, "Bearer ")
	if res := callback(viewerToken, http.MethodGet, cbPath, nil); res.code != http.StatusOK || !strings.Contains(string(res.body), "from backend") {
		t.Fatalf("viewer callback read: %d %s", res.code, res.body)
	}
	if res := callback(viewerToken, http.MethodPost, cbPath, map[string]any{"collection": "items", "data": map[string]any{}}); res.code != http.StatusForbidden {
		t.Fatalf("viewer callback write: %d %s", res.code, res.body)
	}
}

func appStream(t *testing.T, rig *collabRig, u store.User, path string) (<-chan string, func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, rig.url+path, nil)
	req.Header.Set("Cookie", rig.cookie(u))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		cancel()
		t.Fatalf("stream %s: %d", path, resp.StatusCode)
	}
	blocks := make(chan string, 16)
	go func() {
		defer close(blocks)
		br := bufio.NewReader(resp.Body)
		var b strings.Builder
		for {
			line, err := br.ReadString('\n')
			if err != nil {
				return
			}
			if line == "\n" {
				if s := b.String(); !strings.HasPrefix(s, ":") && !strings.HasPrefix(s, "retry") {
					blocks <- s
				}
				b.Reset()
				continue
			}
			b.WriteString(line)
		}
	}()
	return blocks, func() { cancel(); _ = resp.Body.Close() }
}

func nextBlock(blocks <-chan string, d time.Duration) string {
	select {
	case b := <-blocks:
		return b
	case <-time.After(d):
		return fmt.Sprintf("timeout after %s", d)
	}
}
