package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nhatbui/pp-hub/apps/server/internal/collab"
	"github.com/nhatbui/pp-hub/apps/server/internal/share"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

// Pages (M43): the node tree is the page tree. These run the production
// router against a real Postgres, or skip like the rest of this package's
// database tests:
//
//	PP_TEST_DATABASE_URL=postgres://pphub:pphub@localhost:5433/pphub go test ./internal/httpapi/ -run Page

type pagesFixture struct {
	rig      *collabRig
	space    store.Space
	owner    store.User
	member   store.User // a member of the space holding no grant
	outsider store.User // another member, granted single pages
}

func newPagesFixture(t *testing.T, label string) pagesFixture {
	t.Helper()
	rig := newCollabRig(t, collab.Config{})
	ctx := context.Background()
	f := pagesFixture{rig: rig, owner: feedUser(t, rig.q, label+"-owner")}
	sp, err := rig.srv.spaces.Create(ctx, fmt.Sprintf("pages-%s-%d", label, os.Getpid()), f.owner.ID)
	if err != nil {
		t.Fatalf("create space: %v", err)
	}
	t.Cleanup(func() { _ = rig.q.DeleteSpace(ctx, sp.ID) })
	f.space = sp
	f.member = feedUser(t, rig.q, label+"-member")
	f.outsider = feedUser(t, rig.q, label+"-outsider")
	for _, u := range []store.User{f.member, f.outsider} {
		if err := rig.srv.spaces.AddMember(ctx, sp.ID, u.ID, false); err != nil {
			t.Fatalf("add member: %v", err)
		}
	}
	for _, u := range []store.User{f.owner, f.member, f.outsider} {
		rig.login(t, u)
	}
	return f
}

// create is POST /spaces/{id}/documents as u.
func (f pagesFixture) create(t *testing.T, u store.User, body map[string]string) (nodeDTO, rigResponse) {
	t.Helper()
	res := f.rig.do(t, u, http.MethodPost, "/api/v1/spaces/"+uuidString(f.space.ID)+"/documents", body)
	var dto nodeDTO
	if res.code == http.StatusCreated {
		if err := json.Unmarshal(res.body, &dto); err != nil {
			t.Fatalf("decode node: %v; body=%s", err, res.body)
		}
	}
	return dto, res
}

// page creates a block document named name (empty: the default) under
// parent (nil: the top level) as the owner.
func (f pagesFixture) page(t *testing.T, name string, parent *store.Node) store.Node {
	t.Helper()
	body := map[string]string{"kind": "markdown", "name": name}
	if parent != nil {
		body["parent_id"] = uuidString(parent.ID)
	}
	dto, res := f.create(t, f.owner, body)
	if res.code != http.StatusCreated {
		t.Fatalf("create page %q: %d %s", name, res.code, res.body)
	}
	return f.node(t, dto.ID)
}

func (f pagesFixture) node(t *testing.T, id string) store.Node {
	t.Helper()
	var nid pgtype.UUID
	if err := nid.Scan(id); err != nil {
		t.Fatal(err)
	}
	n, err := f.rig.q.GetNodeAny(context.Background(), nid)
	if err != nil {
		t.Fatalf("get node %s: %v", id, err)
	}
	return n
}

func decodeJSON[T any](t *testing.T, res rigResponse) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(res.body, &v); err != nil {
		t.Fatalf("decode: %v; body=%s", err, res.body)
	}
	return v
}

func (f pagesFixture) getPage(t *testing.T, u store.User, n store.Node) (pageDTO, int) {
	t.Helper()
	res := f.rig.do(t, u, http.MethodGet, nodePath(n, "/page"), nil)
	if res.code != http.StatusOK {
		return pageDTO{}, res.code
	}
	return decodeJSON[pageDTO](t, res), res.code
}

func pageTitles(ps []pageMetaDTO) string {
	titles := make([]string, len(ps))
	for i, p := range ps {
		titles[i] = p.Title
	}
	return strings.Join(titles, ",")
}

func idOf(p *string) string {
	if p == nil {
		return "<null>"
	}
	return *p
}

// canContain across every path that sets a parent: create, mkdir, upload,
// move, copy and restore, cycles included.
func TestPagesContainerRule(t *testing.T) {
	f := newPagesFixture(t, "contain")
	rig := f.rig
	ctx := context.Background()

	// A nameless block document is Untitled, suffixed when taken.
	first, res := f.create(t, f.owner, map[string]string{"kind": "markdown"})
	if res.code != http.StatusCreated || first.Name != "Untitled.md" {
		t.Fatalf("nameless document: %d %q %s", res.code, first.Name, res.body)
	}
	second, _ := f.create(t, f.owner, map[string]string{"kind": "markdown"})
	if second.Name != "Untitled (2).md" {
		t.Errorf("second nameless document = %q, want Untitled (2).md", second.Name)
	}
	if _, res := f.create(t, f.owner, map[string]string{"kind": "document"}); res.code != http.StatusBadRequest {
		t.Errorf("nameless office document = %d, want 400", res.code)
	}

	doc := f.page(t, "Plan", nil)
	sub := f.page(t, "", &doc)
	if sub.ParentID != doc.ID || sub.Name != "Untitled.md" {
		t.Fatalf("sub-page: parent %v name %q", sub.ParentID, sub.Name)
	}
	if _, res := f.create(t, f.owner, map[string]string{"kind": "document", "name": "Memo", "parent_id": uuidString(doc.ID)}); res.code != http.StatusBadRequest {
		t.Errorf("office document under a document = %d, want 400", res.code)
	}

	folder := mkNode(t, rig.q, f.space, pgtype.UUID{}, store.NodeKindDir, "Folder", f.owner)
	if res := rig.do(t, f.owner, http.MethodPost, "/api/v1/spaces/"+uuidString(f.space.ID)+"/dirs",
		map[string]string{"name": "Inside", "parent_id": uuidString(doc.ID)}); res.code != http.StatusBadRequest {
		t.Errorf("folder under a document = %d %s, want 400", res.code, res.body)
	}

	// No upload lands under a file, not even a .md: a sub-page is made
	// through /documents.
	for _, name := range []string{"notes.md", "notes.txt"} {
		res := rig.request(t, f.owner, http.MethodPost, "/api/v1/uploads/", map[string]string{
			"Tus-Resumable": "1.0.0", "Upload-Length": "5",
			"Upload-Metadata": tusMeta("filename", name, "space_id", uuidString(f.space.ID), "parent_id", uuidString(doc.ID)),
		}, nil)
		if res.code != http.StatusBadRequest {
			t.Errorf("upload %s under a document = %d %s, want 400", name, res.code, res.body)
		}
	}
	if _, _, err := rig.srv.fs.PutFile(ctx, f.space.ID, doc.ID, pgtype.UUID{}, "x.txt", strings.NewReader("x"), f.owner.ID); err == nil {
		t.Error("fs.PutFile put a text file under a document")
	}

	move := func(n store.Node, parent store.Node) int {
		return rig.do(t, f.owner, http.MethodPatch, nodePath(n, "/move"), map[string]string{"parent_id": uuidString(parent.ID)}).code
	}
	copyTo := func(n store.Node, parent store.Node) int {
		return rig.do(t, f.owner, http.MethodPost, nodePath(n, "/copy"), map[string]string{"parent_id": uuidString(parent.ID)}).code
	}
	text, _ := putVersion(t, rig.srv, f.space, pgtype.UUID{}, pgtype.UUID{}, "plain.txt", "plain", f.owner)
	if code := move(folder, doc); code != http.StatusBadRequest {
		t.Errorf("move folder into document = %d, want 400", code)
	}
	if code := move(text, doc); code != http.StatusBadRequest {
		t.Errorf("move text file into document = %d, want 400", code)
	}
	if code := move(doc, sub); code != http.StatusConflict {
		t.Errorf("move document into its own sub-page = %d, want 409", code)
	}
	loose := f.page(t, "Loose", nil)
	if code := move(loose, sub); code != http.StatusOK {
		t.Errorf("move document into a document = %d, want 200", code)
	}
	if code := copyTo(text, doc); code != http.StatusBadRequest {
		t.Errorf("copy text file into document = %d, want 400", code)
	}
	if code := copyTo(doc, sub); code != http.StatusConflict {
		t.Errorf("copy document into its own sub-page = %d, want 409", code)
	}
	if code := copyTo(doc, doc); code != http.StatusConflict {
		t.Errorf("copy document into itself = %d, want 409", code)
	}
	if code := copyTo(loose, doc); code != http.StatusCreated {
		t.Errorf("copy document into a document = %d, want 201", code)
	}
	if code := move(sub, folder); code != http.StatusOK {
		t.Errorf("move sub-page into a folder = %d, want 200", code)
	}
	if code := move(sub, doc); code != http.StatusOK {
		t.Errorf("move sub-page back = %d, want 200", code)
	}

	// Restore puts a sub-page back under its document...
	if res := rig.do(t, f.owner, http.MethodDelete, nodePath(sub, "/"), nil); res.code != http.StatusNoContent {
		t.Fatalf("trash sub-page: %d %s", res.code, res.body)
	}
	if res := rig.do(t, f.owner, http.MethodPost, nodePath(sub, "/restore"), nil); res.code != http.StatusOK || strings.Contains(string(res.body), `"relocated":true`) {
		t.Fatalf("restore sub-page: %d %s", res.code, res.body)
	}
	if got := f.node(t, uuidString(sub.ID)); got.ParentID != doc.ID {
		t.Errorf("restored sub-page parent = %v, want the document", got.ParentID)
	}
	// ...and to the top level once the document cannot hold it: renamed and
	// saved as text, it is no longer a page.
	if res := rig.do(t, f.owner, http.MethodDelete, nodePath(sub, "/"), nil); res.code != http.StatusNoContent {
		t.Fatalf("trash sub-page: %d", res.code)
	}
	if err := rig.srv.fs.Rename(ctx, doc.ID, "Plan.txt"); err != nil {
		t.Fatal(err)
	}
	doc.Name = "Plan.txt"
	rig.putVersion(t, doc, f.owner, "now text")
	if res := rig.do(t, f.owner, http.MethodPost, nodePath(sub, "/restore"), nil); res.code != http.StatusOK || !strings.Contains(string(res.body), `"relocated":true`) {
		t.Fatalf("restore sub-page: %d %s, want 200 relocated", res.code, res.body)
	}
	if got := f.node(t, uuidString(sub.ID)); got.ParentID.Valid {
		t.Errorf("sub-page restored under a text file (%v), want the top level", got.ParentID)
	}
}

// Sharing a document shares its sub-pages; a sub-page shared alone names no
// ancestor; the tree lists in creation order; trash shows through.
func TestPagesTreeAndAccess(t *testing.T) {
	f := newPagesFixture(t, "tree")
	rig := f.rig
	doc := f.page(t, "Handbook", nil)
	zeta := f.page(t, "Zeta", &doc)
	alpha := f.page(t, "Alpha", &doc)
	deep := f.page(t, "Deep", &zeta)

	grantDirect(t, rig.srv, doc, f.member, f.owner, share.GrantCaps{Level: share.LevelView})
	grantDirect(t, rig.srv, zeta, f.outsider, f.owner, share.GrantCaps{Level: share.LevelView})

	// Through the document's grant: the whole chain.
	p, code := f.getPage(t, f.member, deep)
	if code != http.StatusOK {
		t.Fatalf("member GET deep page = %d", code)
	}
	if p.Page.Title != "Deep" || idOf(p.Page.ParentID) != uuidString(zeta.ID) || pageTitles(p.Ancestors) != "Handbook,Zeta" {
		t.Errorf("member deep page = %+v ancestors %s", p.Page, pageTitles(p.Ancestors))
	}
	if p.Ancestors[0].ParentID != nil || !p.Ancestors[0].HasChildren || p.Page.HasChildren {
		t.Errorf("ancestors = %+v", p.Ancestors)
	}

	// A sub-page shared alone: no parent, no ancestors.
	p, code = f.getPage(t, f.outsider, zeta)
	if code != http.StatusOK {
		t.Fatalf("outsider GET shared sub-page = %d", code)
	}
	if p.Page.ParentID != nil || len(p.Ancestors) != 0 || !p.Page.HasChildren {
		t.Errorf("sub-page shared alone = %+v ancestors %d", p.Page, len(p.Ancestors))
	}
	p, _ = f.getPage(t, f.outsider, deep)
	if pageTitles(p.Ancestors) != "Zeta" || p.Ancestors[0].ParentID != nil {
		t.Errorf("outsider deep ancestors = %+v", p.Ancestors)
	}
	for _, n := range []store.Node{doc, alpha} {
		if _, code := f.getPage(t, f.outsider, n); code != http.StatusNotFound {
			t.Errorf("outsider GET %s = %d, want 404", n.Name, code)
		}
	}
	folder := mkNode(t, rig.q, f.space, pgtype.UUID{}, store.NodeKindDir, "Not a page", f.owner)
	if _, code := f.getPage(t, f.owner, folder); code != http.StatusNotFound {
		t.Errorf("GET page of a folder = %d, want 404", code)
	}

	// Tree order is creation order, not name order.
	res := rig.do(t, f.member, http.MethodGet, nodePath(doc, "/pages"), nil)
	if res.code != http.StatusOK {
		t.Fatalf("pages: %d %s", res.code, res.body)
	}
	pages := decodeJSON[[]pageMetaDTO](t, res)
	if pageTitles(pages) != "Zeta,Alpha" || !pages[0].HasChildren || pages[1].HasChildren ||
		idOf(pages[0].ParentID) != uuidString(doc.ID) {
		t.Errorf("pages = %+v", pages)
	}
	if res := rig.do(t, f.outsider, http.MethodGet, nodePath(doc, "/pages"), nil); res.code != http.StatusNotFound {
		t.Errorf("outsider pages of the document = %d, want 404", res.code)
	}

	// The listing counts sub-pages on the document, in the listing and on
	// the node itself.
	res = rig.do(t, f.owner, http.MethodGet, "/api/v1/spaces/"+uuidString(f.space.ID)+"/children", nil)
	var counted bool
	for _, n := range decodeJSON[[]nodeDTO](t, res) {
		if n.ID == uuidString(doc.ID) {
			counted = n.ChildPageCount == 2
		}
		if n.ID == uuidString(folder.ID) && n.ChildPageCount != 0 {
			t.Errorf("folder child_page_count = %d", n.ChildPageCount)
		}
	}
	if !counted {
		t.Errorf("listing child_page_count of the document is not 2: %s", res.body)
	}
	if n := decodeJSON[nodeDTO](t, rig.do(t, f.owner, http.MethodGet, nodePath(zeta, "/"), nil)); n.ChildPageCount != 1 {
		t.Errorf("GET node child_page_count = %d, want 1", n.ChildPageCount)
	}

	// Trash the document: its pages answer trashed, and leave the tree.
	if res := rig.do(t, f.owner, http.MethodDelete, nodePath(alpha, "/"), nil); res.code != http.StatusNoContent {
		t.Fatalf("trash alpha: %d", res.code)
	}
	if pages := decodeJSON[[]pageMetaDTO](t, rig.do(t, f.owner, http.MethodGet, nodePath(doc, "/pages"), nil)); pageTitles(pages) != "Zeta" {
		t.Errorf("pages after trashing Alpha = %s", pageTitles(pages))
	}
	if p, code := f.getPage(t, f.owner, alpha); code != http.StatusOK || !p.Page.Trashed {
		t.Errorf("trashed page = %d %+v", code, p.Page)
	}
	if res := rig.do(t, f.owner, http.MethodDelete, nodePath(doc, "/"), nil); res.code != http.StatusNoContent {
		t.Fatalf("trash document: %d", res.code)
	}
	p, code = f.getPage(t, f.member, deep)
	if code != http.StatusOK || !p.Page.Trashed || !p.Ancestors[0].Trashed {
		t.Errorf("page under a trashed document = %d %+v %+v", code, p.Page, p.Ancestors)
	}
	if res := rig.do(t, f.owner, http.MethodPost, nodePath(doc, "/restore"), nil); res.code != http.StatusOK {
		t.Fatalf("restore document: %d", res.code)
	}
	if p, _ := f.getPage(t, f.member, deep); p.Page.Trashed {
		t.Error("page still trashed after its document was restored")
	}
}

// Backlinks are filtered by what the caller can view, and the projections'
// writes are guarded against stale versions.
func TestPagesBacklinksAndIndexGuard(t *testing.T) {
	f := newPagesFixture(t, "backlinks")
	rig := f.rig
	ctx := context.Background()
	target := f.page(t, "Target", nil)
	private := f.page(t, "Private", nil)
	shared := f.page(t, "Shared", nil)
	child := f.page(t, "Child", &shared)
	for _, n := range []store.Node{target, shared} {
		grantDirect(t, rig.srv, n, f.member, f.owner, share.GrantCaps{Level: share.LevelView})
	}

	link := func(src store.Node) {
		t.Helper()
		src = f.node(t, uuidString(src.ID))
		if _, err := rig.q.DeletePageRefsForSource(ctx, store.DeletePageRefsForSourceParams{
			SourceID: src.ID, NodeVersionID: src.CurrentVersionID,
		}); err != nil {
			t.Fatal(err)
		}
		n, err := rig.q.InsertPageRefs(ctx, store.InsertPageRefsParams{
			SourceID: src.ID, NodeVersionID: src.CurrentVersionID,
			LinkIds: []pgtype.UUID{target.ID}, SubIds: []pgtype.UUID{},
		})
		if err != nil || n != 1 {
			t.Fatalf("insert page refs for %s: %d %v", src.Name, n, err)
		}
	}
	link(private)
	link(child)

	backlinks := func(u store.User) []pageMetaDTO {
		res := rig.do(t, u, http.MethodGet, nodePath(target, "/backlinks"), nil)
		if res.code != http.StatusOK {
			t.Fatalf("backlinks: %d %s", res.code, res.body)
		}
		return decodeJSON[[]pageMetaDTO](t, res)
	}
	if got := pageTitles(backlinks(f.owner)); got != "Child,Private" {
		t.Errorf("owner backlinks = %s", got)
	}
	got := backlinks(f.member)
	if pageTitles(got) != "Child" || idOf(got[0].ParentID) != uuidString(shared.ID) {
		t.Errorf("member backlinks = %+v", got)
	}
	if res := rig.do(t, f.outsider, http.MethodGet, nodePath(target, "/backlinks"), nil); res.code != http.StatusNotFound {
		t.Errorf("outsider backlinks = %d, want 404", res.code)
	}

	// A late job for a superseded version writes nothing.
	old := f.node(t, uuidString(private.ID)).CurrentVersionID
	rig.putVersion(t, private, f.owner, "newer")
	if n, err := rig.q.InsertPageRefs(ctx, store.InsertPageRefsParams{
		SourceID: private.ID, NodeVersionID: old, LinkIds: []pgtype.UUID{shared.ID}, SubIds: []pgtype.UUID{},
	}); err != nil || n != 0 {
		t.Errorf("stale InsertPageRefs = %d %v, want 0", n, err)
	}
	if n, err := rig.q.DeletePageRefsForSource(ctx, store.DeletePageRefsForSourceParams{
		SourceID: private.ID, NodeVersionID: old,
	}); err != nil || n != 0 {
		t.Errorf("stale DeletePageRefsForSource = %d %v, want 0", n, err)
	}
	icon := pgtype.Text{String: "📘", Valid: true}
	if n, err := rig.q.UpsertPageMeta(ctx, store.UpsertPageMetaParams{NodeID: private.ID, NodeVersionID: old, Icon: icon}); err != nil || n != 0 {
		t.Errorf("stale UpsertPageMeta = %d %v, want 0", n, err)
	}
	cur := f.node(t, uuidString(private.ID)).CurrentVersionID
	if n, err := rig.q.UpsertPageMeta(ctx, store.UpsertPageMetaParams{NodeID: private.ID, NodeVersionID: cur, Icon: icon}); err != nil || n != 1 {
		t.Errorf("UpsertPageMeta = %d %v, want 1", n, err)
	}
	if p, _ := f.getPage(t, f.owner, private); idOf(p.Page.Icon) != "📘" || p.Page.Cover != nil {
		t.Errorf("page meta = %+v", p.Page)
	}

	// A trashed source is no backlink.
	if res := rig.do(t, f.owner, http.MethodDelete, nodePath(shared, "/"), nil); res.code != http.StatusNoContent {
		t.Fatalf("trash: %d", res.code)
	}
	if got := pageTitles(backlinks(f.owner)); got != "Private" {
		t.Errorf("owner backlinks after trashing Shared = %s", got)
	}
}

// Through a link the page routes answer read-only, ancestors stopping at the
// link's root.
func TestSharePagesBoundedByLinkRoot(t *testing.T) {
	f := newPagesFixture(t, "sharelink")
	rig := f.rig
	doc := f.page(t, "Top", nil)
	mid := f.page(t, "Mid", &doc)
	leaf := f.page(t, "Leaf", &mid)
	_, token, err := rig.srv.share.CreateLink(context.Background(), mid.ID, false, pgtype.Timestamptz{}, f.owner.ID)
	if err != nil {
		t.Fatalf("link: %v", err)
	}
	base := "/s/" + token

	res := rig.anon(t, base+"/page?node="+uuidString(leaf.ID), nil)
	if res.code != http.StatusOK {
		t.Fatalf("share page: %d %s", res.code, res.body)
	}
	p := decodeJSON[pageDTO](t, res)
	if pageTitles(p.Ancestors) != "Mid" || p.Ancestors[0].ParentID != nil || idOf(p.Page.ParentID) != uuidString(mid.ID) {
		t.Errorf("share page = %+v ancestors %+v", p.Page, p.Ancestors)
	}
	res = rig.anon(t, base+"/page", nil)
	if p := decodeJSON[pageDTO](t, res); res.code != http.StatusOK || p.Page.ParentID != nil || len(p.Ancestors) != 0 || !p.Page.HasChildren {
		t.Errorf("share root page = %d %s", res.code, res.body)
	}
	if res := rig.anon(t, base+"/page?node="+uuidString(doc.ID), nil); res.code != http.StatusNotFound {
		t.Errorf("share page above the root = %d, want 404", res.code)
	}
	res = rig.anon(t, base+"/pages?node="+uuidString(mid.ID), nil)
	if pages := decodeJSON[[]pageMetaDTO](t, res); res.code != http.StatusOK || pageTitles(pages) != "Leaf" {
		t.Errorf("share pages = %d %s", res.code, res.body)
	}
	if res := rig.anon(t, base+"/pages?node="+uuidString(doc.ID), nil); res.code != http.StatusNotFound {
		t.Errorf("share pages above the root = %d, want 404", res.code)
	}
}
