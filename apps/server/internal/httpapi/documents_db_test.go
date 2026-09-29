package httpapi

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/reearth/ygo/crdt"
	ygsync "github.com/reearth/ygo/sync"

	"github.com/nhatbui/pp-hub/apps/server/internal/blob"
	"github.com/nhatbui/pp-hub/apps/server/internal/blockdoc"
	"github.com/nhatbui/pp-hub/apps/server/internal/collab"
	"github.com/nhatbui/pp-hub/apps/server/internal/mdoc"
	"github.com/nhatbui/pp-hub/apps/server/internal/share"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
	"github.com/nhatbui/pp-hub/apps/server/internal/unfurl"
)

// Block documents outside the editor — the read-only document tree, share
// links, export, templates, link bookmarks, attachments adopted on
// write-back — resolve against real grants, links, versions and attachment
// rows: the production router against a real Postgres, or a loud skip.

func nodePath(n store.Node, rest string) string {
	return "/api/v1/nodes/" + uuidString(n.ID) + rest
}

// anon requests path with no session, as a link visitor does.
func (rig *collabRig) anon(t *testing.T, path string, hdr map[string]string) rigResponse {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, rig.url+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return rigResponse{code: resp.StatusCode, header: resp.Header, body: b}
}

// mustAttach attaches body to doc as u and answers its hash.
func (rig *collabRig) mustAttach(t *testing.T, u store.User, doc store.Node, name string, body []byte) string {
	t.Helper()
	res := rig.attach(t, u, doc, name, body)
	if res.code != http.StatusCreated {
		t.Fatalf("attach %s: %d %s", name, res.code, res.body)
	}
	return res.header.Get("Upload-Attachment")
}

func decodeDocument(t *testing.T, res rigResponse) documentDTO {
	t.Helper()
	var dto documentDTO
	if err := json.Unmarshal(res.body, &dto); err != nil {
		t.Fatalf("decode document: %v; body=%s", err, res.body)
	}
	return dto
}

// docText is every text node of a document tree, concatenated.
func docText(n any) string {
	var b strings.Builder
	var walk func(v any)
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			if s, ok := x["text"].(string); ok {
				b.WriteString(s)
			}
			walk(x["content"])
		case []any:
			for _, c := range x {
				walk(c)
			}
		}
	}
	raw, _ := json.Marshal(n)
	var v any
	_ = json.Unmarshal(raw, &v)
	walk(v)
	return b.String()
}

// The document tree is the whole document, so it needs view — reading is what
// it is for. It reads the current version or one of the node's own, and
// refuses what is not a block document or is too big to parse.
func TestDocumentTreeFollowsDownloadAndVersions(t *testing.T) {
	rig := newCollabRig(t, collab.Config{})
	f := rig.fixture(t, "doctree", "# First\n\nOne.\n")
	first := rig.versions(t, f.doc)[0]
	rig.putVersion(t, f.doc, f.owner, "# Second\n\nTwo.\n")
	other := rig.putMarkdown(t, f.space, f.owner, "other.md", "# Other\n")
	otherVer := rig.versions(t, other)[0]

	for _, tc := range []struct {
		name string
		u    store.User
		want int
	}{
		{"owner", f.owner, http.StatusOK},
		{"editor", f.editor, http.StatusOK},
		{"reader", f.reader, http.StatusOK},
		{"view only", f.viewer, http.StatusOK},
		{"member without a grant", f.stranger, http.StatusForbidden},
	} {
		res := rig.do(t, tc.u, http.MethodGet, nodePath(f.doc, "/document"), nil)
		if res.code != tc.want {
			t.Fatalf("as %s: %d, want %d; body=%s", tc.name, res.code, tc.want, res.body)
		}
	}

	cur := decodeDocument(t, rig.do(t, f.reader, http.MethodGet, nodePath(f.doc, "/document"), nil))
	if cur.Version != 2 || cur.Lossy || cur.Doc.Type != "doc" || !strings.Contains(docText(cur.Doc), "Two.") {
		t.Fatalf("current: version %d lossy %v text %q", cur.Version, cur.Lossy, docText(cur.Doc))
	}
	old := decodeDocument(t, rig.do(t, f.reader, http.MethodGet, nodePath(f.doc, "/document?version="+uuidString(first.ID)), nil))
	if old.Version != 1 || !strings.Contains(docText(old.Doc), "One.") {
		t.Fatalf("version 1: version %d text %q", old.Version, docText(old.Doc))
	}
	if res := rig.do(t, f.reader, http.MethodGet, nodePath(f.doc, "/document?version="+uuidString(otherVer.ID)), nil); res.code != http.StatusNotFound {
		t.Fatalf("another node's version: %d, want 404", res.code)
	}
	if res := rig.do(t, f.reader, http.MethodGet, nodePath(f.doc, "/document?version=nope"), nil); res.code != http.StatusBadRequest {
		t.Fatalf("a malformed version: %d, want 400", res.code)
	}

	txt := rig.putMarkdown(t, f.space, f.owner, "plain.txt", "plain\n")
	if res := rig.do(t, f.owner, http.MethodGet, nodePath(txt, "/document"), nil); res.code != http.StatusBadRequest {
		t.Fatalf("a text file: %d, want 400", res.code)
	}
	lossy := rig.putMarkdown(t, f.space, f.owner, "lossy.md", "Text with a footnote[^1].\n\n[^1]: The note.\n")
	if dto := decodeDocument(t, rig.do(t, f.owner, http.MethodGet, nodePath(lossy, "/document"), nil)); !dto.Lossy {
		t.Fatal("a document with a footnote is not flagged lossy")
	}
	huge := rig.putMarkdown(t, f.space, f.owner, "huge.md", strings.Repeat("a", blockdoc.MaxBytes+1))
	if res := rig.do(t, f.owner, http.MethodGet, nodePath(huge, "/document"), nil); res.code != http.StatusRequestEntityTooLarge {
		t.Fatalf("a document over the limit: %d, want 413", res.code)
	}
}

// Through a link, the document and its attachments are served whenever the
// link covers the document — a link always grants view — and only the
// attachments that document holds. The download flag is not consulted.
func TestShareLinkServesDocumentAndAttachments(t *testing.T) {
	rig := newCollabRig(t, collab.Config{})
	ctx := context.Background()
	f := rig.fixture(t, "sharedoc", "# Outside\n")
	dir, err := rig.srv.fs.Mkdir(ctx, f.space.ID, pgtype.UUID{}, "shared", f.owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	inside, _, err := rig.srv.fs.PutFile(ctx, f.space.ID, dir.ID, pgtype.UUID{}, "inside.md", strings.NewReader("# Inside\n\nLinked.\n"), f.owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	png := onePixelPNG(t)
	hash := rig.mustAttach(t, f.owner, inside, "photo.png", png)
	outsideHash := rig.mustAttach(t, f.owner, f.doc, "other.png", randomBytes(t, 64))

	_, token, err := rig.srv.share.CreateLink(ctx, dir.ID, true, pgtype.Timestamptz{}, f.owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, noDownload, err := rig.srv.share.CreateLink(ctx, dir.ID, false, pgtype.Timestamptz{}, f.owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, fileToken, err := rig.srv.share.CreateLink(ctx, inside.ID, true, pgtype.Timestamptz{}, f.owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	q := "?node=" + uuidString(inside.ID)

	res := rig.anon(t, "/s/"+token+"/document"+q, nil)
	if res.code != http.StatusOK {
		t.Fatalf("document through the link: %d %s", res.code, res.body)
	}
	if dto := decodeDocument(t, res); !strings.Contains(docText(dto.Doc), "Linked.") || dto.Version != 1 {
		t.Fatalf("document through the link: %+v", dto)
	}
	if res := rig.anon(t, "/s/"+fileToken+"/document", nil); res.code != http.StatusOK {
		t.Fatalf("a link to the document itself, no ?node: %d", res.code)
	}
	att := rig.anon(t, "/s/"+token+"/attachments/"+hash+q, nil)
	if att.code != http.StatusOK || !bytes.Equal(att.body, png) {
		t.Fatalf("attachment through the link: %d, %d bytes", att.code, len(att.body))
	}
	for h, want := range map[string]string{
		"Content-Type":           "image/png",
		"X-Content-Type-Options": "nosniff",
		"Cache-Control":          "private, max-age=31536000, immutable",
	} {
		if v := att.header.Get(h); v != want {
			t.Fatalf("attachment through the link: %s %q, want %q", h, v, want)
		}
	}

	for name, tc := range map[string]struct {
		path string
		want int
	}{
		"document, no download":            {"/s/" + noDownload + "/document" + q, http.StatusOK},
		"attachment, no download":          {"/s/" + noDownload + "/attachments/" + hash + q, http.StatusOK},
		"document, no download, outside":   {"/s/" + noDownload + "/document?node=" + uuidString(f.doc.ID), http.StatusNotFound},
		"document outside the link":        {"/s/" + token + "/document?node=" + uuidString(f.doc.ID), http.StatusNotFound},
		"attachment of a doc outside":      {"/s/" + token + "/attachments/" + outsideHash + "?node=" + uuidString(f.doc.ID), http.StatusNotFound},
		"another document's hash":          {"/s/" + token + "/attachments/" + outsideHash + q, http.StatusNotFound},
		"the folder itself":                {"/s/" + token + "/document", http.StatusBadRequest},
		"an invalid token":                 {"/s/nope/document" + q, http.StatusNotFound},
		"the file link naming another doc": {"/s/" + fileToken + "/document?node=" + uuidString(f.doc.ID), http.StatusNotFound},
	} {
		if res := rig.anon(t, tc.path, nil); res.code != tc.want {
			t.Errorf("%s: %d, want %d", name, res.code, tc.want)
		}
	}
}

// An office document inside a shared link can mint an OnlyOffice view session.
func TestShareLinkOfficeEditSession(t *testing.T) {
	rig := newCollabRig(t, collab.Config{})
	ctx := context.Background()
	f := rig.fixture(t, "shareoffice", "# Not office\n")
	sheet, _, err := rig.srv.fs.PutFile(ctx, f.space.ID, pgtype.UUID{}, pgtype.UUID{}, "model.xlsx", strings.NewReader("dummy content"), f.owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, token, err := rig.srv.share.CreateLink(ctx, sheet.ID, false, pgtype.Timestamptz{}, f.owner.ID)
	if err != nil {
		t.Fatal(err)
	}

	res := rig.anon(t, "/s/"+token+"/edit-session", nil)
	if res.code != http.StatusOK {
		t.Fatalf("edit-session through link: %d %s", res.code, res.body)
	}
	var out struct {
		EditorURL string `json:"editor_url"`
		Token     string `json:"token"`
		CanWrite  bool   `json:"can_write"`
	}
	if err := json.Unmarshal(res.body, &out); err != nil {
		t.Fatalf("unmarshal edit-session: %v", err)
	}
	if out.CanWrite {
		t.Error("link edit-session must be read-only (can_write=false)")
	}
	if !strings.Contains(out.EditorURL, "/hosting/wopi/cell/view") {
		t.Errorf("expected cell view editor URL, got: %q", out.EditorURL)
	}
	if out.Token == "" {
		t.Error("missing WOPI token")
	}

	// Markdown file is not supported by office editor.
	_, docToken, err := rig.srv.share.CreateLink(ctx, f.doc.ID, false, pgtype.Timestamptz{}, f.owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	bad := rig.anon(t, "/s/"+docToken+"/edit-session", nil)
	if bad.code != http.StatusBadRequest {
		t.Errorf("markdown in office edit-session: %d, want 400", bad.code)
	}
}

// The export is the markdown with each attachment it references and holds
// rewritten to a unique file under assets/, those files, and nothing else.
// A lossy document keeps its bytes; only the references change.
func TestExportZipsDocumentWithAssets(t *testing.T) {
	rig := newCollabRig(t, collab.Config{})
	f := rig.fixture(t, "export", "")
	a := onePixelPNG(t)
	b := randomBytes(t, 300)
	c := randomBytes(t, 200)
	ha := rig.mustAttach(t, f.owner, f.doc, "photo.png", a)
	hb := rig.mustAttach(t, f.owner, f.doc, "Photo.png", b) // collides case-insensitively
	rig.mustAttach(t, f.owner, f.doc, "unused.bin", c)      // held, never referenced: not exported
	foreign := blob.Hash([]byte("held by nobody here"))
	rig.putVersion(t, f.doc, f.owner, "# Report\n\n![first](attachment:"+ha+")\n\n![second](attachment:"+hb+")\n\n![again](attachment:"+ha+")\n\n![gone](attachment:"+foreign+")\n")

	if res := rig.do(t, f.viewer, http.MethodGet, nodePath(f.doc, "/export"), nil); res.code != http.StatusForbidden {
		t.Fatalf("export as view-only: %d, want 403", res.code)
	}
	res := rig.do(t, f.reader, http.MethodGet, nodePath(f.doc, "/export"), nil)
	if res.code != http.StatusOK || res.header.Get("Content-Type") != "application/zip" {
		t.Fatalf("export: %d %s", res.code, res.header.Get("Content-Type"))
	}
	if cd := res.header.Get("Content-Disposition"); !strings.Contains(cd, `filename=export.zip`) {
		t.Fatalf("Content-Disposition %q, want export.zip", cd)
	}
	files := readZip(t, res.body)
	if len(files) != 3 {
		t.Fatalf("entries %v, want the markdown and two assets", keys(files))
	}
	if !bytes.Equal(files["assets/photo.png"], a) || !bytes.Equal(files["assets/Photo-2.png"], b) {
		t.Fatalf("assets: %v", keys(files))
	}
	md := string(files["export.md"])
	for _, want := range []string{"](assets/photo.png)", "](assets/Photo-2.png)", "](attachment:" + foreign + ")"} {
		if !strings.Contains(md, want) {
			t.Fatalf("markdown lacks %q:\n%s", want, md)
		}
	}
	if strings.Contains(md, "attachment:"+ha) || strings.Contains(md, "attachment:"+hb) {
		t.Fatalf("a held attachment kept its attachment: reference:\n%s", md)
	}
	if n := countReads(t, f.doc, "file_downloaded", "export"); n != 1 {
		t.Fatalf("%d export download events, want 1", n)
	}

	// Lossy: the footnote and the raw HTML survive the export untouched.
	lossySrc := "Intro<sup>1</sup> text[^n].\n\n![p](attachment:" + ha + ")\n\n[^n]: A note.\n"
	lossy := rig.putMarkdown(t, f.space, f.owner, "lossy notes.md", lossySrc)
	rig.mustAttach(t, f.owner, lossy, "my photo (1).png", a)
	res = rig.do(t, f.owner, http.MethodGet, nodePath(lossy, "/export"), nil)
	if res.code != http.StatusOK {
		t.Fatalf("export lossy: %d %s", res.code, res.body)
	}
	files = readZip(t, res.body)
	want := strings.ReplaceAll(lossySrc, "attachment:"+ha, "assets/my%20photo%20%281%29.png")
	if got := string(files["lossy notes.md"]); got != want {
		t.Fatalf("lossy export:\n%s\nwant:\n%s", got, want)
	}
	if !bytes.Equal(files["assets/my photo (1).png"], a) {
		t.Fatalf("lossy export assets: %v", keys(files))
	}
}

func readZip(t *testing.T, body []byte) map[string][]byte {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatalf("zip: %v", err)
	}
	out := map[string][]byte{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		b, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatal(err)
		}
		out[f.Name] = b
	}
	return out
}

func keys(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// countReads counts node's read events of kind whose detail names key.
func countReads(t *testing.T, node store.Node, kind, key string) int {
	t.Helper()
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, os.Getenv("PP_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	t.Cleanup(func() {
		p, err := pgxpool.New(context.Background(), os.Getenv("PP_TEST_DATABASE_URL"))
		if err == nil {
			_, _ = p.Exec(context.Background(), `DELETE FROM security_events WHERE node_id = $1`, node.ID)
			p.Close()
		}
	})
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM security_events WHERE node_id = $1 AND kind = $2 AND detail ? $3`,
		node.ID, kind, key).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// A document made from a template holds exactly the template, in the
// language asked for, and opens editable: not lossy.
func TestCreateDocumentFromTemplate(t *testing.T) {
	rig := newCollabRig(t, collab.Config{})
	f := rig.fixture(t, "template", "")

	var list []blockdoc.Template
	res := rig.do(t, f.owner, http.MethodGet, "/api/v1/templates/markdown?locale=vi-VN", nil)
	if res.code != http.StatusOK || json.Unmarshal(res.body, &list) != nil {
		t.Fatalf("list vi: %d %s", res.code, res.body)
	}
	if want := blockdoc.Templates("vi"); len(list) != len(want) || list[0] != want[0] {
		t.Fatalf("vi catalog %+v", list)
	}
	res = rig.do(t, f.owner, http.MethodGet, "/api/v1/templates/markdown", nil)
	if err := json.Unmarshal(res.body, &list); err != nil || list[0] != blockdoc.Templates("en")[0] {
		t.Fatalf("default catalog (no saved locale): %s", res.body)
	}

	create := func(body map[string]string) rigResponse {
		return rig.do(t, f.owner, http.MethodPost, "/api/v1/spaces/"+uuidString(f.space.ID)+"/documents", body)
	}
	for _, locale := range []string{"en", "vi"} {
		for _, tpl := range blockdoc.Templates(locale) {
			res := create(map[string]string{"kind": "markdown", "name": locale + "-" + tpl.ID, "template": tpl.ID, "locale": locale})
			if res.code != http.StatusCreated {
				t.Fatalf("create %s/%s: %d %s", locale, tpl.ID, res.code, res.body)
			}
			var dto nodeDTO
			if err := json.Unmarshal(res.body, &dto); err != nil {
				t.Fatal(err)
			}
			var id pgtype.UUID
			if err := id.Scan(dto.ID); err != nil {
				t.Fatal(err)
			}
			doc, err := rig.q.GetNode(context.Background(), id)
			if err != nil {
				t.Fatal(err)
			}
			want, _ := blockdoc.TemplateContent(tpl.ID, locale)
			if got := rig.content(t, rig.versions(t, doc)[0]); got != string(want) {
				t.Fatalf("%s/%s content differs from the template", locale, tpl.ID)
			}
			if sess := rig.mustSession(t, f.owner, doc); sess.Lossy {
				t.Fatalf("%s/%s opens lossy", locale, tpl.ID)
			}
			if dto := decodeDocument(t, rig.do(t, f.owner, http.MethodGet, nodePath(doc, "/document"), nil)); dto.Lossy {
				t.Fatalf("%s/%s document tree lossy", locale, tpl.ID)
			}
		}
	}
	if res := create(map[string]string{"kind": "markdown", "name": "x", "template": "nope"}); res.code != http.StatusBadRequest {
		t.Fatalf("unknown template: %d, want 400", res.code)
	}
	if res := create(map[string]string{"kind": "document", "name": "x", "template": "outline"}); res.code != http.StatusBadRequest {
		t.Fatalf("a template on an office kind: %d, want 400", res.code)
	}
}

// A bookmark's page is fetched by the server for an editor of a markdown
// document, and its preview image becomes the document's attachment. The
// fetcher is the test seam: production's refuses this loopback page.
func TestUnfurlStoresThePreviewImage(t *testing.T) {
	rig := newCollabRig(t, collab.Config{})
	f := rig.fixture(t, "unfurl", "# Links\n")
	png := onePixelPNG(t)
	page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/post":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(`<html><head><meta property="og:title" content="A post"><meta property="og:description" content="About things"><meta property="og:image" content="/cover.png"></head></html>`))
		case "/cover.png":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(png)
		}
	}))
	defer page.Close()
	unfurlPath := nodePath(f.doc, "/unfurl")
	body := map[string]string{"url": page.URL + "/post"}

	// The production policy: loopback is refused before any byte is read.
	if res := rig.do(t, f.editor, http.MethodPost, unfurlPath, body); res.code != http.StatusBadRequest {
		t.Fatalf("production fetcher on loopback: %d %s, want 400", res.code, res.body)
	}

	rig.srv.unfurl = unfurl.New(unfurl.Config{AllowAddr: func(a netip.Addr) bool { return a.IsLoopback() }})
	for _, tc := range []struct {
		name string
		u    store.User
		want int
	}{
		{"reader", f.reader, http.StatusForbidden},
		{"view only", f.viewer, http.StatusForbidden},
		{"member without a grant", f.stranger, http.StatusForbidden},
	} {
		if res := rig.do(t, tc.u, http.MethodPost, unfurlPath, body); res.code != tc.want {
			t.Fatalf("as %s: %d, want %d", tc.name, res.code, tc.want)
		}
	}
	txt := rig.putMarkdown(t, f.space, f.owner, "plain.txt", "plain\n")
	if res := rig.do(t, f.owner, http.MethodPost, nodePath(txt, "/unfurl"), body); res.code != http.StatusBadRequest {
		t.Fatalf("a text file: %d, want 400", res.code)
	}
	if res := rig.do(t, f.editor, http.MethodPost, unfurlPath, map[string]string{"url": "file:///etc/passwd"}); res.code != http.StatusBadRequest {
		t.Fatalf("a file: URL: %d, want 400", res.code)
	}

	res := rig.do(t, f.editor, http.MethodPost, unfurlPath, body)
	if res.code != http.StatusOK {
		t.Fatalf("unfurl: %d %s", res.code, res.body)
	}
	var dto unfurlDTO
	if err := json.Unmarshal(res.body, &dto); err != nil {
		t.Fatal(err)
	}
	hash := blob.Hash(png)
	if dto.URL != page.URL+"/post" || dto.Title != "A post" || dto.Description != "About things" || dto.Thumbnail != "attachment:"+hash {
		t.Fatalf("unfurl %+v", dto)
	}
	got := rig.attachment(t, f.reader, f.doc, hash)
	if got.code != http.StatusOK || !bytes.Equal(got.body, png) {
		t.Fatalf("the stored preview image: %d", got.code)
	}
	att, err := rig.q.GetAttachment(context.Background(), store.GetAttachmentParams{NodeID: f.doc.ID, BlobHash: hash})
	if err != nil || att.CreatedBy != f.editor.ID || !strings.HasSuffix(att.Name, "-preview.png") {
		t.Fatalf("attachment row %+v, %v", att, err)
	}
}

// A video renders in place and seeks: the attachments route answers ranges.
func TestAttachmentServesVideoRanges(t *testing.T) {
	rig := newCollabRig(t, collab.Config{})
	f := rig.fixture(t, "video", "# Clip\n")
	clip := randomBytes(t, 4096)
	hash := rig.mustAttach(t, f.editor, f.doc, "clip.mp4", clip)

	res := rig.request(t, f.reader, http.MethodGet, nodePath(f.doc, "/attachments/"+hash), map[string]string{"Range": "bytes=100-199"}, nil)
	if res.code != http.StatusPartialContent || !bytes.Equal(res.body, clip[100:200]) {
		t.Fatalf("range: %d, %d bytes", res.code, len(res.body))
	}
	if cr := res.header.Get("Content-Range"); cr != "bytes 100-199/4096" {
		t.Fatalf("Content-Range %q", cr)
	}
	if ct, cd := res.header.Get("Content-Type"), res.header.Get("Content-Disposition"); ct != "video/mp4" || !strings.HasPrefix(cd, "inline") {
		t.Fatalf("a video must play in place: %s, %s", ct, cd)
	}
}

// A reference pasted from another document is adopted on write-back when its
// writer may download a document holding it — and only then.
func TestWriteBackAdoptsReadableAttachments(t *testing.T) {
	rig := newCollabRig(t, collab.Config{})
	ctx := context.Background()
	f := rig.fixture(t, "adopt", "# Source\n")
	png := onePixelPNG(t)
	hash := rig.mustAttach(t, f.owner, f.doc, "shared.png", png)
	pasted := "# Target\n\n![](attachment:" + hash + ")\n"

	for _, tc := range []struct {
		name    string
		writer  store.User
		adopted bool
	}{
		{"downloader of the source", f.reader, true},
		{"editor of the source", f.editor, true},
		{"view-only on the source", f.viewer, false},
		{"no access to the source", f.stranger, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			target := rig.putMarkdown(t, f.space, f.owner, "target-"+strings.ReplaceAll(tc.name, " ", "-")+".md", "# Target\n")
			grantDirect(t, rig.srv, target, tc.writer, f.owner, share.GrantCaps{Level: share.LevelEdit})
			p := rig.mustDial(t, tc.writer, rig.mustSession(t, tc.writer, target).Room)
			if !p.edit(t, pasted) {
				t.Fatal("edit refused")
			}
			waitUntil(t, "the paste to be written back", func() bool { return len(rig.versions(t, target)) == 2 })
			p.close()
			_, err := rig.q.GetAttachment(ctx, store.GetAttachmentParams{NodeID: target.ID, BlobHash: hash})
			if adopted := err == nil; adopted != tc.adopted {
				t.Fatalf("adopted %v (%v), want %v", adopted, err, tc.adopted)
			}
			code := rig.attachment(t, f.owner, target, hash).code
			if want := map[bool]int{true: http.StatusOK, false: http.StatusNotFound}[tc.adopted]; code != want {
				t.Fatalf("the target's attachment: %d, want %d", code, want)
			}
		})
	}
}

// formatFirstText applies marks to the first n characters of the peer's first
// text run — a change that inserts no character, only formatting (a comment
// anchored on saved text) — and reports whether the server applied it.
func (p *wsPeer) formatFirstText(t *testing.T, n int, attrs crdt.Attributes) bool {
	t.Helper()
	sv := p.doc.StateVector()
	var txt *crdt.YXmlText
	for _, c := range p.doc.GetXmlFragment(mdoc.FragmentName).Children() {
		el, ok := c.(*crdt.YXmlElement)
		if !ok {
			continue
		}
		for _, cc := range el.Children() {
			if x, ok := cc.(*crdt.YXmlText); ok && txt == nil {
				txt = x
			}
		}
	}
	if txt == nil {
		t.Fatal("no text run to format")
	}
	p.doc.Transact(func(txn *crdt.Transaction) { txt.Format(txn, 0, n, attrs) })
	idx := p.send(ygsync.EncodeUpdate(crdt.EncodeStateAsUpdateV1(p.doc, sv)))
	var applied bool
	waitUntil(t, "the server's acknowledgement", func() bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		if len(p.acks) > idx {
			applied = p.acks[idx]
			return true
		}
		return p.closed
	})
	return applied
}

// A change that only adds a mark — a comment anchored on text already saved —
// is an edit like any other: written back as its author's version when the
// room goes quiet, and on shutdown when it has not yet.
func TestWriteBackPersistsMarkOnlyChanges(t *testing.T) {
	comment := crdt.Attributes{"comment--abcdEFGH": map[string]any{"threadId": "thread-1"}}
	for _, tc := range []struct {
		name string
		cfg  collab.Config
	}{
		{"when the room goes quiet", collab.Config{}},
		{"on shutdown", collab.Config{CoalesceWindow: time.Hour, CoalesceMaxWait: time.Hour}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rig := newCollabRig(t, tc.cfg)
			f := rig.fixture(t, "markonly", "Hello world\n")
			p := rig.mustDial(t, f.editor, rig.mustSession(t, f.editor, f.doc).Room)
			if !p.formatFirstText(t, 5, comment) {
				t.Fatal("the mark was refused")
			}
			if tc.cfg.CoalesceWindow == time.Hour {
				rig.shutdown(t)
			}
			waitUntil(t, "the mark to be written back", func() bool { return len(rig.versions(t, f.doc)) == 2 })
			v := rig.versions(t, f.doc)[0]
			if got := rig.content(t, v); !strings.Contains(got, `"threadId":"thread-1"`) || v.CreatedBy != f.editor.ID {
				t.Fatalf("version by %s: %q", uuidString(v.CreatedBy), got)
			}
		})
	}
}
