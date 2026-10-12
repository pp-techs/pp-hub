package blockdoc

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/url"
	"path"
	"strings"
	"time"
	"unicode"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nhatbui/pp-hub/apps/server/internal/blob"
	"github.com/nhatbui/pp-hub/apps/server/internal/fs"
	"github.com/nhatbui/pp-hub/apps/server/internal/mdoc"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

// Export is a subtree ready to be written as a zip — one block document with
// its sub-pages, or a folder — laid out before anything is written, since a
// link between two pages in it becomes a relative path:
//
//	Doc.md               a page
//	Doc/Sub.md           its sub-pages, recursively
//	Doc/Sub/Deep.md
//	assets/photo.png     the attachments of the pages beside it
//	Doc/assets/…
//
// Each page is written as its markdown with every attachment it references
// and holds (its cover included) pointing into the assets/ beside it, and a
// page link pointing at the page's .md when that page is in the archive, at
// its address in pp-hub otherwise. A relative reference (`![](images/a.png)`)
// to a file or folder in the archive points at its entry; one to a file
// outside it is copied into the assets/ beside the page when the export's
// Reach allows, and stays as written otherwise. Names are unique within a
// directory, case-insensitively, a page's folder included. Any other file is
// copied as it is. Everything that can fail with a status to report fails
// while preparing; WriteZip runs once the response is committed.
type Export struct {
	// Name is the archive's file name, `<document base>.zip` for a
	// document export.
	Name string

	files Files
	// external is pp-hub's address, for links to pages outside the archive.
	external string
	reach    Reach
	root     []*exportItem
	// pages maps every page's id to its markdown's entry, titles to its
	// title.
	pages  map[string]string
	titles map[string]string
	// entries maps every node in the archive to its entry, for relative
	// references.
	entries map[pgtype.UUID]string
	// first is the document export's own page, prepared up front so that
	// what fails reading it is still a status.
	first *exportPage
}

// Reach decides whether a page's relative reference to target, a file
// outside the archive, may be copied into it. nil copies nothing.
type Reach func(ctx context.Context, page, target store.Node) (bool, error)

// exportItem is one node's place in the archive.
type exportItem struct {
	node store.Node
	page bool
	// path is the entry: the file, the page's markdown, or the folder.
	path     string
	assets   *assetsDir // a page's: the assets/ beside it
	children []*exportItem
}

// assetsDir is the assets/ directory the pages of one archive directory
// share, its file names unique across all of them.
type assetsDir struct {
	name  string // as linked from beside it
	path  string
	taken map[string]bool
}

// exportPage is a page ready to be written.
type exportPage struct {
	markdown []byte
	assets   []exportAsset
}

// exportAsset is an entry under assets/: one of the page's attachments
// (hash), or a file a relative reference named (file).
type exportAsset struct {
	hash string
	file pgtype.UUID
	path string // the entry
}

// PrepareExport plans doc's archive — doc and its sub-pages — and reads doc
// itself: ErrNotMarkdown and ErrTooLarge are doc's. A reference to an
// attachment a page does not hold stays as it is: it resolves nowhere, in
// pp-hub or out of it. external is pp-hub's address (PP_EXTERNAL_URL); reach
// decides which files outside the archive its relative references bring in.
func (s *Service) PrepareExport(ctx context.Context, doc store.Node, external string, reach Reach) (*Export, error) {
	e, err := s.plan(ctx, doc, external, true)
	if err != nil {
		return nil, err
	}
	e.reach = reach
	item := e.root[0]
	if e.first, err = e.preparePage(ctx, item); err != nil {
		return nil, err
	}
	md := path.Base(item.path)
	e.Name = strings.TrimSuffix(md, path.Ext(md)) + ".zip"
	return e, nil
}

// PrepareZip plans the archive of the folder dir: its contents, the pages
// among them exported as PrepareExport does.
func (s *Service) PrepareZip(ctx context.Context, dir store.Node, external string) (*Export, error) {
	return s.plan(ctx, dir, external, false)
}

// plan lays out root's subtree: root itself as the archive's one entry when
// self is set (a document), else its contents.
func (s *Service) plan(ctx context.Context, root store.Node, external string, self bool) (*Export, error) {
	tree, err := s.files.Tree(ctx, root)
	if err != nil {
		return nil, err
	}
	e := &Export{
		files:    s.files,
		external: strings.TrimRight(external, "/"),
		pages:    map[string]string{},
		titles:   map[string]string{},
		entries:  map[pgtype.UUID]string{},
	}
	top := tree.Children
	if self {
		top = []*fs.TreeNode{tree}
	}
	e.root = e.layout("", top)
	return e, nil
}

// layout names the entries of one archive directory, then of each below it.
// The shared assets/ is named last, so a folder or page called "assets" keeps
// its name and the attachments take "assets-2".
func (e *Export) layout(dir string, nodes []*fs.TreeNode) []*exportItem {
	taken := map[string]bool{}
	items := make([]*exportItem, 0, len(nodes))
	var pages []*exportItem
	for _, n := range nodes {
		it := &exportItem{node: n.Node, page: n.Page}
		switch {
		case n.Node.Kind == store.NodeKindDir:
			name := cleanName(n.Node.Name)
			if name == "" {
				name = "folder"
			}
			it.path = path.Join(dir, uniqueName(name, taken))
			it.children = e.layout(it.path, n.Children)
		case n.Page:
			md, folder := pageNames(n.Node.Name, len(n.Children) > 0, taken)
			it.path = path.Join(dir, md)
			if folder != "" {
				it.children = e.layout(path.Join(dir, folder), n.Children)
			}
			id := uuidString(n.Node.ID)
			e.pages[id] = it.path
			e.titles[id] = fs.PageTitle(n.Node.Name)
			pages = append(pages, it)
		default:
			name := cleanName(n.Node.Name)
			if name == "" {
				name = "file"
			}
			it.path = path.Join(dir, uniqueName(name, taken))
		}
		e.entries[n.Node.ID] = it.path
		items = append(items, it)
	}
	if len(pages) > 0 {
		name := uniqueName("assets", taken)
		a := &assetsDir{name: name, path: path.Join(dir, name), taken: map[string]bool{}}
		for _, it := range pages {
			it.assets = a
		}
	}
	return items
}

// pageNames names a page's markdown and, when it has sub-pages, the folder
// they go in: the same base, both free in taken.
func pageNames(name string, folder bool, taken map[string]bool) (md, dir string) {
	md = markdownName(name)
	ext := path.Ext(md)
	base := strings.TrimSuffix(md, ext)
	if base == "" {
		base = "document"
	}
	candidate := base
	for n := 2; taken[strings.ToLower(candidate+ext)] || (folder && taken[strings.ToLower(candidate)]); n++ {
		candidate = fmt.Sprintf("%s-%d", base, n)
	}
	taken[strings.ToLower(candidate+ext)] = true
	if folder {
		taken[strings.ToLower(candidate)] = true
		dir = candidate
	}
	return candidate + ext, dir
}

// preparePage reads a page and writes its markdown for the archive, naming
// its assets in the assets/ beside it.
func (e *Export) preparePage(ctx context.Context, it *exportItem) (*exportPage, error) {
	_, ver, rc, err := e.files.OpenFile(ctx, it.node.ID)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	// Parsed from the same bytes the lossy path rewrites.
	src, err := readSource(ver, rc)
	if err != nil {
		return nil, err
	}
	parsed, err := mdoc.Parse(src)
	if err != nil {
		return nil, err
	}

	held, err := e.files.ListAttachments(ctx, it.node.ID)
	if err != nil {
		return nil, err
	}
	byHash := make(map[string]store.NodeAttachment, len(held))
	for _, a := range held {
		byHash[a.BlobHash] = a
	}
	p := &exportPage{}
	links := map[string]string{} // hash → link written into the markdown
	add := func(hash string) {
		att, ok := byHash[hash]
		if !ok || links[hash] != "" {
			return
		}
		file := uniqueName(assetName(att), it.assets.taken)
		p.assets = append(p.assets, exportAsset{hash: hash, path: path.Join(it.assets.path, file)})
		links[hash] = escapeLink(it.assets.name) + "/" + escapeLink(file)
	}
	for _, hash := range mdoc.Attachments(parsed.Doc) {
		add(hash)
	}
	icon, cover := mdoc.FrontmatterMeta(parsed.Frontmatter)
	if hash, ok := strings.CutPrefix(cover, mdoc.AttachmentScheme); ok {
		add(hash)
	}

	subs, linked := mdoc.PageRefs(parsed.Doc)
	refs := append(subs, linked...)
	hrefs := make(map[string]string, len(refs))
	var outside []string
	dir := path.Dir(it.path)
	for _, id := range refs {
		if target, ok := e.pages[id]; ok {
			hrefs[id] = relativeLink(dir, target)
			continue
		}
		hrefs[id] = e.external + "/edit/" + id
		outside = append(outside, id)
	}
	rel, err := e.relativeLinks(ctx, it, p, parsed.Doc)
	if err != nil {
		return nil, err
	}

	switch {
	case len(links) == 0 && len(refs) == 0 && len(rel) == 0:
		p.markdown = src
	case parsed.Lossy:
		// Rendering a lossy document writes what the block set can hold and
		// drops the rest — an export must not be the thing that loses it. The
		// references are rewritten in the bytes instead; a hash is 64 hex
		// digits, so `attachment:<hash>` cannot be a prefix of anything else,
		// and a page's link target is `/edit/<id>` closed by its paren.
		out := src
		for hash, link := range links {
			out = bytes.ReplaceAll(out, []byte(mdoc.AttachmentScheme+hash), []byte(link))
		}
		for id, href := range hrefs {
			out = bytes.ReplaceAll(out, []byte("](/edit/"+id+")"), []byte("]("+href+")"))
		}
		// Best effort: a relative reference written as a plain `](path)`.
		for ref, link := range rel {
			out = bytes.ReplaceAll(out, []byte("]("+ref+")"), []byte("]("+link+")"))
		}
		p.markdown = out
	default:
		titles := e.titles
		if len(outside) > 0 {
			// Titles of pages outside the archive, read once per page.
			found, err := e.files.PageTitles(ctx, it.node.SpaceID, outside)
			if err != nil {
				return nil, err
			}
			titles = make(map[string]string, len(e.titles)+len(found))
			for id, t := range e.titles {
				titles[id] = t
			}
			for id, t := range found {
				titles[id] = t
			}
		}
		doc := mdoc.MapAttachments(parsed.Doc, func(hash string) string {
			if link, ok := links[hash]; ok {
				return link
			}
			return mdoc.AttachmentScheme + hash
		})
		doc = mdoc.MapRelative(doc, func(ref string) (string, bool) {
			link, ok := rel[ref]
			return link, ok
		})
		fm := parsed.Frontmatter
		if hash, ok := strings.CutPrefix(cover, mdoc.AttachmentScheme); ok && links[hash] != "" {
			fm = mdoc.SetFrontmatterMeta(fm, icon, links[hash])
		}
		p.markdown = mdoc.RenderWith(doc, fm, mdoc.RenderOptions{
			PageTitle: func(id string) string { return titles[id] },
			PageHref:  func(id string) string { return hrefs[id] },
		})
	}
	return p, nil
}

// relativeLinks maps the page's relative references to where they lead in the
// archive: a node in it to its entry; a file outside it, when the export's
// reach allows, to a copy added to p's assets. A reference that resolves to
// nothing, or to what the archive does not carry, is absent and stays as it
// is. A `#fragment` stays on the link (a heading in a linked page).
func (e *Export) relativeLinks(ctx context.Context, it *exportItem, p *exportPage, doc mdoc.Node) (map[string]string, error) {
	out := map[string]string{}
	copied := map[pgtype.UUID]string{}
	dir := path.Dir(it.path)
	for _, ref := range mdoc.RelativeRefs(doc) {
		segs, _ := mdoc.RelativePath(ref)
		target, err := e.files.ResolveRelative(ctx, it.node, segs)
		if errors.Is(err, fs.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		fragment := ""
		if i := strings.IndexByte(ref, '#'); i >= 0 {
			fragment = ref[i:]
		}
		link, ok := "", false
		if entry, in := e.entries[target.ID]; in {
			link, ok = relativeLink(dir, entry), true
		} else if link, ok = copied[target.ID]; !ok && target.Kind == store.NodeKindFile && e.reach != nil {
			if ok, err = e.reach(ctx, it.node, target); err != nil {
				return nil, err
			}
			if ok {
				name := cleanName(target.Name)
				if name == "" {
					name = "file"
				}
				file := uniqueName(name, it.assets.taken)
				p.assets = append(p.assets, exportAsset{file: target.ID, path: path.Join(it.assets.path, file)})
				link = escapeLink(it.assets.name) + "/" + escapeLink(file)
				copied[target.ID] = link
			}
		}
		if ok && link+fragment != ref {
			out[ref] = link + fragment
		}
	}
	return out, nil
}

// relativeLink is the link from a markdown in dir to the entry target, each
// segment escaped as escapeLink does.
func relativeLink(dir, target string) string {
	from := splitPath(dir)
	to := splitPath(target)
	i := 0
	for i < len(from) && i < len(to)-1 && from[i] == to[i] {
		i++
	}
	parts := make([]string, 0, len(from)-i+len(to)-i)
	for range from[i:] {
		parts = append(parts, "..")
	}
	for _, seg := range to[i:] {
		parts = append(parts, escapeLink(seg))
	}
	return strings.Join(parts, "/")
}

func splitPath(p string) []string {
	if p == "" || p == "." {
		return nil
	}
	return strings.Split(p, "/")
}

// WriteZip streams the archive to w, entry by entry: each page's markdown
// then its assets, each other file copied straight from the blob store, one
// open at a time. A page too large to read or no longer markdown is copied
// as it is. Any other failure ends the archive unfinished with the error,
// which the caller can only log — the response is already under way.
func (e *Export) WriteZip(ctx context.Context, w io.Writer) error {
	zw := zip.NewWriter(w)
	if err := e.writeItems(ctx, zw, e.root); err != nil {
		return err
	}
	return zw.Close()
}

func (e *Export) writeItems(ctx context.Context, zw *zip.Writer, items []*exportItem) error {
	for _, it := range items {
		var err error
		switch {
		case it.node.Kind == store.NodeKindDir:
		case it.page:
			err = e.writePage(ctx, zw, it)
		default:
			err = e.writeFile(ctx, zw, it)
		}
		if err != nil {
			return err
		}
		if err := e.writeItems(ctx, zw, it.children); err != nil {
			return err
		}
	}
	return nil
}

func (e *Export) writePage(ctx context.Context, zw *zip.Writer, it *exportItem) error {
	// A document export's own page was prepared with the plan.
	p := e.first
	if p == nil || it != e.root[0] {
		var err error
		if p, err = e.preparePage(ctx, it); errors.Is(err, ErrTooLarge) || errors.Is(err, ErrNotMarkdown) {
			return e.writeFile(ctx, zw, it)
		} else if err != nil {
			return fmt.Errorf("%s: %w", it.path, err)
		}
	}
	entry, err := zw.CreateHeader(&zip.FileHeader{Name: it.path, Method: zip.Deflate})
	if err != nil {
		return err
	}
	if _, err := entry.Write(p.markdown); err != nil {
		return err
	}
	for _, a := range p.assets {
		if err := e.writeAsset(ctx, zw, it.node.ID, a); err != nil {
			return err
		}
	}
	return nil
}

func (e *Export) writeFile(ctx context.Context, zw *zip.Writer, it *exportItem) error {
	_, _, rc, err := e.files.OpenFile(ctx, it.node.ID)
	if err != nil {
		return fmt.Errorf("%s: %w", it.path, err)
	}
	defer rc.Close()
	entry, err := zw.Create(it.path)
	if err != nil {
		return err
	}
	_, err = io.Copy(entry, rc)
	return err
}

func (e *Export) writeAsset(ctx context.Context, zw *zip.Writer, doc pgtype.UUID, a exportAsset) error {
	var (
		mimeType string
		modified time.Time
		rc       io.ReadCloser
	)
	if a.file.Valid {
		_, ver, r, err := e.files.OpenFile(ctx, a.file)
		if err != nil {
			return fmt.Errorf("%s: %w", a.path, err)
		}
		mimeType, modified, rc = ver.Mime, ver.CreatedAt.Time, r
	} else {
		att, r, err := e.files.OpenAttachment(ctx, doc, a.hash)
		if err != nil {
			return fmt.Errorf("attachment %s: %w", a.hash[:12], err)
		}
		mimeType, modified, rc = att.Mime, att.CreatedAt.Time, r
	}
	defer rc.Close()
	// Images and videos are compressed already; deflating them again costs
	// CPU for nothing.
	method := zip.Deflate
	if t := strings.ToLower(mimeType); strings.HasPrefix(t, "image/") || strings.HasPrefix(t, "video/") {
		method = zip.Store
	}
	entry, err := zw.CreateHeader(&zip.FileHeader{Name: a.path, Method: method, Modified: modified})
	if err != nil {
		return err
	}
	_, err = io.Copy(entry, rc)
	return err
}

// markdownName is the entry the document is written as: its own name, with
// .md added if it lacks it.
func markdownName(name string) string {
	name = cleanName(name)
	if name == "" {
		name = "document"
	}
	if !strings.EqualFold(path.Ext(name), ".md") {
		name += ".md"
	}
	return name
}

// assetName is an attachment's file name made safe for a zip entry: the base
// of whatever path it carried, without control characters, falling back to
// the hash (and an extension for its type) when nothing is left.
func assetName(att store.NodeAttachment) string {
	if name := cleanName(att.Name); name != "" {
		return name
	}
	name := att.BlobHash
	if blob.IsHash(name) {
		name = name[:12]
	}
	if exts, _ := mime.ExtensionsByType(att.Mime); len(exts) > 0 {
		name += exts[0]
	}
	return name
}

func cleanName(name string) string {
	if i := strings.LastIndexAny(name, `/\`); i >= 0 {
		name = name[i+1:]
	}
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, name)
	name = strings.TrimSpace(name)
	if name == "." || name == ".." {
		return ""
	}
	return name
}

// uniqueName is name, or name with "-2", "-3"… before its extension, whichever
// is not taken yet (case-insensitively: archives are unpacked onto
// case-insensitive filesystems). It marks the result taken.
func uniqueName(name string, taken map[string]bool) string {
	ext := path.Ext(name)
	base := strings.TrimSuffix(name, ext)
	if base == "" {
		base, ext = name, ""
	}
	candidate := name
	for n := 2; taken[strings.ToLower(candidate)]; n++ {
		candidate = fmt.Sprintf("%s-%d%s", base, n, ext)
	}
	taken[strings.ToLower(candidate)] = true
	return candidate
}

// escapeLink makes a file name safe inside a markdown link destination:
// percent-encoded like a URL path segment, parentheses included, so a space or
// an unbalanced paren cannot end the link early.
func escapeLink(name string) string {
	s := url.PathEscape(name)
	return strings.NewReplacer("(", "%28", ")", "%29").Replace(s)
}

func uuidString(id pgtype.UUID) string {
	v, _ := id.Value()
	s, _ := v.(string)
	return s
}
