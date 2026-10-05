package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nhatbui/pp-hub/apps/server/internal/mdoc"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

// Copying a page tree (M43): sub-pages come along, and page ids inside the
// copied set point at the copies. Runs like the other pages tests:
//
//	PP_TEST_DATABASE_URL=postgres://pphub:pphub@localhost:5433/pphub go test ./internal/httpapi/ -run CopyPages

func subPageMD(n store.Node) string {
	id := uuidString(n.ID)
	return "<!-- slash:subPage {\"pageId\":\"" + id + "\"} -->\n[" + strings.TrimSuffix(n.Name, ".md") + "](/edit/" + id + ")"
}

func pageLinkMD(n store.Node) string {
	id := uuidString(n.ID)
	return "<!-- slash:pageLink {\"pageId\":\"" + id + "\"} -->[" + strings.TrimSuffix(n.Name, ".md") + "](/edit/" + id + ")<!-- /slash:pageLink -->"
}

// copyNode is POST /nodes/{id}/copy into parent (nil: beside the original).
func (f pagesFixture) copyNode(t *testing.T, n store.Node, parent *store.Node) store.Node {
	t.Helper()
	body := map[string]string{}
	if parent != nil {
		body["parent_id"] = uuidString(parent.ID)
	}
	res := f.rig.do(t, f.owner, http.MethodPost, nodePath(n, "/copy"), body)
	if res.code != http.StatusCreated {
		t.Fatalf("copy %s: %d %s", n.Name, res.code, res.body)
	}
	var dto nodeDTO
	if err := json.Unmarshal(res.body, &dto); err != nil {
		t.Fatalf("decode copy: %v", err)
	}
	return f.node(t, dto.ID)
}

// current is n's current version, read afresh.
func (f pagesFixture) current(t *testing.T, n store.Node) store.NodeVersion {
	t.Helper()
	n = f.node(t, uuidString(n.ID))
	ver, err := f.rig.q.GetVersion(context.Background(), n.CurrentVersionID)
	if err != nil {
		t.Fatalf("current version of %s: %v", n.Name, err)
	}
	return ver
}

// refs parses n's current markdown and returns its sub-page and link ids.
func (f pagesFixture) refs(t *testing.T, n store.Node) (md string, subs, links []string) {
	t.Helper()
	md = f.rig.content(t, f.current(t, n))
	parsed, err := mdoc.Parse([]byte(md))
	if err != nil {
		t.Fatal(err)
	}
	subs, links = mdoc.PageRefs(parsed.Doc)
	return md, subs, links
}

// child is the one live child of parent named name.
func (f pagesFixture) child(t *testing.T, parent store.Node, name string) store.Node {
	t.Helper()
	kids, err := f.rig.q.ListChildren(context.Background(), store.ListChildrenParams{SpaceID: parent.SpaceID, ParentID: parent.ID})
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range kids {
		if k.Name == name {
			return k
		}
	}
	t.Fatalf("%s has no child %q", parent.Name, name)
	return store.Node{}
}

func nodeIDs(ns ...store.Node) []string {
	out := make([]string, len(ns))
	for i, n := range ns {
		out[i] = uuidString(n.ID)
	}
	return out
}

func TestCopyPagesMapsTree(t *testing.T) {
	f := newPagesFixture(t, "copytree")
	rig := f.rig

	other := f.page(t, "Elsewhere", nil)
	plan := f.page(t, "Plan", nil)
	// Created Zeta first: the copies must keep page-tree order, not names.
	zeta := f.page(t, "Zeta", &plan)
	alpha := f.page(t, "Alpha", &plan)
	planMD := "# Plan\n\n" + subPageMD(zeta) + "\n\n" + subPageMD(alpha) + "\n\nSee " + pageLinkMD(other) + " and " + pageLinkMD(plan) + ".\n"
	zetaMD := "Back to " + pageLinkMD(plan) + ".\n"
	rig.putVersion(t, plan, f.owner, planMD)
	rig.putVersion(t, zeta, f.owner, zetaMD)
	alphaVer := f.current(t, alpha)

	cp := f.copyNode(t, plan, nil)
	if cp.Name != "Plan (2).md" || cp.ParentID.Valid {
		t.Fatalf("copy = %q under %v, want Plan (2).md at the top", cp.Name, cp.ParentID)
	}
	zetaCp, alphaCp := f.child(t, cp, "Zeta.md"), f.child(t, cp, "Alpha.md")

	md, subs, links := f.refs(t, cp)
	if want := nodeIDs(zetaCp, alphaCp); !slices.Equal(subs, want) {
		t.Errorf("copy sub-pages = %v, want the copies %v\n%s", subs, want, md)
	}
	if want := nodeIDs(other, cp); !slices.Equal(links, want) {
		t.Errorf("copy links = %v, want the outside page then itself %v\n%s", links, want, md)
	}
	if !strings.Contains(md, "[Zeta](/edit/"+uuidString(zetaCp.ID)+")") || !strings.Contains(md, "[Elsewhere](/edit/"+uuidString(other.ID)+")") ||
		!strings.Contains(md, "[Plan (2)](/edit/"+uuidString(cp.ID)+")") {
		t.Errorf("copy markdown titles/hrefs:\n%s", md)
	}
	if vs := rig.versions(t, cp); len(vs) != 1 {
		t.Errorf("copy has %d versions, want 1", len(vs))
	}

	md, subs, links = f.refs(t, zetaCp)
	if want := nodeIDs(cp); len(subs) != 0 || !slices.Equal(links, want) {
		t.Errorf("sub-page copy refs = %v %v, want link %v\n%s", subs, links, want, md)
	}
	// Nothing to map: the copy shares the original's bytes.
	if got := f.current(t, alphaCp); got.BlobHash != alphaVer.BlobHash {
		t.Errorf("sub-page without page refs: blob %s, want shared %s", got.BlobHash, alphaVer.BlobHash)
	}

	// The originals are untouched.
	if got := rig.content(t, f.current(t, plan)); got != planMD {
		t.Errorf("original changed:\n%s", got)
	}
	if got := rig.content(t, f.current(t, zeta)); got != zetaMD {
		t.Errorf("original sub-page changed:\n%s", got)
	}

	// Page-tree order survives: Zeta was created before Alpha.
	res := rig.do(t, f.owner, http.MethodGet, nodePath(cp, "/pages"), nil)
	if res.code != http.StatusOK {
		t.Fatalf("pages: %d %s", res.code, res.body)
	}
	if got := pageTitles(decodeJSON[[]pageMetaDTO](t, res)); got != "Zeta,Alpha" {
		t.Errorf("copied page order = %s, want Zeta,Alpha", got)
	}

	// A copied sub-page alone keeps pointing at the original parent, which
	// was not copied.
	lone := f.copyNode(t, zeta, &other)
	if _, _, links := f.refs(t, lone); !slices.Equal(links, nodeIDs(plan)) {
		t.Errorf("lone sub-page copy links = %v, want the original %v", links, nodeIDs(plan))
	}
	if got, want := f.current(t, lone).BlobHash, f.current(t, zeta).BlobHash; got != want {
		t.Errorf("lone sub-page copy blob %s, want shared %s", got, want)
	}
}

func TestCopyPagesPlainDocumentSharesBlob(t *testing.T) {
	f := newPagesFixture(t, "copyplain")
	doc := f.page(t, "Plain", nil)
	f.rig.putVersion(t, doc, f.owner, "Just text, no pages.\n")
	cp := f.copyNode(t, doc, nil)
	if got, want := f.current(t, cp).BlobHash, f.current(t, doc).BlobHash; got != want {
		t.Errorf("copy blob %s, want shared %s", got, want)
	}
}

func TestCopyPagesFolder(t *testing.T) {
	f := newPagesFixture(t, "copyfolder")
	rig := f.rig
	folder := mkNode(t, rig.q, f.space, pgtype.UUID{}, store.NodeKindDir, "Folder", f.owner)
	plan := f.page(t, "Plan", &folder)
	sub := f.page(t, "Sub", &plan)
	rig.putVersion(t, plan, f.owner, "Intro\n\n"+subPageMD(sub)+"\n")
	rig.putVersion(t, sub, f.owner, "Up: "+pageLinkMD(plan)+"\n")

	dest := mkNode(t, rig.q, f.space, pgtype.UUID{}, store.NodeKindDir, "Dest", f.owner)
	cp := f.copyNode(t, folder, &dest)
	planCp := f.child(t, cp, "Plan.md")
	subCp := f.child(t, planCp, "Sub.md")
	if md, subs, _ := f.refs(t, planCp); !slices.Equal(subs, nodeIDs(subCp)) {
		t.Errorf("folder copy: document sub-pages = %v, want %v\n%s", subs, nodeIDs(subCp), md)
	}
	if md, _, links := f.refs(t, subCp); !slices.Equal(links, nodeIDs(planCp)) {
		t.Errorf("folder copy: sub-page links = %v, want %v\n%s", links, nodeIDs(planCp), md)
	}
}

// A lossy document is not re-rendered — that would drop what the block set
// cannot hold — but its page ids are rewritten in the bytes.
func TestCopyPagesLossyRewritesBytes(t *testing.T) {
	f := newPagesFixture(t, "copylossy")
	doc := f.page(t, "Notes", nil)
	sub := f.page(t, "Sub", &doc)
	src := "Text with a footnote[^1].\n\n" + subPageMD(sub) + "\n\n[^1]: The note.\n"
	f.rig.putVersion(t, doc, f.owner, src)

	cp := f.copyNode(t, doc, nil)
	subCp := f.child(t, cp, "Sub.md")
	want := strings.ReplaceAll(src, uuidString(sub.ID), uuidString(subCp.ID))
	if got := f.rig.content(t, f.current(t, cp)); got != want {
		t.Errorf("lossy copy:\n%s\nwant:\n%s", got, want)
	}
}
