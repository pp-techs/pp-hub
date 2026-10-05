package httpapi

import (
	"context"
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nhatbui/pp-hub/apps/server/internal/fs"
	"github.com/nhatbui/pp-hub/apps/server/internal/share"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

// Pages (M43): a block document is a page, and a page under a page is its
// sub-page — the node tree is the page tree (fs.CanContain). These routes
// feed the editor's page tree, breadcrumb, header and backlinks. See
// docs/architecture/milestones/M43.md.

// pageMetaDTO is one page as the editor's PageStore holds it.
type pageMetaDTO struct {
	ID string `json:"id"`
	// ParentID is the parent page, null when the parent is not a page or
	// the caller cannot view it: an id is never leaked.
	ParentID *string `json:"parent_id"`
	// Title is the node's name without `.md`; retitling is renaming.
	Title string `json:"title"`
	// Icon (an emoji) and Cover (`attachment:<hash>`, an attachment of this
	// page) come from the frontmatter, as last indexed.
	Icon        *string `json:"icon"`
	Cover       *string `json:"cover"`
	HasChildren bool    `json:"has_children"`
	// Trashed: the page, or a folder or page above it, is in the trash.
	Trashed bool `json:"trashed"`
}

// pageDTO answers GET /page: the page and its ancestor pages, root first.
type pageDTO struct {
	Page      pageMetaDTO   `json:"page"`
	Ancestors []pageMetaDTO `json:"ancestors"`
}

func optionalText(t pgtype.Text) *string {
	if !t.Valid || t.String == "" {
		return nil
	}
	return &t.String
}

func optionalID(id pgtype.UUID) *string {
	if !id.Valid {
		return nil
	}
	s := uuidString(id)
	return &s
}

func newPageMeta(id, parent pgtype.UUID, name string, icon, cover pgtype.Text, hasChildren, trashed bool) pageMetaDTO {
	return pageMetaDTO{
		ID:          uuidString(id),
		ParentID:    optionalID(parent),
		Title:       fs.PageTitle(name),
		Icon:        optionalText(icon),
		Cover:       optionalText(cover),
		HasChildren: hasChildren,
		Trashed:     trashed,
	}
}

// isPageRow reports whether a chain row is a block document.
func isPageRow(c store.ListPageChainRow) bool {
	return c.Kind == store.NodeKindFile && fs.IsMarkdown(c.Mime)
}

// childPageCounts is how many live sub-pages each of ids holds, in one query.
func (s *Server) childPageCounts(ctx context.Context, ids []pgtype.UUID) (map[pgtype.UUID]int64, error) {
	out := make(map[pgtype.UUID]int64, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := s.store.CountChildPages(ctx, ids)
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.ParentID] = r.Pages
	}
	return out, nil
}

// attachChildPageCounts fills ChildPageCount on the block documents among
// nodes, in one batched query. A failure costs the badge, not the listing.
func (s *Server) attachChildPageCounts(ctx context.Context, nodes []nodeDTO) {
	ids := make([]pgtype.UUID, 0, len(nodes))
	for _, n := range nodes {
		if n.Kind != string(store.NodeKindFile) || !fs.IsMarkdown(n.Mime) {
			continue
		}
		var id pgtype.UUID
		if err := id.Scan(n.ID); err == nil {
			ids = append(ids, id)
		}
	}
	counts, err := s.childPageCounts(ctx, ids)
	if err != nil {
		return
	}
	for i := range nodes {
		var id pgtype.UUID
		if err := id.Scan(nodes[i].ID); err == nil {
			nodes[i].ChildPageCount = counts[id]
		}
	}
}

// buildPage answers the page chain[0] and its ancestors chain[1:shown], the
// rows the caller may be told about. A row is trashed when it or anything
// above it within chain is.
func (s *Server) buildPage(ctx context.Context, chain []store.ListPageChainRow, shown int) (pageDTO, error) {
	ids := make([]pgtype.UUID, shown)
	for i := range ids {
		ids[i] = chain[i].ID
	}
	counts, err := s.childPageCounts(ctx, ids)
	if err != nil {
		return pageDTO{}, err
	}
	trashed := make([]bool, len(chain))
	for i, above := len(chain)-1, false; i >= 0; i-- {
		above = above || chain[i].DeletedAt.Valid
		trashed[i] = above
	}
	metas := make([]pageMetaDTO, shown)
	for i := range shown {
		var parent pgtype.UUID
		if i+1 < shown {
			parent = chain[i+1].ID
		}
		c := chain[i]
		metas[i] = newPageMeta(c.ID, parent, c.Name, c.Icon, c.Cover, counts[c.ID] > 0, trashed[i])
	}
	out := pageDTO{Page: metas[0], Ancestors: make([]pageMetaDTO, 0, shown-1)}
	for i := shown - 1; i >= 1; i-- {
		out.Ancestors = append(out.Ancestors, metas[i])
	}
	return out, nil
}

// requirePage resolves {nodeID} as a page the caller can view — a trashed
// one included — with its chain of ancestors and the caller's access to each
// of them, resolved in one query. Anything else is 404: a page that cannot be
// viewed is not confirmed to exist.
func (s *Server) requirePage(w http.ResponseWriter, r *http.Request) ([]store.ListPageChainRow, map[pgtype.UUID]share.Resolved, bool) {
	nodeID, err := uuidParam(r, "nodeID")
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "invalid node id")
		return nil, nil, false
	}
	chain, err := s.store.ListPageChain(r.Context(), nodeID)
	if err != nil {
		Error(w, r, err)
		return nil, nil, false
	}
	if len(chain) == 0 || !isPageRow(chain[0]) {
		writeProblem(w, http.StatusNotFound, "Not Found", "no such page")
		return nil, nil, false
	}
	ids := make([]pgtype.UUID, len(chain))
	for i, c := range chain {
		ids[i] = c.ID
	}
	// Access resolves through tombstoned ancestors the way the live tree
	// does (NodeAccessCaps), so a page in the trash answers as it would
	// have before it went there.
	res, err := s.share.AccessCaps(r.Context(), ids, caller(r).ID)
	if err != nil {
		Error(w, r, err)
		return nil, nil, false
	}
	if !share.EffectiveCaps(res[chain[0].ID]).View {
		writeProblem(w, http.StatusNotFound, "Not Found", "no such page")
		return nil, nil, false
	}
	return chain, res, true
}

// shownAncestors counts chain[0] and the ancestors after it that are pages
// visible(i) allows, stopping at the first that is not.
func shownAncestors(chain []store.ListPageChainRow, visible func(int) bool) int {
	n := 1
	for n < len(chain) && isPageRow(chain[n]) && visible(n) {
		n++
	}
	return n
}

// handleNodePage answers a page and its ancestor pages, root first. The chain
// stops at the first ancestor that is not a page or that the caller cannot
// view, and the topmost page shown then has no parent_id.
func (s *Server) handleNodePage(w http.ResponseWriter, r *http.Request) {
	chain, res, ok := s.requirePage(w, r)
	if !ok {
		return
	}
	shown := shownAncestors(chain, func(i int) bool { return share.EffectiveCaps(res[chain[i].ID]).View })
	out, err := s.buildPage(r.Context(), chain, shown)
	if err != nil {
		Error(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// handleNodePages lists a page's live sub-pages in tree order. View inherits
// downward, so whoever views the page views every sub-page: nothing to
// filter.
func (s *Server) handleNodePages(w http.ResponseWriter, r *http.Request) {
	chain, _, ok := s.requirePage(w, r)
	if !ok {
		return
	}
	s.writeChildPages(w, r, chain)
}

// writeChildPages answers chain[0]'s live sub-pages, trashed when it is.
func (s *Server) writeChildPages(w http.ResponseWriter, r *http.Request, chain []store.ListPageChainRow) {
	rows, err := s.store.ListChildPages(r.Context(), chain[0].ID)
	if err != nil {
		Error(w, r, err)
		return
	}
	trashed := false
	for _, c := range chain {
		trashed = trashed || c.DeletedAt.Valid
	}
	out := make([]pageMetaDTO, 0, len(rows))
	for _, c := range rows {
		out = append(out, newPageMeta(c.ID, chain[0].ID, c.Name, c.Icon, c.Cover, c.HasChildren, trashed))
	}
	writeJSON(w, http.StatusOK, out)
}

// handleNodeBacklinks lists the live pages that link to this one or hold it
// as a sub-page, those the caller can view. A backlink's parent is named only
// when it is a page the caller can view too.
func (s *Server) handleNodeBacklinks(w http.ResponseWriter, r *http.Request) {
	chain, _, ok := s.requirePage(w, r)
	if !ok {
		return
	}
	rows, err := s.store.ListBacklinks(r.Context(), chain[0].ID)
	if err != nil {
		Error(w, r, err)
		return
	}
	ids := make([]pgtype.UUID, 0, 2*len(rows))
	for _, b := range rows {
		ids = append(ids, b.ID)
		if b.ParentIsPage {
			ids = append(ids, b.ParentID)
		}
	}
	res, err := s.share.AccessCaps(r.Context(), ids, caller(r).ID)
	if err != nil {
		Error(w, r, err)
		return
	}
	viewable := func(id pgtype.UUID) bool { return share.EffectiveCaps(res[id]).View }
	out := make([]pageMetaDTO, 0, len(rows))
	for _, b := range rows {
		if !viewable(b.ID) {
			continue
		}
		var parent pgtype.UUID
		if b.ParentIsPage && viewable(b.ParentID) {
			parent = b.ParentID
		}
		out = append(out, newPageMeta(b.ID, parent, b.Name, b.Icon, b.Cover, b.HasChildren, false))
	}
	writeJSON(w, http.StatusOK, out)
}

// sharePage resolves ?node= inside the link as a page, with its chain cut at
// the link's root: nothing above what the link grants is named. The link
// covers only live nodes (shareScope), so nothing here is trashed.
func (s *Server) sharePage(w http.ResponseWriter, r *http.Request) ([]store.ListPageChainRow, bool) {
	sh, node, ok := s.shareScope(w, r, "node")
	if !ok {
		return nil, false
	}
	chain, err := s.store.ListPageChain(r.Context(), node.ID)
	if err != nil {
		Error(w, r, err)
		return nil, false
	}
	root := 0
	for root < len(chain) && chain[root].ID != sh.NodeID {
		root++
	}
	if root == len(chain) || !isPageRow(chain[0]) {
		writeProblem(w, http.StatusNotFound, "Not Found", "not found in this share")
		return nil, false
	}
	return chain[:root+1], true
}

// handleSharePage is handleNodePage through a link: ancestors stop at the
// link's root.
func (s *Server) handleSharePage(w http.ResponseWriter, r *http.Request) {
	chain, ok := s.sharePage(w, r)
	if !ok {
		return
	}
	out, err := s.buildPage(r.Context(), chain, shownAncestors(chain, func(int) bool { return true }))
	if err != nil {
		Error(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// handleSharePages is handleNodePages through a link. Whatever is under a
// node the link covers, it covers too.
func (s *Server) handleSharePages(w http.ResponseWriter, r *http.Request) {
	chain, ok := s.sharePage(w, r)
	if !ok {
		return
	}
	s.writeChildPages(w, r, chain)
}
