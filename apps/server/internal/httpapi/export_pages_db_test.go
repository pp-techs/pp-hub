package httpapi

import (
	"bytes"
	"context"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nhatbui/pp-hub/apps/server/internal/collab"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

// exportPagesFixture is Doc with a sub-page Sub that holds Deep, Sub linking
// back to Doc and to a page outside the export, with an image and a cover.
type exportPagesFixture struct {
	doc, sub, deep, outside store.Node
	image, cover            []byte
}

func newExportPages(t *testing.T, rig *collabRig, f collabFixture, parent pgtype.UUID) exportPagesFixture {
	t.Helper()
	ctx := context.Background()
	put := func(name string, parent pgtype.UUID) store.Node {
		n, _, err := rig.srv.fs.PutFile(ctx, f.space.ID, parent, pgtype.UUID{}, name, strings.NewReader("draft\n"), f.owner.ID)
		if err != nil {
			t.Fatalf("put %s: %v", name, err)
		}
		return n
	}
	x := exportPagesFixture{image: onePixelPNG(t), cover: randomBytes(t, 120)}
	x.doc = put("Doc.md", parent)
	x.sub = put("Sub.md", x.doc.ID)
	x.deep = put("Deep.md", x.sub.ID)
	x.outside = rig.putMarkdown(t, f.space, f.owner, "Outside.md", "elsewhere\n")
	hi := rig.mustAttach(t, f.owner, x.sub, "pic.png", x.image)
	hc := rig.mustAttach(t, f.owner, x.sub, "cover.bin", x.cover)

	rig.putVersion(t, x.doc, f.owner, "# Doc\n\n"+subPageBlock(x.sub)+"\n")
	rig.putVersion(t, x.sub, f.owner, "---\nicon: 🌱\ncover: attachment:"+hc+"\n---\n"+
		"Back to "+pageLinkInline(x.doc)+" and "+pageLinkInline(x.outside)+".\n\n"+
		"![pic](attachment:"+hi+")\n\n"+subPageBlock(x.deep)+"\n")
	rig.putVersion(t, x.deep, f.owner, "Deep down "+pageLinkInline(x.sub)+".\n")
	return x
}

func subPageBlock(n store.Node) string {
	id := uuidString(n.ID)
	return `<!-- slash:subPage {"pageId":"` + id + `"} -->` + "\n[stale](/edit/" + id + ")"
}

func pageLinkInline(n store.Node) string {
	id := uuidString(n.ID)
	return `<!-- slash:pageLink {"pageId":"` + id + `"} -->[stale](/edit/` + id + `)<!-- /slash:pageLink -->`
}

// checkExportedPages asserts the pages' layout under prefix: Doc.md beside
// Doc/, its sub-pages there recursively, Sub's attachments and cover in the
// assets/ beside it, and every link between them relative.
func checkExportedPages(t *testing.T, files map[string][]byte, prefix, external string, x exportPagesFixture) {
	t.Helper()
	doc := string(files[prefix+"Doc.md"])
	if want := subPageMarker(x.sub) + "\n[Sub](Doc/Sub.md)"; !strings.Contains(doc, want) {
		t.Fatalf("Doc.md lacks %q:\n%s", want, doc)
	}
	sub := string(files[prefix+"Doc/Sub.md"])
	for _, want := range []string{
		"cover: assets/cover.bin\n",
		"icon: 🌱\n",
		pageLinkMarker(x.doc) + "[Doc](../Doc.md)<!-- /slash:pageLink -->",
		pageLinkMarker(x.outside) + "[" + strings.TrimSuffix(x.outside.Name, ".md") + "](" + external + "/edit/" + uuidString(x.outside.ID) + ")",
		"![pic](assets/pic.png)",
		subPageMarker(x.deep) + "\n[Deep](Sub/Deep.md)",
	} {
		if !strings.Contains(sub, want) {
			t.Fatalf("Sub.md lacks %q:\n%s", want, sub)
		}
	}
	if strings.Contains(sub, "attachment:") {
		t.Fatalf("Sub.md kept an attachment: reference:\n%s", sub)
	}
	deep := string(files[prefix+"Doc/Sub/Deep.md"])
	if want := "[Sub](../Sub.md)"; !strings.Contains(deep, want) {
		t.Fatalf("Deep.md lacks %q:\n%s", want, deep)
	}
	if !bytes.Equal(files[prefix+"Doc/assets/pic.png"], x.image) || !bytes.Equal(files[prefix+"Doc/assets/cover.bin"], x.cover) {
		t.Fatalf("Sub's assets missing: %v", keys(files))
	}
}

func subPageMarker(n store.Node) string {
	return `<!-- slash:subPage {"pageId":"` + uuidString(n.ID) + `"} -->`
}

func pageLinkMarker(n store.Node) string {
	return `<!-- slash:pageLink {"pageId":"` + uuidString(n.ID) + `"} -->`
}

func sortedKeys(m map[string][]byte) []string {
	k := keys(m)
	slices.Sort(k)
	return k
}

// A document exports with its sub-pages beneath it, recursively; links
// between pages in the archive are relative, others point into pp-hub.
func TestExportDocumentWithSubPages(t *testing.T) {
	rig := newCollabRig(t, collab.Config{})
	f := rig.fixture(t, "export-pages", "")
	x := newExportPages(t, rig, f, pgtype.UUID{})

	res := rig.do(t, f.owner, http.MethodGet, nodePath(x.doc, "/export"), nil)
	if res.code != http.StatusOK {
		t.Fatalf("export: %d %s", res.code, res.body)
	}
	if cd := res.header.Get("Content-Disposition"); !strings.Contains(cd, `filename=Doc.zip`) {
		t.Fatalf("Content-Disposition %q, want Doc.zip", cd)
	}
	files := readZip(t, res.body)
	want := []string{"Doc.md", "Doc/Sub.md", "Doc/Sub/Deep.md", "Doc/assets/cover.bin", "Doc/assets/pic.png"}
	if got := sortedKeys(files); !slices.Equal(got, want) {
		t.Fatalf("entries %v, want %v", got, want)
	}
	checkExportedPages(t, files, "", rig.srv.externalURL, x)
	if n := countReads(t, x.doc, "file_downloaded", "export"); n != 1 {
		t.Fatalf("%d export download events, want 1", n)
	}

	// From a sub-page, the export is that page down; the page above it is
	// outside and linked into pp-hub.
	res = rig.do(t, f.owner, http.MethodGet, nodePath(x.sub, "/export"), nil)
	if res.code != http.StatusOK {
		t.Fatalf("export sub-page: %d %s", res.code, res.body)
	}
	files = readZip(t, res.body)
	if got := string(files["Sub.md"]); !strings.Contains(got, "[Doc]("+rig.srv.externalURL+"/edit/"+uuidString(x.doc.ID)+")") ||
		!strings.Contains(got, "[Deep](Sub/Deep.md)") || files["assets/pic.png"] == nil {
		t.Fatalf("sub-page export %v:\n%s", sortedKeys(files), got)
	}
}

// The folder zip lays documents out as the export does, under their folder;
// a page's folder never takes a sibling's name.
func TestFolderZipExportsSubPages(t *testing.T) {
	rig := newCollabRig(t, collab.Config{})
	f := rig.fixture(t, "zip-pages", "")
	ctx := context.Background()
	mkdir := func(name string, parent pgtype.UUID) store.Node {
		d, err := rig.srv.fs.Mkdir(ctx, f.space.ID, parent, name, f.owner.ID)
		if err != nil {
			t.Fatalf("mkdir %s: %v", name, err)
		}
		return d
	}
	put := func(name, content string, parent pgtype.UUID) store.Node {
		n, _, err := rig.srv.fs.PutFile(ctx, f.space.ID, parent, pgtype.UUID{}, name, strings.NewReader(content), f.owner.ID)
		if err != nil {
			t.Fatalf("put %s: %v", name, err)
		}
		return n
	}
	outer := mkdir("Outer", pgtype.UUID{})
	inner := mkdir("F", outer.ID)
	x := newExportPages(t, rig, f, inner.ID)
	put("notes.txt", "plain", inner.ID)

	// G holds a folder and a page with sub-pages of the same name.
	g := mkdir("G", outer.ID)
	note := mkdir("Note", g.ID)
	put("x.txt", "x", note.ID)
	notePage := put("Note.md", "top\n", g.ID)
	kid := put("Kid.md", "kid\n", notePage.ID)
	rig.putVersion(t, notePage, f.owner, subPageBlock(kid)+"\n")
	rig.putVersion(t, kid, f.owner, "Up "+pageLinkInline(notePage)+" and over "+pageLinkInline(x.sub)+".\n")

	res := rig.do(t, f.owner, http.MethodGet, nodePath(outer, "/download"), nil)
	if res.code != http.StatusOK || res.header.Get("Content-Type") != "application/zip" {
		t.Fatalf("zip: %d %s", res.code, res.body)
	}
	files := readZip(t, res.body)
	want := []string{
		"F/Doc.md", "F/Doc/Sub.md", "F/Doc/Sub/Deep.md", "F/Doc/assets/cover.bin", "F/Doc/assets/pic.png", "F/notes.txt",
		"G/Note-2.md", "G/Note-2/Kid.md", "G/Note/x.txt",
	}
	if got := sortedKeys(files); !slices.Equal(got, want) {
		t.Fatalf("entries %v, want %v", got, want)
	}
	checkExportedPages(t, files, "F/", rig.srv.externalURL, x)
	if got := string(files["G/Note-2.md"]); !strings.Contains(got, "[Kid](Note-2/Kid.md)") {
		t.Fatalf("Note-2.md:\n%s", got)
	}
	if got := string(files["G/Note-2/Kid.md"]); !strings.Contains(got, "[Note](../Note-2.md)") || !strings.Contains(got, "[Sub](../../F/Doc/Sub.md)") {
		t.Fatalf("Kid.md:\n%s", got)
	}
	if string(files["F/notes.txt"]) != "plain" || string(files["G/Note/x.txt"]) != "x" {
		t.Fatal("plain files not copied as they are")
	}
}
