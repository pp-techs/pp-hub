package httpapi

import (
	"context"
	"encoding/xml"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"golang.org/x/net/webdav"

	"github.com/nhatbui/pp-hub/apps/server/internal/collab"
	ppfs "github.com/nhatbui/pp-hub/apps/server/internal/fs"
	"github.com/nhatbui/pp-hub/apps/server/internal/share"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

// The WebDAV namespace, as one person sees it (dav.go):
//
//	/                     their spaces, and "Shared with me" when other spaces shared with them
//	/<space>/             the space's top level: what they own and what is shared with them
//	/<space>/<folder>/…   a folder, listed as the web client lists it
//	…/<Doc>.pages/        a document's sub-pages, beside the document
//
// Every listing is the web client's listing — the same service calls, the
// same capability resolution — narrowed to what a mount can use: files the
// person may only view, not download, are left out, and so are extension
// nodes, the archive and the trash. Paths are labels, not node names: two
// entry points of one name at a space's top level are told apart by a short
// id, and a space by its name.

type davKind int

const (
	davRoot    davKind = iota // "/"
	davSpace                  // a space's top level
	davShared                 // "/Shared with me"
	davNode                   // a folder or a file
	davPages                  // a document's sub-pages
	davScratch                // a scratch file (dav_names.go)
)

// davEntry is one resource of the namespace.
type davEntry struct {
	kind  davKind
	path  string // clean, rooted at "/" (the mount), no trailing slash
	name  string
	space store.Space
	// node is the folder or file for davNode, and the document for davPages.
	node    store.NodeWithMeta
	caps    share.Caps
	scratch davScratchFile
	mod     time.Time
}

func (e *davEntry) isDir() bool { return e.kind != davNode && e.kind != davScratch || e.isNodeDir() }

func (e *davEntry) isNodeDir() bool { return e.kind == davNode && e.node.Kind == store.NodeKindDir }

func (e *davEntry) isFile() bool { return e.kind == davNode && e.node.Kind == store.NodeKindFile }

func (e *davEntry) isMarkdown() bool { return e.isFile() && ppfs.IsMarkdown(e.node.VerMime) }

// container is where creating name inside e lands: the space, the parent
// (invalid for the space's top level), and whether it is a document's pages
// rather than a folder. ok is false where nothing can be created — the root,
// "Shared with me", a file.
func (e *davEntry) container() (spaceID, parentID pgtype.UUID, pages, ok bool) {
	switch {
	case e.kind == davSpace:
		return e.space.ID, pgtype.UUID{}, false, true
	case e.isNodeDir():
		return e.node.SpaceID, e.node.ID, false, true
	case e.kind == davPages:
		return e.node.SpaceID, e.node.ID, true, true
	}
	return pgtype.UUID{}, pgtype.UUID{}, false, false
}

var errDavNotFound = os.ErrNotExist

// davView resolves paths for one request, as its caller. Listings are
// memoised for the request: PROPFIND stats every child it lists, and each
// stat would otherwise walk the path from the root again.
type davView struct {
	s      *Server
	r      *http.Request
	ctx    context.Context
	u      *store.User
	client string // the token, as the audit log names it
	lists  map[string][]*davEntry
}

func (s *Server) newDavView(r *http.Request) *davView {
	return &davView{
		s: s, r: r, ctx: r.Context(), u: caller(r),
		client: davClient(r.Context()),
		lists:  map[string][]*davEntry{},
	}
}

// key is name in dir as the caller sees it, for davState.
func (v *davView) key(dir, name string) davNameKey {
	return davNameKey{user: v.u.ID, dir: dir, name: name}
}

// forget drops memoised listings after a write changed what they hold.
func (v *davView) forget() { v.lists = map[string][]*davEntry{} }

// davClean is p as a namespace path: rooted, cleaned, no trailing slash.
func davClean(p string) string {
	p = path.Clean("/" + p)
	return p
}

// resolve finds the entry at p.
func (v *davView) resolve(p string) (*davEntry, error) {
	p = davClean(p)
	e := &davEntry{kind: davRoot, path: "/", name: ""}
	if p == "/" {
		return e, nil
	}
	for _, seg := range strings.Split(strings.TrimPrefix(p, "/"), "/") {
		child, err := v.child(e, seg)
		if err != nil {
			return nil, err
		}
		e = child
	}
	return e, nil
}

// child is the entry named name inside dir.
func (v *davView) child(dir *davEntry, name string) (*davEntry, error) {
	if !dir.isDir() {
		return nil, errDavNotFound
	}
	list, err := v.list(dir)
	if err != nil {
		return nil, err
	}
	for _, c := range list {
		if c.name == name {
			return c, nil
		}
	}
	return nil, errDavNotFound
}

// resolveParent is the folder p would be created in, and its last segment.
func (v *davView) resolveParent(p string) (*davEntry, string, error) {
	p = davClean(p)
	if p == "/" {
		return nil, "", errDavNotFound
	}
	dir, err := v.resolve(path.Dir(p))
	if err != nil {
		return nil, "", err
	}
	return dir, path.Base(p), nil
}

// list is dir's children.
func (v *davView) list(dir *davEntry) ([]*davEntry, error) {
	if l, ok := v.lists[dir.path]; ok {
		return l, nil
	}
	var (
		out []*davEntry
		err error
	)
	switch dir.kind {
	case davRoot:
		out, err = v.listRoot()
	case davSpace:
		var rows []store.NodeWithMeta
		if rows, err = v.s.fs.EntryPointsWithMeta(v.ctx, v.u.ID, dir.space.ID, true); err == nil {
			out, err = v.nodeEntries(dir, rows, false)
		}
	case davShared:
		var rows []store.NodeWithMeta
		if rows, err = v.s.fs.ExternalEntryPointsWithMeta(v.ctx, v.u.ID, v.s.share.ExternalSharingEnabled()); err == nil {
			out, err = v.nodeEntries(dir, rows, false)
		}
	case davNode:
		if !dir.isNodeDir() {
			return nil, errDavNotFound
		}
		var rows []store.NodeWithMeta
		if dir.caps.View {
			rows, err = v.s.fs.ListWithMeta(v.ctx, dir.node.SpaceID, dir.node.ID)
		} else {
			rows, err = v.s.fs.ListGrantedWithMeta(v.ctx, dir.node.SpaceID, dir.node.ID, v.u.ID)
		}
		if err == nil {
			out, err = v.nodeEntries(dir, rows, false)
		}
	case davPages:
		var rows []store.NodeWithMeta
		if rows, err = v.s.fs.ListWithMeta(v.ctx, dir.node.SpaceID, dir.node.ID); err == nil {
			out, err = v.nodeEntries(dir, rows, true)
		}
	default:
		return nil, errDavNotFound
	}
	if err != nil {
		return nil, err
	}
	v.lists[dir.path] = out
	return out, nil
}

// listRoot is the person's spaces, and "Shared with me" when anything is.
func (v *davView) listRoot() ([]*davEntry, error) {
	spaces, err := v.s.spaces.ListForUser(v.ctx, v.u.ID)
	if err != nil {
		return nil, err
	}
	out := make([]*davEntry, 0, len(spaces)+1)
	taken := map[string]bool{davSharedRoot: true}
	for _, sp := range spaces {
		if sp.ArchivedAt.Valid {
			continue
		}
		name := davLabel(sp.Name)
		if taken[name] {
			name = davDisambiguate(name, uuidString(sp.ID))
		}
		taken[name] = true
		out = append(out, &davEntry{kind: davSpace, path: path.Join("/", name), name: name, space: sp, mod: sp.CreatedAt.Time})
	}
	ext, err := v.s.fs.ExternalEntryPointsWithMeta(v.ctx, v.u.ID, v.s.share.ExternalSharingEnabled())
	if err != nil {
		return nil, err
	}
	if len(ext) > 0 {
		out = append(out, &davEntry{kind: davShared, path: "/" + davSharedRoot, name: davSharedRoot})
	}
	return out, nil
}

// nodeEntries turns a listing into entries: the caller's capabilities
// resolved in one batch, what a mount cannot use dropped, duplicate names
// told apart, sub-page folders and scratch files added, and a file an
// editor is replacing hidden.
func (v *davView) nodeEntries(dir *davEntry, rows []store.NodeWithMeta, pagesOnly bool) ([]*davEntry, error) {
	ids := make([]pgtype.UUID, 0, len(rows))
	for _, n := range rows {
		ids = append(ids, n.ID)
	}
	res, err := v.s.share.AccessCaps(v.ctx, ids, v.u.ID)
	if err != nil {
		return nil, err
	}
	archive := dir.space.ArchiveRootID
	var docs []pgtype.UUID
	kept := make([]*davEntry, 0, len(rows))
	for _, n := range rows {
		caps := share.EffectiveCaps(res[n.ID])
		switch {
		case n.Kind == store.NodeKindExt,
			archive.Valid && n.ID == archive,
			!caps.View,
			n.Kind == store.NodeKindFile && !caps.Download,
			pagesOnly && !ppfs.IsMarkdown(n.VerMime):
			continue
		}
		e := &davEntry{kind: davNode, name: n.Name, space: dir.space, node: n, caps: caps, mod: n.CreatedAt.Time}
		if n.Kind == store.NodeKindFile {
			e.mod = n.VerCreatedAt.Time
			if ppfs.IsMarkdown(n.VerMime) {
				docs = append(docs, n.ID)
			}
		}
		if !e.space.ID.Valid {
			e.space = store.Space{ID: n.SpaceID}
		}
		kept = append(kept, e)
	}
	counts, err := v.s.childPageCounts(v.ctx, docs)
	if err != nil {
		return nil, err
	}

	// Stable labels: the first of a name keeps it, by creation then id.
	sort.SliceStable(kept, func(i, j int) bool {
		a, b := kept[i].node, kept[j].node
		if !a.CreatedAt.Time.Equal(b.CreatedAt.Time) {
			return a.CreatedAt.Time.Before(b.CreatedAt.Time)
		}
		return uuidString(a.ID) < uuidString(b.ID)
	})
	taken := map[string]bool{}
	out := make([]*davEntry, 0, len(kept))
	for _, e := range kept {
		if taken[e.name] {
			e.name = davDisambiguate(e.name, uuidString(e.node.ID))
		}
		taken[e.name] = true
		e.path = path.Join(dir.path, e.name)
		if v.s.dav.hidden(v.key(dir.path, e.name), e.node.ID) {
			continue
		}
		out = append(out, e)
	}
	for _, e := range kept {
		if !e.isMarkdown() {
			continue
		}
		name := davPagesName(e.name)
		if counts[e.node.ID] == 0 && !v.s.dav.hasPagesDir(v.key(dir.path, name)) {
			continue
		}
		if taken[name] {
			name += "~"
		}
		taken[name] = true
		out = append(out, &davEntry{kind: davPages, path: path.Join(dir.path, name), name: name, space: e.space, node: e.node, caps: e.caps, mod: e.mod})
	}
	for _, f := range v.s.dav.scratchIn(v.u.ID, dir.path) {
		if taken[f.key.name] {
			// A real file took the name (a lock-null reservation was
			// written): the file wins.
			continue
		}
		taken[f.key.name] = true
		out = append(out, &davEntry{kind: davScratch, path: path.Join(dir.path, f.key.name), name: f.key.name, space: dir.space, scratch: f, mod: f.mod})
	}
	return out, nil
}

// size is what the entry's bytes measure.
func (v *davView) size(e *davEntry) int64 {
	switch {
	case e.isFile():
		return e.node.VerSize
	case e.kind == davScratch:
		return e.scratch.size
	}
	return 0
}

// open reads the entry's bytes: a file's current version, a set-aside's
// version, or a scratch file's. The size is the byte count the reader
// yields. A block document open in the editor is flushed first, so a copy
// or a move carries what its editors see.
func (v *davView) open(e *davEntry) (io.ReadCloser, int64, error) {
	switch {
	case e.isFile():
		if e.isMarkdown() {
			if err := v.s.collab.Flush(v.ctx, e.node.ID); err != nil {
				return nil, 0, err
			}
		}
		_, ver, rc, err := v.s.fs.OpenFile(v.ctx, e.node.ID)
		return rc, ver.Size, err
	case e.kind == davScratch && e.scratch.alias.Valid:
		rc, _, err := v.openAlias(e.scratch)
		return rc, e.scratch.size, err
	case e.kind == davScratch && e.scratch.file != "":
		f, err := os.Open(e.scratch.file)
		return f, e.scratch.size, err
	case e.kind == davScratch:
		return io.NopCloser(strings.NewReader("")), 0, nil
	}
	return nil, 0, errDavNotFound
}

// openAlias reads the version a set-aside alias holds, if the caller may
// still download the file: an alias is a name for someone's file, not a
// copy of it, so it confers nothing the file does not. A file gone, or no
// longer the caller's to download, reads as not found.
func (v *davView) openAlias(f davScratchFile) (io.ReadCloser, store.Node, error) {
	node, err := v.s.fs.Get(v.ctx, f.alias)
	if err != nil {
		return nil, store.Node{}, errDavNotFound
	}
	caps, err := v.s.share.EffectiveCapsForNode(v.ctx, node, v.u.ID)
	if err != nil {
		return nil, store.Node{}, err
	}
	if !caps.Has(share.CapDownload) {
		v.s.dav.dropScratch(f.key)
		return nil, store.Node{}, errDavNotFound
	}
	rc, err := v.s.blobs.Get(v.ctx, f.aliasHash)
	if err != nil {
		return nil, store.Node{}, err
	}
	return rc, node, nil
}

// canCreateIn reports whether the caller may create a file in dir: a space
// they are a member of, or a folder or document they can edit.
func (v *davView) canCreateIn(dir *davEntry) bool {
	switch dir.kind {
	case davSpace:
		return true
	case davNode:
		return dir.isNodeDir() && dir.caps.Edit
	case davPages:
		return dir.caps.Edit
	}
	return false
}

// editorLocked reports whether an office editor holds node. A block
// document's room does not count: writes go through it (collab.Reseed).
func (v *davView) editorLocked(node pgtype.UUID) (bool, error) {
	tok, err := v.s.wopi.CurrentLock(v.ctx, node)
	if err != nil {
		return false, err
	}
	return tok != "" && !collab.IsLockToken(tok), nil
}

// lockKey is what a WebDAV lock on e is held against: the node, so two
// people reaching it by different paths contend for one lock, or the
// caller's own path for anything without a node.
func (v *davView) lockKey(e *davEntry, p string) string {
	if e != nil && e.kind == davNode {
		return "node:" + uuidString(e.node.ID)
	}
	return "path:" + uuidString(v.u.ID) + ":" + davClean(p)
}

// lockedByOther reports whether someone else's WebDAV lock covers p.
func (v *davView) lockedByOther(e *davEntry, p string) bool {
	return v.s.dav.lockedByOther(v.lockKey(e, p), v.u.ID, time.Now())
}

// --- golang.org/x/net/webdav adapters ---
//
// PROPFIND, PROPPATCH, LOCK and UNLOCK are answered by x/net/webdav over
// these adapters: it owns the XML. Everything that changes content is
// answered by dav.go itself, which is why the write half of the
// FileSystem is a refusal.

type davFS struct{ v *davView }

var _ webdav.FileSystem = davFS{}

func (f davFS) Stat(_ context.Context, name string) (os.FileInfo, error) {
	e, err := f.v.resolve(name)
	if err != nil {
		return nil, err
	}
	return davInfo{v: f.v, e: e}, nil
}

// OpenFile opens an entry for PROPFIND, which lists and reads properties.
// The one create it is asked for is LOCK on an unmapped name: that reserves
// the name (a lock-null resource) until the client writes it.
func (f davFS) OpenFile(_ context.Context, name string, flag int, _ os.FileMode) (webdav.File, error) {
	e, err := f.v.resolve(name)
	if errors.Is(err, errDavNotFound) && flag&os.O_CREATE != 0 {
		dir, base, perr := f.v.resolveParent(name)
		if perr != nil {
			return nil, perr
		}
		if !f.v.canCreateIn(dir) {
			return nil, os.ErrPermission
		}
		if err := f.v.s.dav.lockNull(f.v.key(dir.path, base)); err != nil {
			return nil, err
		}
		f.v.forget()
		e, err = f.v.resolve(name)
	}
	if err != nil {
		return nil, err
	}
	return &davFile{v: f.v, e: e}, nil
}

func (davFS) Mkdir(context.Context, string, os.FileMode) error { return os.ErrPermission }
func (davFS) RemoveAll(context.Context, string) error          { return os.ErrPermission }
func (davFS) Rename(context.Context, string, string) error     { return os.ErrPermission }

// davInfo is an entry as os.FileInfo, with the ETag and content type x/net
// would otherwise compute by reading the file.
type davInfo struct {
	v *davView
	e *davEntry
}

var (
	_ webdav.ETager       = davInfo{}
	_ webdav.ContentTyper = davInfo{}
)

func (i davInfo) Name() string {
	if i.e.kind == davRoot {
		return "/"
	}
	return i.e.name
}
func (i davInfo) Size() int64 { return i.v.size(i.e) }
func (i davInfo) Mode() fs.FileMode {
	if i.e.isDir() {
		return fs.ModeDir | 0o755
	}
	return 0o644
}
func (i davInfo) ModTime() time.Time { return i.e.mod }
func (i davInfo) IsDir() bool        { return i.e.isDir() }
func (i davInfo) Sys() any           { return nil }

func (i davInfo) ETag(context.Context) (string, error) {
	if i.e.isFile() {
		return strconv.Quote(i.e.node.VerBlobHash), nil
	}
	return "", webdav.ErrNotImplemented
}

func (i davInfo) ContentType(context.Context) (string, error) {
	if i.e.isFile() && i.e.node.VerMime != "" {
		return i.e.node.VerMime, nil
	}
	return "application/octet-stream", nil
}

// davFile is an open entry: its listing for PROPFIND, its properties, and
// nothing else — content is served by dav.go.
type davFile struct {
	v *davView
	e *davEntry
}

var (
	_ webdav.File            = (*davFile)(nil)
	_ webdav.DeadPropsHolder = (*davFile)(nil)
)

func (f *davFile) Close() error                   { return nil }
func (f *davFile) Read([]byte) (int, error)       { return 0, os.ErrPermission }
func (f *davFile) Seek(int64, int) (int64, error) { return 0, os.ErrPermission }
func (f *davFile) Write([]byte) (int, error)      { return 0, os.ErrPermission }
func (f *davFile) Stat() (fs.FileInfo, error)     { return davInfo{v: f.v, e: f.e}, nil }
func (f *davFile) Readdir(int) ([]fs.FileInfo, error) {
	list, err := f.v.list(f.e)
	if err != nil {
		return nil, err
	}
	out := make([]fs.FileInfo, len(list))
	for i, c := range list {
		out[i] = davInfo{v: f.v, e: c}
	}
	return out, nil
}

var (
	davQuotaUsed      = xml.Name{Space: "DAV:", Local: "quota-used-bytes"}
	davQuotaAvailable = xml.Name{Space: "DAV:", Local: "quota-available-bytes"}
)

// DeadProps answers the space's quota (RFC 4331) on its top level, which
// Explorer and Finder show as the drive's capacity. Nothing else is held.
func (f *davFile) DeadProps() (map[xml.Name]webdav.Property, error) {
	if f.e.kind != davSpace {
		return nil, nil
	}
	sp := f.e.space
	props := map[xml.Name]webdav.Property{
		davQuotaUsed: {XMLName: davQuotaUsed, InnerXML: []byte(strconv.FormatInt(sp.UsedBytes, 10))},
	}
	if sp.QuotaBytes > 0 {
		free := max(sp.QuotaBytes-sp.UsedBytes, 0)
		props[davQuotaAvailable] = webdav.Property{XMLName: davQuotaAvailable, InnerXML: []byte(strconv.FormatInt(free, 10))}
	}
	return props, nil
}

// Patch accepts and drops every property a client sets. Finder and
// Explorer stamp times and attributes after each write and treat a refusal
// as a failed save; nothing here would read them back.
func (f *davFile) Patch(patches []webdav.Proppatch) ([]webdav.Propstat, error) {
	st := webdav.Propstat{Status: http.StatusOK}
	for _, p := range patches {
		for _, prop := range p.Props {
			st.Props = append(st.Props, webdav.Property{XMLName: prop.XMLName})
		}
	}
	return []webdav.Propstat{st}, nil
}

// davLocks is the webdav.LockSystem for one request: locks are held by a
// person against a node (davView.lockKey), and an office editor's lock on a
// file refuses a WebDAV lock on it.
type davLocks struct{ v *davView }

var _ webdav.LockSystem = davLocks{}

func (l davLocks) Confirm(now time.Time, name0, name1 string, _ ...webdav.Condition) (func(), error) {
	for _, name := range []string{name0, name1} {
		if name == "" {
			continue
		}
		e, _ := l.v.resolve(name)
		if l.v.s.dav.lockedByOther(l.v.lockKey(e, name), l.v.u.ID, now) {
			return nil, webdav.ErrConfirmationFailed
		}
	}
	return func() {}, nil
}

// Create takes a lock only where the caller could write: a lock is a claim
// to change the thing, and a view-only reader holding one would stop every
// editor's mount for a day. x/net/webdav answers ErrLocked with 423, any
// other error with 500, so a refusal is ErrLocked.
func (l davLocks) Create(now time.Time, d webdav.LockDetails) (string, error) {
	e, err := l.v.resolve(d.Root)
	switch {
	case err == nil && e.kind == davNode:
		if !e.caps.Edit {
			return "", webdav.ErrLocked
		}
		if e.isFile() {
			locked, err := l.v.editorLocked(e.node.ID)
			if err != nil {
				return "", err
			}
			if locked {
				return "", webdav.ErrLocked
			}
		}
	case err == nil && e.kind == davScratch:
		dir, _, perr := l.v.resolveParent(d.Root)
		if perr != nil || !l.v.canCreateIn(dir) {
			return "", webdav.ErrLocked
		}
	case err == nil:
		// The root, a space, "Shared with me", a pages folder.
		return "", webdav.ErrLocked
	case errors.Is(err, errDavNotFound):
		// A name about to be created (lock-null): its folder must take it.
		dir, _, perr := l.v.resolveParent(d.Root)
		if perr != nil || !l.v.canCreateIn(dir) {
			return "", webdav.ErrLocked
		}
		e = nil
	default:
		return "", err
	}
	token, err := l.v.s.dav.createLock(l.v.lockKey(e, d.Root), l.v.u.ID, d, now)
	if errors.Is(err, errDavTooManyLocks) {
		return "", webdav.ErrLocked
	}
	return token, err
}

func (l davLocks) Refresh(now time.Time, token string, d time.Duration) (webdav.LockDetails, error) {
	return l.v.s.dav.refreshLock(token, l.v.u.ID, d, now)
}

// Unlock releases a lock, and with it a name the lock reserved and nothing
// was written to (RFC 4918 §7.3).
func (l davLocks) Unlock(_ time.Time, token string) error {
	lock, err := l.v.s.dav.unlock(token, l.v.u.ID)
	if err != nil {
		return err
	}
	if dir, name, err := l.v.resolveParent(lock.details.Root); err == nil {
		k := l.v.key(dir.path, name)
		if f, ok := l.v.s.dav.scratchFile(k); ok && f.lockNull {
			l.v.s.dav.dropScratch(k)
		}
	}
	return nil
}
