package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nhatbui/pp-hub/apps/server/internal/collab"
	"github.com/nhatbui/pp-hub/apps/server/internal/share"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

// A block document's relative references (`![](images/a.png)`) resolve
// against its place in the tree, on real grants and versions: the production
// router against a real Postgres, or a loud skip.

// relativeRig is a folder holding a README beside the files it names, and a
// file outside the folder. The README's grantees hold nothing on the files.
type relativeRig struct {
	*collabRig
	f       collabFixture
	folder  store.Node
	readme  store.Node
	v1      store.NodeVersion
	png     []byte
	outside []byte
	nfd     []byte
	pdf     []byte
}

func newRelativeRig(t *testing.T) relativeRig {
	t.Helper()
	rig := newCollabRig(t, collab.Config{})
	f := rig.fixture(t, "relative", "")
	ctx := context.Background()
	folder, err := rig.srv.fs.Mkdir(ctx, f.space.ID, pgtype.UUID{}, "Team docs", f.owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	images, err := rig.srv.fs.Mkdir(ctx, f.space.ID, folder.ID, "images", f.owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	r := relativeRig{collabRig: rig, f: f, folder: folder,
		png: onePixelPNG(t), outside: randomBytes(t, 40), nfd: randomBytes(t, 50), pdf: randomBytes(t, 60)}
	put := func(parent pgtype.UUID, name string, body []byte) store.Node {
		n, _, err := rig.srv.fs.PutFile(ctx, f.space.ID, parent, pgtype.UUID{}, name, bytes.NewReader(body), f.owner.ID)
		if err != nil {
			t.Fatalf("put %s: %v", name, err)
		}
		return n
	}
	put(images.ID, "a.png", r.png)
	put(folder.ID, "spec.pdf", r.pdf)
	// Decomposed, as Finder names it; the markdown spells it composed.
	put(folder.ID, "Bu\u0300i.png", r.nfd)
	put(pgtype.UUID{}, "outside.png", r.outside)
	r.readme = put(folder.ID, "README.md", []byte("# Readme\n\n![a](images/a.png)\n"))
	r.v1 = rig.versions(t, r.readme)[0]
	grantDirect(t, rig.srv, r.readme, f.editor, f.owner, share.GrantCaps{Level: share.LevelEdit})
	grantDirect(t, rig.srv, r.readme, f.reader, f.owner, share.GrantCaps{Level: share.LevelView, CanDownload: true})
	grantDirect(t, rig.srv, r.readme, f.viewer, f.owner, share.GrantCaps{Level: share.LevelView})
	return r
}

func relativePath(doc store.Node, rest, ref string) string {
	return nodePath(doc, rest) + "?path=" + url.QueryEscape(ref)
}

// A reader of the document sees what it names inside its folder when the
// author of the version being read may download it — and nothing else they
// could not read themselves: not outside the folder, not what a later
// author without access names, not from a document with no folder above it.
func TestRelativeReferencesFollowTheDocumentsReach(t *testing.T) {
	r := newRelativeRig(t)
	f := r.f

	res := r.do(t, f.viewer, http.MethodGet, relativePath(r.readme, "/relative", "./images/a.png"), nil)
	if res.code != http.StatusOK || !bytes.Equal(res.body, r.png) {
		t.Fatalf("image through the document's reach: %d %s", res.code, res.body)
	}
	for h, want := range map[string]string{
		"Content-Type":           "image/png",
		"X-Content-Type-Options": "nosniff",
		"Cache-Control":          "private, no-cache",
	} {
		if v := res.header.Get(h); v != want {
			t.Fatalf("%s %q, want %q", h, v, want)
		}
	}
	if cd := res.header.Get("Content-Disposition"); !strings.HasPrefix(cd, "inline") {
		t.Fatalf("Content-Disposition %q, want inline", cd)
	}
	if n := countReads(t, r.folderChild(t, "images", "a.png"), "file_viewed", "document"); n != 1 {
		t.Fatalf("%d reads of the image naming the document, want 1", n)
	}

	for name, tc := range map[string]struct {
		u    store.User
		ref  string
		want int
	}{
		"composed name, decomposed file":  {f.viewer, "B%C3%B9i.png", http.StatusOK},
		"a pdf downloads":                 {f.viewer, "spec.pdf", http.StatusOK},
		"outside the folder, reader":      {f.reader, "../outside.png", http.StatusNotFound},
		"outside the folder, its owner":   {f.owner, "../outside.png", http.StatusOK},
		"a folder":                        {f.owner, "images", http.StatusNotFound},
		"nothing there":                   {f.owner, "images/b.png", http.StatusNotFound},
		"above the space":                 {f.owner, "../../a.png", http.StatusNotFound},
		"not relative":                    {f.owner, "https://example.com/a.png", http.StatusBadRequest},
		"an attachment":                   {f.owner, "attachment:abc", http.StatusBadRequest},
		"rooted":                          {f.owner, "/images/a.png", http.StatusBadRequest},
		"no access to the document":       {f.stranger, "images/a.png", http.StatusForbidden},
		"folded dot-dot stays inside":     {f.viewer, "images/../images/a.png", http.StatusOK},
		"query and fragment are not path": {f.viewer, "images/a.png?v=2#x", http.StatusOK},
	} {
		if res := r.do(t, tc.u, http.MethodGet, relativePath(r.readme, "/relative", tc.ref), nil); res.code != tc.want {
			t.Errorf("%s: %d, want %d (%s)", name, res.code, tc.want, res.body)
		}
	}
	if res := r.do(t, f.viewer, http.MethodGet, relativePath(r.readme, "/relative", "spec.pdf"), nil); !strings.HasPrefix(res.header.Get("Content-Disposition"), "attachment") {
		t.Fatalf("pdf Content-Disposition %q, want attachment", res.header.Get("Content-Disposition"))
	}

	// An editor handed only the README names a neighbour: their version
	// reaches nothing they cannot download, so it shows nothing new — and
	// takes the image the owner's version showed with it.
	r.putVersion(t, r.readme, f.editor, "# Readme\n\n![a](images/a.png)\n\n![s](../outside.png)\n")
	if res := r.do(t, f.viewer, http.MethodGet, relativePath(r.readme, "/relative", "images/a.png"), nil); res.code != http.StatusNotFound {
		t.Fatalf("image through a version by someone without access: %d, want 404", res.code)
	}
	if res := r.do(t, f.editor, http.MethodGet, relativePath(r.readme, "/relative", "images/a.png"), nil); res.code != http.StatusNotFound {
		t.Fatalf("the editor reading their own reference: %d, want 404", res.code)
	}
	old := relativePath(r.readme, "/relative", "images/a.png") + "&version=" + uuidString(r.v1.ID)
	if res := r.do(t, f.viewer, http.MethodGet, old, nil); res.code != http.StatusOK {
		t.Fatalf("image through the owner's version: %d, want 200", res.code)
	}
	if res := r.do(t, f.viewer, http.MethodGet, relativePath(r.readme, "/relative", "images/a.png")+"&version="+uuidString(f.doc.ID), nil); res.code != http.StatusNotFound {
		t.Fatalf("another node's id as the version: %d, want 404", res.code)
	}

	// A document with no folder above it reaches nothing on its own.
	top := r.putMarkdown(t, f.space, f.owner, "top.md", "![o](outside.png)\n")
	grantDirect(t, r.srv, top, f.reader, f.owner, share.GrantCaps{Level: share.LevelView, CanDownload: true})
	if res := r.do(t, f.reader, http.MethodGet, relativePath(top, "/relative", "outside.png"), nil); res.code != http.StatusNotFound {
		t.Fatalf("top-level document's neighbour: %d, want 404", res.code)
	}
	if res := r.do(t, f.owner, http.MethodGet, relativePath(top, "/relative", "outside.png"), nil); res.code != http.StatusOK {
		t.Fatalf("top-level document's neighbour, its owner: %d, want 200", res.code)
	}
}

// folderChild is the live node named name under the folder's child dir.
func (r relativeRig) folderChild(t *testing.T, dir, name string) store.Node {
	t.Helper()
	ctx := context.Background()
	d, err := r.q.GetLiveChildByName(ctx, store.GetLiveChildByNameParams{SpaceID: r.f.space.ID, ParentID: r.folder.ID, Name: dir})
	if err != nil {
		t.Fatal(err)
	}
	n, err := r.q.GetLiveChildByName(ctx, store.GetLiveChildByNameParams{SpaceID: r.f.space.ID, ParentID: d.ID, Name: name})
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// A link to the document shows what the document reaches; a link names only
// what it covers itself, for the visitor to open.
func TestRelativeReferencesThroughALink(t *testing.T) {
	r := newRelativeRig(t)
	ctx := context.Background()
	_, docToken, err := r.srv.share.CreateLink(ctx, r.readme.ID, false, pgtype.Timestamptz{}, r.f.owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, folderToken, err := r.srv.share.CreateLink(ctx, r.folder.ID, false, pgtype.Timestamptz{}, r.f.owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	q := func(ref string) string { return "?path=" + url.QueryEscape(ref) }
	res := r.anon(t, "/s/"+docToken+"/relative"+q("images/a.png"), nil)
	if res.code != http.StatusOK || !bytes.Equal(res.body, r.png) {
		t.Fatalf("image through a link to the document: %d %s", res.code, res.body)
	}
	node := "&node=" + uuidString(r.readme.ID)
	for name, tc := range map[string]struct {
		path string
		want int
	}{
		"outside the folder":                  {"/s/" + docToken + "/relative" + q("../outside.png"), http.StatusNotFound},
		"meta outside a link to the document": {"/s/" + docToken + "/relative/meta" + q("images/a.png"), http.StatusNotFound},
		"meta inside a link to the folder":    {"/s/" + folderToken + "/relative/meta" + q("images/a.png") + node, http.StatusOK},
		"bytes inside a link to the folder":   {"/s/" + folderToken + "/relative" + q("images/a.png") + node, http.StatusOK},
		"a link to the folder, outside it":    {"/s/" + folderToken + "/relative" + q("../outside.png") + node, http.StatusNotFound},
		"the folder itself is no document":    {"/s/" + folderToken + "/relative" + q("images/a.png"), http.StatusBadRequest},
		"an invalid token":                    {"/s/nope/relative" + q("images/a.png"), http.StatusNotFound},
	} {
		if res := r.anon(t, tc.path, nil); res.code != tc.want {
			t.Errorf("%s: %d, want %d (%s)", name, res.code, tc.want, res.body)
		}
	}
}

// What a relative link names is answered only to someone who may open it.
func TestRelativeMetaNamesOnlyWhatTheReaderCanOpen(t *testing.T) {
	r := newRelativeRig(t)
	var got relativeNodeDTO
	res := r.do(t, r.f.owner, http.MethodGet, relativePath(r.readme, "/relative/meta", "images"), nil)
	if res.code != http.StatusOK {
		t.Fatalf("meta of a folder: %d %s", res.code, res.body)
	}
	if err := json.Unmarshal(res.body, &got); err != nil || got.Kind != "dir" || got.Name != "images" {
		t.Fatalf("meta of a folder: %+v %v", got, err)
	}
	res = r.do(t, r.f.owner, http.MethodGet, relativePath(r.readme, "/relative/meta", "README.md#intro"), nil)
	if err := json.Unmarshal(res.body, &got); err != nil || !got.Markdown || got.ID != uuidString(r.readme.ID) {
		t.Fatalf("meta of a page: %d %+v", res.code, got)
	}
	if res := r.do(t, r.f.viewer, http.MethodGet, relativePath(r.readme, "/relative/meta", "images/a.png"), nil); res.code != http.StatusNotFound {
		t.Fatalf("meta of a file the reader cannot open: %d, want 404", res.code)
	}
}

// Embedding copies the file into the document's attachments, for whoever may
// edit the document and download the file — never on the document's reach.
func TestRelativeEmbedTakesTheCallersOwnDownload(t *testing.T) {
	r := newRelativeRig(t)
	body := map[string]string{"path": "images/a.png"}
	if res := r.do(t, r.f.editor, http.MethodPost, nodePath(r.readme, "/relative/embed"), body); res.code != http.StatusNotFound {
		t.Fatalf("embed by an editor who cannot read the file: %d, want 404", res.code)
	}
	if res := r.do(t, r.f.reader, http.MethodPost, nodePath(r.readme, "/relative/embed"), body); res.code != http.StatusForbidden {
		t.Fatalf("embed by a reader: %d, want 403", res.code)
	}
	res := r.do(t, r.f.owner, http.MethodPost, nodePath(r.readme, "/relative/embed"), body)
	if res.code != http.StatusOK {
		t.Fatalf("embed: %d %s", res.code, res.body)
	}
	var got relativeEmbedDTO
	if err := json.Unmarshal(res.body, &got); err != nil || !strings.HasPrefix(got.Src, "attachment:") || got.Mime != "image/png" {
		t.Fatalf("embed answer: %+v %v", got, err)
	}
	again := r.do(t, r.f.owner, http.MethodPost, nodePath(r.readme, "/relative/embed"), body)
	var second relativeEmbedDTO
	_ = json.Unmarshal(again.body, &second)
	if second.Src != got.Src {
		t.Fatalf("embedding twice: %s then %s, want the same attachment", got.Src, second.Src)
	}
	hash := strings.TrimPrefix(got.Src, "attachment:")
	att := r.do(t, r.f.viewer, http.MethodGet, nodePath(r.readme, "/attachments/"+hash), nil)
	if att.code != http.StatusOK || !bytes.Equal(att.body, r.png) {
		t.Fatalf("embedded attachment: %d", att.code)
	}
	if res := r.do(t, r.f.owner, http.MethodPost, nodePath(r.readme, "/relative/embed"), map[string]string{"path": "images"}); res.code != http.StatusBadRequest {
		t.Fatalf("embed a folder: %d, want 400", res.code)
	}
}

// An export brings what the document's relative references reach for the
// caller into assets/, and rewrites them; what it cannot reach stays as
// written.
func TestExportCarriesRelativeReferences(t *testing.T) {
	r := newRelativeRig(t)
	r.putVersion(t, r.readme, r.f.owner, "# Readme\n\n![a](images/a.png)\n\n![o](../outside.png)\n\n[spec](spec.pdf#page=2)\n")
	res := r.do(t, r.f.reader, http.MethodGet, nodePath(r.readme, "/export"), nil)
	if res.code != http.StatusOK {
		t.Fatalf("export: %d %s", res.code, res.body)
	}
	files := readZip(t, res.body)
	if !bytes.Equal(files["assets/a.png"], r.png) || !bytes.Equal(files["assets/spec.pdf"], r.pdf) {
		t.Fatalf("assets: %v", keys(files))
	}
	if _, ok := files["assets/outside.png"]; ok {
		t.Fatalf("a file outside the document's reach was exported: %v", keys(files))
	}
	md := string(files["README.md"])
	for _, want := range []string{"](assets/a.png)", "](../outside.png)", "](assets/spec.pdf#page=2)"} {
		if !strings.Contains(md, want) {
			t.Fatalf("markdown lacks %q:\n%s", want, md)
		}
	}
}
