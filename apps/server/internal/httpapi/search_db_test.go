package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nhatbui/pp-hub/apps/server/internal/auth"
	"github.com/nhatbui/pp-hub/apps/server/internal/blob"
	"github.com/nhatbui/pp-hub/apps/server/internal/fs"
	"github.com/nhatbui/pp-hub/apps/server/internal/preview"
	"github.com/nhatbui/pp-hub/apps/server/internal/share"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
	"github.com/nhatbui/pp-hub/apps/server/internal/textsearch"
)

// Content search (M27) is SQL — the full-text and substring predicates, the
// current-version join, the ranking — so it runs against a real Postgres:
//
//	PP_TEST_DATABASE_URL=postgres://pphub:pphub@localhost:5433/<db> go test ./internal/httpapi/ -run Search

// searchEnv is feedEnv with a blob store, so files have real versions.
func searchEnv(t *testing.T) (*Server, *store.Queries) {
	t.Helper()
	srv, q := feedEnv(t)
	blobs, err := blob.NewLocalStore(t.TempDir(), q.BlobClaims())
	if err != nil {
		t.Fatalf("blob store: %v", err)
	}
	srv.fs = fs.NewService(q, blobs)
	return srv, q
}

// putIndexed stores a file version and indexes it the way the extract:text
// job does, minus the queue.
func putIndexed(t *testing.T, srv *Server, q *store.Queries, sp store.Space, parent, fileID pgtype.UUID,
	name, body string, by store.User) store.Node {
	t.Helper()
	ctx := context.Background()
	node, ver := putVersion(t, srv, sp, parent, fileID, name, body, by)
	ex, err := preview.TextExtractor{}.Extract(ctx, strings.NewReader(body), preview.Payload{Mime: ver.Mime})
	if err != nil {
		t.Fatalf("extract %s: %v", name, err)
	}
	content, folded := textsearch.Prepare(ex.Text)
	if n, err := q.UpsertNodeText(ctx, store.UpsertNodeTextParams{
		NodeID: node.ID, NodeVersionID: ver.ID, Content: content, Folded: folded,
	}); err != nil || n != 1 {
		t.Fatalf("index %s: rows=%d err=%v", name, n, err)
	}
	return node
}

func putVersion(t *testing.T, srv *Server, sp store.Space, parent, fileID pgtype.UUID,
	name, body string, by store.User) (store.Node, store.NodeVersion) {
	t.Helper()
	node, ver, err := srv.fs.PutFile(context.Background(), sp.ID, parent, fileID, name, strings.NewReader(body), by.ID)
	if err != nil {
		t.Fatalf("put %s: %v", name, err)
	}
	return node, ver
}

type searchHit struct {
	ID                string    `json:"id"`
	Name              string    `json:"name"`
	Match             string    `json:"match"`
	ParentName        string    `json:"parent_name"`
	Snippet           *string   `json:"snippet"`
	SnippetHighlights *[][2]int `json:"snippet_highlights"`
}

func searchAs(t *testing.T, srv *Server, u store.User, q string) []searchHit {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/search?q="+url.QueryEscape(q), nil)
	req = req.WithContext(context.WithValue(req.Context(), sessionCtxKey, &auth.Session{ID: u.ID, User: u}))
	rec := httptest.NewRecorder()
	srv.handleSearch(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("search %q: %d %s", q, rec.Code, rec.Body)
	}
	var hits []searchHit
	if err := json.Unmarshal(rec.Body.Bytes(), &hits); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return hits
}

func findHit(hits []searchHit, id pgtype.UUID) (searchHit, int) {
	for i, h := range hits {
		if h.ID == id.String() {
			return h, i
		}
	}
	return searchHit{}, -1
}

// highlightedText is what the palette would render bold.
func highlightedText(h searchHit) []string {
	if h.Snippet == nil || h.SnippetHighlights == nil {
		return nil
	}
	rs := []rune(*h.Snippet)
	var out []string
	for _, r := range *h.SnippetHighlights {
		out = append(out, string(rs[r[0]:r[1]]))
	}
	return out
}

type contentFixture struct {
	space  store.Space
	owner  store.User
	member store.User
	shared store.Node // folder granted to member with download
	notes  store.Node // notes.md inside shared
	plan   store.Node // "Budget plan.md" at the root, owner only
}

func newContentFixture(t *testing.T, srv *Server, q *store.Queries, label string) contentFixture {
	t.Helper()
	ctx := context.Background()
	f := contentFixture{owner: feedUser(t, q, label+"-owner"), member: feedUser(t, q, label+"-member")}
	sp, err := srv.spaces.Create(ctx, "search-"+label+"-"+f.owner.ID.String(), f.owner.ID)
	if err != nil {
		t.Fatalf("space: %v", err)
	}
	t.Cleanup(func() { _ = q.DeleteSpace(ctx, sp.ID) })
	if err := srv.spaces.AddMember(ctx, sp.ID, f.member.ID, false); err != nil {
		t.Fatalf("member: %v", err)
	}
	f.space = sp
	f.shared = mkNode(t, q, sp, pgtype.UUID{}, store.NodeKindDir, "shared", f.owner)
	grantDirect(t, srv, f.shared, f.member, f.owner, share.GrantCaps{Level: share.LevelView, CanDownload: true})
	f.notes = putIndexed(t, srv, q, sp, f.shared.ID, pgtype.UUID{}, "notes.md",
		"# Họp nhóm\n\nThe **budget** review for the Tiếng Việt team is on Friday.\n", f.owner)
	f.plan = putIndexed(t, srv, q, sp, pgtype.UUID{}, pgtype.UUID{}, "Budget plan.md", "Q3 numbers\n", f.owner)
	return f
}

func TestSearchFindsContentAndRanksNameHitsFirst(t *testing.T) {
	srv, q := searchEnv(t)
	f := newContentFixture(t, srv, q, "rank")

	hits := searchAs(t, srv, f.owner, "budget")
	plan, pi := findHit(hits, f.plan.ID)
	notes, ni := findHit(hits, f.notes.ID)
	if pi < 0 || ni < 0 {
		t.Fatalf("want both the name hit and the content hit, got %+v", hits)
	}
	if pi > ni {
		t.Errorf("name hit ranked %d, below content-only hit at %d", pi, ni)
	}
	if plan.Match != "name" || plan.Snippet != nil {
		t.Errorf("name hit: match=%q snippet=%v", plan.Match, plan.Snippet)
	}
	if notes.Match != "content" || notes.Snippet == nil {
		t.Fatalf("content hit: match=%q snippet=%v", notes.Match, notes.Snippet)
	}
	if got := highlightedText(notes); !slices.Equal(got, []string{"budget"}) {
		t.Errorf("highlights %v in %q", got, *notes.Snippet)
	}
	if strings.Contains(*notes.Snippet, "**") {
		t.Errorf("snippet carries markdown syntax: %q", *notes.Snippet)
	}

	// A mid-word substring reaches content through the fallback.
	if _, i := findHit(searchAs(t, srv, f.owner, "udge"), f.notes.ID); i < 0 {
		t.Error("substring fallback missed a mid-word hit")
	}
}

// Diacritics are folded on both sides: the query matches with, without, or in
// another case, and the snippet highlights the original words.
func TestSearchVietnameseDiacritics(t *testing.T) {
	srv, q := searchEnv(t)
	f := newContentFixture(t, srv, q, "vi")

	for _, query := range []string{"tiếng việt", "Tiếng Việt", "TIẾNG VIỆT", "tieng viet", "tiêng"} {
		h, i := findHit(searchAs(t, srv, f.owner, query), f.notes.ID)
		if i < 0 {
			t.Errorf("%q did not find the document", query)
			continue
		}
		want := []string{"Tiếng", "Việt"}
		if query == "tiêng" {
			want = []string{"Tiếng"}
		}
		if got := highlightedText(h); !slices.Equal(got, want) {
			t.Errorf("%q highlighted %v in %q", query, got, *h.Snippet)
		}
	}
	// A heading is text too.
	if _, i := findHit(searchAs(t, srv, f.owner, "hop nhom"), f.notes.ID); i < 0 {
		t.Error("heading text not searchable")
	}
}

// The content branch goes through the same reachability filter as names and
// needs the same view a read takes: a hit's snippet is the file's text.
func TestSearchContentRespectsAccess(t *testing.T) {
	srv, q := searchEnv(t)
	f := newContentFixture(t, srv, q, "access")

	private := mkNode(t, q, f.space, pgtype.UUID{}, store.NodeKindDir, "private", f.owner)
	secret := putIndexed(t, srv, q, f.space, private.ID, pgtype.UUID{}, "secret.txt", "budget cuts nobody may see", f.owner)
	viewOnly := putIndexed(t, srv, q, f.space, pgtype.UUID{}, pgtype.UUID{}, "memo.txt", "budget memo", f.owner)
	grantDirect(t, srv, viewOnly, f.member, f.owner, share.GrantCaps{Level: share.LevelView})

	hits := searchAs(t, srv, f.member, "budget")
	if _, i := findHit(hits, f.notes.ID); i < 0 {
		t.Error("member lost a content hit inside a folder shared with download")
	}
	if _, i := findHit(hits, secret.ID); i >= 0 {
		t.Error("content hit leaked a file the member cannot reach")
	}
	// A view-only file can be read, so its text is a hit like any other.
	if _, i := findHit(hits, viewOnly.ID); i < 0 {
		t.Error("view-only file's text is searchable, and was not found")
	}
	if _, i := findHit(hits, f.plan.ID); i >= 0 {
		t.Error("name hit leaked a file the member cannot reach")
	}
	// Name search for the view-only file is unchanged.
	if _, i := findHit(searchAs(t, srv, f.member, "memo"), viewOnly.ID); i < 0 {
		t.Error("view-only file no longer found by name")
	}

	// Another company's member finds nothing of this space at all.
	outsider := feedUser(t, q, "access-outsider")
	if len(searchAs(t, srv, outsider, "budget")) != 0 {
		t.Error("a non-member found content in the space")
	}
}

// Only the node's current version is searched: a new version replaces the
// text, and until its extraction lands the stale text does not match.
func TestSearchNewVersionReplacesIndex(t *testing.T) {
	srv, q := searchEnv(t)
	f := newContentFixture(t, srv, q, "version")

	putVersion(t, srv, f.space, f.shared.ID, f.notes.ID, "notes.md", "Minutes: nothing about money\n", f.owner)
	if _, i := findHit(searchAs(t, srv, f.owner, "friday"), f.notes.ID); i >= 0 {
		t.Error("stale text of a superseded version still matched")
	}
	putIndexed(t, srv, q, f.space, f.shared.ID, f.notes.ID, "notes.md", "Minutes: nothing about money, sent Friday\n", f.owner)
	if _, i := findHit(searchAs(t, srv, f.owner, "review"), f.notes.ID); i >= 0 {
		t.Error("old text still indexed after the new version was extracted")
	}
	h, i := findHit(searchAs(t, srv, f.owner, "money"), f.notes.ID)
	if i < 0 || !slices.Equal(highlightedText(h), []string{"money"}) {
		t.Errorf("new version's text not searchable: %+v", h)
	}
}

// Trash and purge behave exactly as they do for name search, because content
// hits come through the same query and the same deleted_at filter.
func TestSearchContentTrashAndPurgeMatchNameSearch(t *testing.T) {
	srv, q := searchEnv(t)
	f := newContentFixture(t, srv, q, "trash")
	ctx := context.Background()

	present := func(query string, id pgtype.UUID) bool {
		_, i := findHit(searchAs(t, srv, f.owner, query), id)
		return i >= 0
	}
	// Trashing the containing folder: whatever name search says about the
	// file inside, content search says the same.
	if err := srv.fs.Delete(ctx, f.shared.ID, f.owner.ID); err != nil {
		t.Fatal(err)
	}
	if byName, byContent := present("notes", f.notes.ID), present("friday", f.notes.ID); byName != byContent {
		t.Errorf("under a trashed folder: name search %v, content search %v", byName, byContent)
	}
	if _, _, err := srv.fs.Restore(ctx, f.shared.ID); err != nil {
		t.Fatal(err)
	}

	if err := srv.fs.Delete(ctx, f.notes.ID, f.owner.ID); err != nil {
		t.Fatal(err)
	}
	if present("notes", f.notes.ID) || present("friday", f.notes.ID) {
		t.Error("a trashed file is still found")
	}
	if _, _, err := srv.fs.Restore(ctx, f.notes.ID); err != nil {
		t.Fatal(err)
	}
	if !present("notes", f.notes.ID) || !present("friday", f.notes.ID) {
		t.Error("a file restored from the trash is not found again")
	}

	if err := srv.fs.Delete(ctx, f.notes.ID, f.owner.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.fs.Purge(ctx, f.notes.ID); err != nil {
		t.Fatal(err)
	}
	// The row itself goes with the node (FK cascade), asserted in
	// preview.TestExtractionFollowsTheCurrentVersion.
	if present("notes", f.notes.ID) || present("friday", f.notes.ID) {
		t.Error("a purged file is still found")
	}
}

// M43: a sub-page's location runs through the pages above it, by title, up to
// the folder holding the topmost one — and only as far up as the caller can
// see.
func TestSearchSubPageLocation(t *testing.T) {
	srv, q := searchEnv(t)
	f := newContentFixture(t, srv, q, "pageloc")

	projects := mkNode(t, q, f.space, pgtype.UUID{}, store.NodeKindDir, "Projects", f.owner)
	plan := putIndexed(t, srv, q, f.space, projects.ID, pgtype.UUID{}, "Plan.md", "# Plan\n", f.owner)
	sub := putIndexed(t, srv, q, f.space, plan.ID, pgtype.UUID{}, "Zebra roadmap.md", "# Roadmap\n", f.owner)
	leaf := putIndexed(t, srv, q, f.space, sub.ID, pgtype.UUID{}, "Zebra leaf.md", "# Leaf\n", f.owner)
	inFolder := putIndexed(t, srv, q, f.space, projects.ID, pgtype.UUID{}, "Zebra notes.md", "# Notes\n", f.owner)

	location := func(u store.User, id pgtype.UUID) string {
		t.Helper()
		h, i := findHit(searchAs(t, srv, u, "zebra"), id)
		if i < 0 {
			t.Fatalf("%s: hit %v missing", u.Email, id)
		}
		return h.ParentName
	}
	for _, c := range []struct {
		id   pgtype.UUID
		want string
	}{
		{sub.ID, "Projects / Plan"},
		{leaf.ID, "Projects / Plan / Zebra roadmap"},
		{inFolder.ID, "Projects"},
	} {
		if got := location(f.owner, c.id); got != c.want {
			t.Errorf("owner: location of %v = %q, want %q", c.id, got, c.want)
		}
	}

	// The member holds a grant on Plan.md only: Projects is not theirs to
	// name, the pages from Plan down are.
	grantDirect(t, srv, plan, f.member, f.owner, share.GrantCaps{Level: share.LevelView})
	if got := location(f.member, sub.ID); got != "Plan" {
		t.Errorf("member: location of the sub-page = %q, want %q", got, "Plan")
	}
	if got := location(f.member, leaf.ID); got != "Plan / Zebra roadmap" {
		t.Errorf("member: location of the leaf = %q, want %q", got, "Plan / Zebra roadmap")
	}

	// A grant on the leaf alone names nothing above it.
	other := feedUser(t, q, "pageloc-other")
	if err := srv.spaces.AddMember(context.Background(), f.space.ID, other.ID, false); err != nil {
		t.Fatalf("member: %v", err)
	}
	grantDirect(t, srv, leaf, other, f.owner, share.GrantCaps{Level: share.LevelView})
	if got := location(other, leaf.ID); got != "" {
		t.Errorf("leaf-only grantee: location = %q, want none", got)
	}
}
