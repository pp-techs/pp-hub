package httpapi

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgtype"
	"golang.org/x/net/webdav"

	ppfs "github.com/nhatbui/pp-hub/apps/server/internal/fs"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

// Writes over WebDAV (dav.go). Every one of them is an /api/v1 call:
//
//   - a new file is a tus upload, sent whole in its creation request;
//   - a new version of a file is the same upload naming the file;
//   - a new version of a block document is PUT /nodes/{id}/text, through
//     the document's room, so the people editing it see it at once;
//   - a new sub-page is POST /spaces/{id}/documents under its document;
//   - rename, move, copy and delete are the routes the web client calls.
//
// Two rules sit on top. A write is checked against the version the same
// token last read of the file: when someone else has written since, the
// bytes land beside it as a conflicted copy and nobody's work is lost. And a
// safe save — a temporary copy renamed over the file, or the file deleted
// and its replacement written under its name — becomes a new version of the
// file, so it keeps its id, history, shares and comments.

// davWrite is bytes on their way to a file.
type davWrite struct {
	body   io.Reader
	length int64
	// ifMatch is the client's own precondition (If-Match), "" for none.
	ifMatch string
	// base checks the version this token last read: a client's save does,
	// an explicit COPY over a file does not.
	base bool
}

// davTarget is a file being written: its node and current version.
type davTarget struct {
	node store.Node
	ver  store.NodeVersion
}

func (v *davView) target(id pgtype.UUID) (davTarget, error) {
	node, err := v.s.fs.Get(v.ctx, id)
	if err != nil {
		return davTarget{}, err
	}
	if node.Kind != store.NodeKindFile {
		return davTarget{}, ppfs.ErrNotFile
	}
	ver, err := v.s.store.GetCurrentVersion(v.ctx, id)
	if err != nil {
		return davTarget{}, err
	}
	return davTarget{node: node, ver: ver}, nil
}

// --- PUT ---

func (v *davView) put(w http.ResponseWriter, r *http.Request, p string) {
	dir, name, err := v.resolveParent(p)
	if err != nil {
		davFail(w, http.StatusConflict, "the parent folder does not exist")
		return
	}
	length := davLength(r)
	if limit := v.s.uploadCap(); limit > 0 && length > limit {
		davFail(w, http.StatusRequestEntityTooLarge, "this file is larger than this server accepts")
		return
	}
	existing, err := v.child(dir, name)
	if err != nil && !errors.Is(err, errDavNotFound) {
		davResolveFail(w, r, err)
		return
	}
	if existing != nil && existing.isDir() {
		davFail(w, http.StatusMethodNotAllowed, "a folder has this name")
		return
	}
	if existing != nil && r.Header.Get("If-None-Match") == "*" && !(existing.kind == davScratch && existing.scratch.lockNull) {
		davFail(w, http.StatusPreconditionFailed, "this file already exists")
		return
	}
	if v.lockedByOther(existing, p) {
		davFail(w, webdav.StatusLocked, "someone else holds a lock on this file")
		return
	}

	if scratchTarget(name, existing) {
		if !v.canCreateIn(dir) {
			davFail(w, http.StatusForbidden, "files cannot be created here")
			return
		}
		if _, err := v.s.dav.putScratch(v.key(dir.path, name), r.Body, v.s.uploadCap()); err != nil {
			davFail(w, davScratchStatus(err), err.Error())
			return
		}
		davWritten(w, existing == nil, "")
		return
	}

	body := io.Reader(r.Body)
	if length < 0 {
		// Neither Content-Length nor X-Expected-Entity-Length: the upload
		// needs its length before the first byte, so the body is spooled.
		spool, n, err := davSpool(r.Body, v.s.uploadCap())
		if err != nil {
			davFail(w, http.StatusRequestEntityTooLarge, err.Error())
			return
		}
		defer func() { _ = spool.Close(); _ = os.Remove(spool.Name()) }()
		body, length = spool, n
	}
	status, msg, etag := v.write(dir, name, existing, davWrite{
		body: body, length: length, ifMatch: r.Header.Get("If-Match"), base: true,
	})
	if status >= 300 {
		davFail(w, status, msg)
		return
	}
	davWritten(w, status == http.StatusCreated, etag)
}

// davWritten answers a successful write.
func davWritten(w http.ResponseWriter, created bool, etag string) {
	if etag != "" {
		w.Header().Set("ETag", strconv.Quote(etag))
	}
	if created {
		w.WriteHeader(http.StatusCreated)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// davLength is the body's declared length: Content-Length, or Finder's
// X-Expected-Entity-Length beside a chunked body; -1 when neither says.
func davLength(r *http.Request) int64 {
	if r.ContentLength >= 0 {
		return r.ContentLength
	}
	if n, err := strconv.ParseInt(r.Header.Get("X-Expected-Entity-Length"), 10, 64); err == nil && n >= 0 {
		return n
	}
	return -1
}

// davSpool copies body to a temporary file, at most limit bytes when limit
// is positive, and rewinds it.
func davSpool(body io.Reader, limit int64) (*os.File, int64, error) {
	f, err := os.CreateTemp("", "pp-dav-put-*")
	if err != nil {
		return nil, 0, err
	}
	src := body
	if limit > 0 {
		src = io.LimitReader(body, limit+1)
	}
	n, err := io.Copy(f, src)
	if err == nil && limit > 0 && n > limit {
		err = errors.New("this file is larger than this server accepts")
	}
	if err == nil {
		_, err = f.Seek(0, io.SeekStart)
	}
	if err != nil {
		_ = f.Close()
		_ = os.Remove(f.Name())
		return nil, 0, err
	}
	return f, n, nil
}

// write lands wr at name in dir, where existing is what the name holds now
// (nil for nothing). It answers the status the client gets — 201 for a new
// file or a conflicted copy, 204 for a new version — with a reason on
// failure and the written version's hash on success when it is known.
func (v *davView) write(dir *davEntry, name string, existing *davEntry, wr davWrite) (int, string, string) {
	defer v.forget()
	k := v.key(dir.path, name)
	if existing != nil && existing.kind == davScratch {
		// A name locked before it was written (lock-null): the write
		// creates it.
		existing = nil
		v.s.dav.dropScratch(k)
	}
	if existing != nil {
		return v.replace(dir, name, existing.node.ID, wr)
	}
	if t, ok := v.s.dav.tomb(k); ok {
		// The file that had this name a moment ago was trashed, or set
		// aside, by this person: this is its safe save.
		if !t.trashed || v.call(http.MethodPost, "/nodes/"+uuidString(t.node)+"/restore", nil, nil).ok() {
			if status, msg, etag := v.replace(dir, name, t.node, wr); status < 300 {
				v.s.dav.unbury(k)
				return http.StatusCreated, "", etag
			} else if status != http.StatusNotFound {
				return status, msg, ""
			}
		}
		v.s.dav.unbury(k)
	}
	return v.create(dir, name, wr)
}

// replace writes a new version of the file id, or a conflicted copy beside
// it when its current version is not the one this token last read.
func (v *davView) replace(dir *davEntry, name string, id pgtype.UUID, wr davWrite) (int, string, string) {
	t, err := v.target(id)
	if errors.Is(err, ppfs.ErrNotFound) || errors.Is(err, ppfs.ErrNotFile) {
		return http.StatusNotFound, "", ""
	}
	if err != nil {
		return http.StatusInternalServerError, "", ""
	}
	cur := t.ver.BlobHash
	if wr.ifMatch != "" && wr.ifMatch != "*" && wr.ifMatch != strconv.Quote(cur) {
		return http.StatusPreconditionFailed, "the file changed since that version", ""
	}
	stale := false
	if wr.base && wr.ifMatch == "" {
		if base, ok := v.s.dav.lastRead(v.client, id); ok && base != cur {
			stale = true
		}
	}
	if ppfs.IsMarkdown(t.ver.Mime) {
		return v.replaceMarkdown(dir, name, t, wr, stale)
	}
	if stale {
		return v.conflict(dir, name, t, wr)
	}
	res := v.upload(map[string]string{"node_id": uuidString(id)}, wr.length, wr.body)
	if res.status == webdav.StatusLocked && res.header.Get("Location") == "" {
		// An office editor has the file open, and the upload was refused
		// before it opened — before a byte was read. The editor's next save
		// would overwrite this one.
		return v.conflict(dir, name, t, wr)
	}
	if !res.ok() {
		return davStatus(res), res.detail(), ""
	}
	return v.written(id, http.StatusNoContent)
}

// replaceMarkdown writes a block document's next version through its room.
// A document is text an editor holds whole, so its bytes are read whole.
func (v *davView) replaceMarkdown(dir *davEntry, name string, t davTarget, wr davWrite, stale bool) (int, string, string) {
	data, err := io.ReadAll(io.LimitReader(wr.body, maxAgentTextBytes+1))
	if err != nil {
		return http.StatusBadRequest, "the upload was interrupted", ""
	}
	if len(data) > maxAgentTextBytes {
		return http.StatusRequestEntityTooLarge, "a markdown document is written whole, and this one is larger than 1 MiB", ""
	}
	if !utf8.Valid(data) {
		return http.StatusUnsupportedMediaType, "a markdown document must be UTF-8 text", ""
	}
	again := davWrite{body: bytes.NewReader(data), length: int64(len(data))}
	if stale {
		return v.conflict(dir, name, t, again)
	}
	res := v.callJSON(http.MethodPut, "/nodes/"+uuidString(t.node.ID)+"/text", map[string]string{
		"markdown": string(data), "base_etag": t.ver.BlobHash,
	})
	switch {
	case res.status == http.StatusConflict:
		// The room held edits nobody had saved, and they became a version
		// first: this write is no longer based on what is there.
		return v.conflict(dir, name, t, again)
	case !res.ok():
		return davStatus(res), res.detail(), ""
	}
	return v.written(t.node.ID, http.StatusNoContent)
}

// conflict lands wr beside the file it lost to, under a name that says so.
func (v *davView) conflict(dir *davEntry, name string, t davTarget, wr davWrite) (int, string, string) {
	cname := davConflictName(name, displayName(*v.u), time.Now())
	if _, _, _, ok := dir.container(); ok {
		return v.create(dir, cname, wr)
	}
	// "Shared with me" has no place of its own to put a file: beside the
	// file is its folder.
	return v.createIn(t.node.SpaceID, t.node.ParentID, false, cname, wr, nil)
}

// create writes a new file named name in dir.
func (v *davView) create(dir *davEntry, name string, wr davWrite) (int, string, string) {
	spaceID, parentID, pages, ok := dir.container()
	if !ok {
		return http.StatusForbidden, "files cannot be created here", ""
	}
	if msg := v.caseClash(dir, name); msg != "" {
		return http.StatusConflict, msg, ""
	}
	return v.createIn(spaceID, parentID, pages, name, wr, dir)
}

// createIn writes a new file named name under parentID in spaceID — a
// sub-page when pages — and remembers it as read by this token. dir, when
// known, is where it will be listed.
func (v *davView) createIn(spaceID, parentID pgtype.UUID, pages bool, name string, wr davWrite, dir *davEntry) (int, string, string) {
	if _, ok := plainFileName(name); !ok {
		return http.StatusBadRequest, "not a usable file name", ""
	}
	// A name can be taken by something the caller is not shown — a file
	// they may only view, somebody's private item at a space's top level.
	// The upload would land under a suffixed name the client never asked
	// for; refusing says what happened.
	siblings, err := v.s.fs.List(v.ctx, spaceID, parentID)
	if err != nil {
		return http.StatusInternalServerError, "", ""
	}
	for _, n := range siblings {
		if n.Name == name {
			return http.StatusConflict, "this folder already holds something by that name that you cannot open", ""
		}
	}
	var status int
	var msg, etag string
	if pages {
		status, msg, etag = v.createPage(spaceID, parentID, name, wr)
	} else {
		res := v.upload(map[string]string{
			"filename": name, "space_id": uuidString(spaceID), "parent_id": uuidString(parentID),
		}, wr.length, wr.body)
		if !res.ok() {
			return davStatus(res), res.detail(), ""
		}
		status = http.StatusCreated
	}
	if status >= 300 || dir == nil {
		return status, msg, etag
	}
	v.forget()
	if e, err := v.child(dir, name); err == nil && e.isFile() {
		v.s.dav.rememberRead(v.client, e.node.ID, e.node.VerBlobHash)
		// A lock the client took on the name before writing it now holds
		// the file.
		v.s.dav.rekeyLocks(v.lockKey(nil, e.path), v.lockKey(e, e.path))
		return http.StatusCreated, "", e.node.VerBlobHash
	}
	return status, msg, etag
}

// createPage makes a sub-page of the document parentID: an empty document
// through POST /documents, then its content through its room.
func (v *davView) createPage(spaceID, parentID pgtype.UUID, name string, wr davWrite) (int, string, string) {
	if !strings.EqualFold(path.Ext(name), ".md") {
		return http.StatusForbidden, "only markdown documents can be sub-pages", ""
	}
	data, err := io.ReadAll(io.LimitReader(wr.body, maxAgentTextBytes+1))
	switch {
	case err != nil:
		return http.StatusBadRequest, "the upload was interrupted", ""
	case len(data) > maxAgentTextBytes:
		return http.StatusRequestEntityTooLarge, "a markdown document is written whole, and this one is larger than 1 MiB", ""
	case !utf8.Valid(data):
		return http.StatusUnsupportedMediaType, "a markdown document must be UTF-8 text", ""
	}
	res := v.callJSON(http.MethodPost, "/spaces/"+uuidString(spaceID)+"/documents", map[string]string{
		"kind": "markdown", "name": name, "parent_id": uuidString(parentID),
	})
	if !res.ok() {
		return davStatus(res), res.detail(), ""
	}
	var created nodeDTO
	var id pgtype.UUID
	if json.Unmarshal(res.body.Bytes(), &created) != nil || id.Scan(created.ID) != nil {
		return http.StatusInternalServerError, "", ""
	}
	if len(data) > 0 {
		t, err := v.target(id)
		if err != nil {
			return http.StatusInternalServerError, "", ""
		}
		res := v.callJSON(http.MethodPut, "/nodes/"+created.ID+"/text", map[string]string{
			"markdown": string(data), "base_etag": t.ver.BlobHash,
		})
		if !res.ok() {
			// The page exists only to hold this content; without it, it
			// goes.
			if v.call(http.MethodDelete, "/nodes/"+created.ID, nil, nil).ok() {
				v.call(http.MethodDelete, "/nodes/"+created.ID+"/purge", nil, nil)
			}
			return davStatus(res), res.detail(), ""
		}
	}
	return v.written(id, http.StatusCreated)
}

// written remembers the version now current as read by this token — the
// client holds exactly those bytes — and answers status with its hash.
func (v *davView) written(id pgtype.UUID, status int) (int, string, string) {
	ver, err := v.s.store.GetCurrentVersion(v.ctx, id)
	if err != nil {
		return status, "", ""
	}
	v.s.dav.rememberRead(v.client, id, ver.BlobHash)
	return status, "", ver.BlobHash
}

// upload sends a whole file through tus creation-with-upload: one request
// that opens the upload, carries every byte and commits it. The upload is
// abandoned if it did not commit, so a failed write holds no quota.
func (v *davView) upload(meta map[string]string, length int64, body io.Reader) *bufferedResponse {
	pairs := make([]string, 0, len(meta))
	for k, val := range meta {
		if val == "" {
			continue
		}
		pairs = append(pairs, k+" "+base64.StdEncoding.EncodeToString([]byte(val)))
	}
	res := v.call(http.MethodPost, "/uploads", http.Header{
		"Tus-Resumable":   {tusVersion},
		"Upload-Length":   {strconv.FormatInt(length, 10)},
		"Upload-Metadata": {strings.Join(pairs, ",")},
		"Content-Type":    {"application/offset+octet-stream"},
	}, body)
	loc := strings.TrimPrefix(res.header.Get("Location"), "/api/v1")
	if res.status == http.StatusCreated && res.header.Get("Upload-Offset") == strconv.FormatInt(length, 10) {
		return res
	}
	if loc != "" {
		v.call(http.MethodDelete, loc, http.Header{"Tus-Resumable": {tusVersion}}, nil)
	}
	if res.status == http.StatusCreated {
		// Opened, but the body ended before the length it declared.
		return &bufferedResponse{header: http.Header{}, status: http.StatusBadRequest}
	}
	return res
}

// davStatus is the status a failed API call means to a WebDAV client: the
// API's own, except a full space, which WebDAV calls 507.
func davStatus(res *bufferedResponse) int {
	if res.status == http.StatusRequestEntityTooLarge && strings.Contains(res.body.String(), "Quota Exceeded") {
		return webdav.StatusInsufficientStorage
	}
	return res.status
}

// --- COPY, MOVE ---

func (v *davView) copyMove(w http.ResponseWriter, r *http.Request, p string) {
	dstPath, ok := davDestination(r)
	if !ok {
		davFail(w, http.StatusBadRequest, "Destination must be a path under "+davPrefix+"/")
		return
	}
	move := r.Method == "MOVE"
	overwrite := r.Header.Get("Overwrite") != "F"
	src, err := v.resolve(p)
	if err != nil {
		davResolveFail(w, r, err)
		return
	}
	if dstPath == src.path {
		davFail(w, http.StatusForbidden, "the destination is the source")
		return
	}
	dstDir, dstName, err := v.resolveParent(dstPath)
	if err != nil {
		davFail(w, http.StatusConflict, "the destination folder does not exist")
		return
	}
	dst, err := v.child(dstDir, dstName)
	if err != nil && !errors.Is(err, errDavNotFound) {
		davResolveFail(w, r, err)
		return
	}
	switch {
	case dst != nil && dst.kind != davNode && dst.kind != davScratch:
		davFail(w, http.StatusForbidden, "that name is a space or a document's pages folder, which cannot be replaced")
		return
	case strings.HasPrefix(src.path+"/", dstPath+"/"):
		davFail(w, http.StatusForbidden, "the destination holds the source")
		return
	case strings.HasPrefix(dstPath, src.path+"/"):
		davFail(w, http.StatusForbidden, "a folder cannot go inside itself")
		return
	}
	if dst != nil && dst.kind == davScratch && dst.scratch.lockNull {
		v.s.dav.dropScratch(v.key(dstDir.path, dstName))
		v.forget()
		dst = nil
	}
	if dst != nil && !overwrite {
		davFail(w, http.StatusPreconditionFailed, "something already has this name")
		return
	}
	if (move && v.lockedByOther(src, p)) || v.lockedByOther(dst, dstPath) {
		davFail(w, webdav.StatusLocked, "someone else holds a lock on this")
		return
	}
	var status int
	var msg string
	switch {
	case src.kind == davScratch:
		status, msg = v.fromScratch(src, dstDir, dstName, dst, move)
	case src.kind == davNode && move:
		status, msg = v.moveNode(src, dstDir, dstName, dst)
	case src.kind == davNode:
		status, msg = v.copyNode(src, dstDir, dstName, dst, r.Header.Get("Depth") == "0")
	default:
		status, msg = http.StatusForbidden, "a space, the root and a pages folder cannot be moved or copied"
	}
	if status >= 300 {
		davFail(w, status, msg)
		return
	}
	w.WriteHeader(status)
}

// davDestination is the namespace path a Destination header names. Only
// its path is read: behind a proxy the host the client used is not this
// server's.
func davDestination(r *http.Request) (string, bool) {
	u, err := url.Parse(r.Header.Get("Destination"))
	if err != nil || u.Path == "" {
		return "", false
	}
	if u.Path != davPrefix && !strings.HasPrefix(u.Path, davPrefix+"/") {
		return "", false
	}
	return davClean(strings.TrimPrefix(u.Path, davPrefix)), true
}

// davMoved is the status of a MOVE or COPY: 201 when nothing was replaced.
func davMoved(replaced bool) int {
	if replaced {
		return http.StatusNoContent
	}
	return http.StatusCreated
}

// fromScratch moves or copies a scratch file. Onto another scratch name it
// stays scratch; onto a real name it is a write of its bytes there — the
// last step of a safe save.
func (v *davView) fromScratch(src, dstDir *davEntry, dstName string, dst *davEntry, move bool) (int, string) {
	srcKey, dstKey := src.scratch.key, v.key(dstDir.path, dstName)
	if scratchTarget(dstName, dst) {
		if !v.canCreateIn(dstDir) {
			return http.StatusForbidden, "files cannot be created here"
		}
		if move {
			v.s.dav.renameScratch(srcKey, dstKey)
			return davMoved(dst != nil), ""
		}
		rc, _, err := v.open(src)
		if err != nil {
			return http.StatusNotFound, ""
		}
		defer rc.Close()
		if _, err := v.s.dav.putScratch(dstKey, rc, v.s.uploadCap()); err != nil {
			return davScratchStatus(err), err.Error()
		}
		return davMoved(dst != nil), ""
	}
	if dst != nil && dst.isDir() {
		return http.StatusConflict, "a folder has this name"
	}
	if f := src.scratch; f.alias.Valid && move && dst == nil && dstDir.path == srcKey.dir {
		if t, ok := v.s.dav.tomb(dstKey); ok && !t.trashed && t.node == f.alias {
			// An editor put the file back where it was, unchanged.
			v.s.dav.unbury(dstKey)
			v.s.dav.dropScratch(srcKey)
			return http.StatusCreated, ""
		}
		if f.pending {
			// Set aside, then moved on to another name: a rename of the
			// file, done in two steps.
			res := v.callJSON(http.MethodPatch, "/nodes/"+uuidString(f.alias)+"/rename", map[string]string{"name": dstName})
			if !res.ok() {
				return davStatus(res), res.detail()
			}
			v.s.dav.unbury(v.key(srcKey.dir, f.origin))
			v.s.dav.dropScratch(srcKey)
			v.forget()
			return http.StatusCreated, ""
		}
	}
	rc, size, err := v.open(src)
	if err != nil {
		return http.StatusNotFound, ""
	}
	defer rc.Close()
	status, msg, _ := v.write(dstDir, dstName, dst, davWrite{body: rc, length: size, base: true})
	if status >= 300 {
		return status, msg
	}
	if move {
		v.s.dav.dropScratch(srcKey)
	}
	return davMoved(dst != nil), ""
}

// moveNode moves or renames a file or folder.
func (v *davView) moveNode(src, dstDir *davEntry, dstName string, dst *davEntry) (int, string) {
	srcKey, dstKey := v.key(path.Dir(src.path), src.name), v.key(dstDir.path, dstName)
	free := dst == nil || dst.kind == davScratch
	sameDir := path.Dir(src.path) == dstDir.path
	if src.isFile() && free && sameDir && (isDavScratch(dstName) || isDavSetAside(src.name, dstName)) {
		// An editor setting the file aside while it writes the new copy
		// (Word: report.docx → ~WRL0001.tmp; vim: notes → notes~). The
		// file stays; the mount sees its current version under the new
		// name, and nothing under its own until the save lands there.
		if !src.caps.Edit {
			return http.StatusForbidden, "insufficient permission"
		}
		if err := v.s.dav.setAside(dstKey, src.node.ID, src.node.VerBlobHash, src.name, src.node.VerSize); err != nil {
			return davScratchStatus(err), err.Error()
		}
		v.s.dav.bury(srcKey, src.node.ID, false)
		return davMoved(dst != nil), ""
	}
	if free && isDavScratch(dstName) {
		return http.StatusForbidden, "that name is reserved for client metadata files"
	}
	if dst != nil && dst.kind == davScratch {
		v.s.dav.dropScratch(dstKey)
		v.forget()
		dst = nil
	}
	if src.isFile() && (dst == nil || dst.isFile()) {
		if _, buried := v.s.dav.tomb(dstKey); dst != nil || buried {
			// A temporary copy renamed over the file: its bytes are the
			// file's next version, and the copy is gone.
			if !src.caps.Edit {
				return http.StatusForbidden, "insufficient permission"
			}
			rc, size, err := v.open(src)
			if err != nil {
				return http.StatusNotFound, ""
			}
			status, msg, _ := v.write(dstDir, dstName, dst, davWrite{body: rc, length: size, base: true})
			_ = rc.Close()
			if status >= 300 {
				return status, msg
			}
			v.dispose(src)
			return davMoved(dst != nil), ""
		}
	}
	if status, msg := v.canRelocate(src, dstDir); status != 0 {
		return status, msg
	}
	var undo func()
	if dst != nil {
		var status int
		var msg string
		if undo, status, msg = v.trashForOverwrite(dst); status != 0 {
			return status, msg
		}
	}
	if status, msg := v.relocate(src, dstDir, dstName); status >= 300 {
		if undo != nil {
			undo()
		}
		return status, msg
	}
	return davMoved(dst != nil), ""
}

// trashForOverwrite moves dst to the trash so something can take its name,
// and returns how to bring it back if that fails.
func (v *davView) trashForOverwrite(dst *davEntry) (func(), int, string) {
	id := "/nodes/" + uuidString(dst.node.ID)
	if res := v.call(http.MethodDelete, id, nil, nil); !res.ok() {
		return nil, davStatus(res), res.detail()
	}
	v.forget()
	return func() {
		v.call(http.MethodPost, id+"/restore", nil, nil)
		v.forget()
	}, 0, ""
}

// canRelocate refuses, before anything is changed, a move relocate could
// only fail: there is no place to put it, or a folder would cross spaces.
func (v *davView) canRelocate(src, dstDir *davEntry) (int, string) {
	if path.Dir(src.path) == dstDir.path {
		return 0, ""
	}
	spaceID, _, _, ok := dstDir.container()
	switch {
	case !ok:
		return http.StatusForbidden, "nothing can be moved here"
	case spaceID != src.node.SpaceID && !src.isFile():
		return http.StatusForbidden, "a folder cannot move to another space; copy what it holds instead"
	case spaceID != src.node.SpaceID && !src.caps.Edit:
		return http.StatusForbidden, "insufficient permission"
	}
	return 0, ""
}

// dispose removes a file whose bytes became another file's version: purged
// when it was plainly the save's temporary copy — this person's, one
// version, minutes old — and trashed otherwise.
func (v *davView) dispose(src *davEntry) {
	id := "/nodes/" + uuidString(src.node.ID)
	if !v.call(http.MethodDelete, id, nil, nil).ok() {
		return
	}
	n := src.node
	if n.CreatedBy == v.u.ID && n.VerVersionNo <= 1 && time.Since(n.CreatedAt.Time) < davYoung {
		v.call(http.MethodDelete, id+"/purge", nil, nil)
	}
}

// relocate renames and moves src to dstName in dstDir, through the routes
// the web client uses, so every rule a move has applies. A failure part way
// is undone.
func (v *davView) relocate(src, dstDir *davEntry, dstName string) (int, string) {
	defer v.forget()
	if status, msg := v.canRelocate(src, dstDir); status != 0 {
		return status, msg
	}
	id := "/nodes/" + uuidString(src.node.ID)
	sameDir := path.Dir(src.path) == dstDir.path
	spaceID, parentID, _, _ := dstDir.container()
	if !sameDir && spaceID != src.node.SpaceID {
		// Another space is another quota and another set of people: the
		// file is copied there, and the original goes to the trash.
		rc, size, err := v.open(src)
		if err != nil {
			return http.StatusNotFound, ""
		}
		status, msg, _ := v.createIn(spaceID, parentID, false, dstName, davWrite{body: rc, length: size}, dstDir)
		_ = rc.Close()
		if status >= 300 {
			return status, msg
		}
		if res := v.call(http.MethodDelete, id, nil, nil); !res.ok() {
			v.forget()
			if c, err := v.child(dstDir, dstName); err == nil && c.kind == davNode {
				cid := "/nodes/" + uuidString(c.node.ID)
				if v.call(http.MethodDelete, cid, nil, nil).ok() {
					v.call(http.MethodDelete, cid+"/purge", nil, nil)
				}
			}
			return davStatus(res), res.detail()
		}
		return http.StatusCreated, ""
	}
	if msg := v.caseClash(dstDir, dstName); msg != "" && !strings.EqualFold(dstName, src.name) {
		return http.StatusConflict, msg
	}
	// A label the listing made up ("Report (1a2b3c4d).docx") is not a
	// name: carried along unchanged, the node keeps its own.
	newName := dstName
	if dstName == src.name {
		newName = src.node.Name
	}
	rename := func(name string) *bufferedResponse {
		return v.callJSON(http.MethodPatch, id+"/rename", map[string]string{"name": name})
	}
	move := func(parent pgtype.UUID) *bufferedResponse {
		return v.callJSON(http.MethodPatch, id+"/move", map[string]string{"parent_id": uuidString(parent)})
	}
	if sameDir {
		if newName == src.node.Name {
			return http.StatusCreated, ""
		}
		if res := rename(newName); !res.ok() {
			return davStatus(res), res.detail()
		}
		return http.StatusCreated, ""
	}
	// Move, then rename in the destination. When the destination already
	// holds the old name, rename first, then move.
	res := move(parentID)
	if res.ok() {
		if newName != src.node.Name {
			if res := rename(newName); !res.ok() {
				move(src.node.ParentID)
				return davStatus(res), res.detail()
			}
		}
		return http.StatusCreated, ""
	}
	if res.status != http.StatusConflict || newName == src.node.Name {
		return davStatus(res), res.detail()
	}
	if res := rename(newName); !res.ok() {
		return davStatus(res), res.detail()
	}
	if res := move(parentID); !res.ok() {
		rename(src.node.Name)
		return davStatus(res), res.detail()
	}
	return http.StatusCreated, ""
}

// copyNode copies a file or folder. A file's bytes are written anew at the
// destination — a copy of the file, not of its sub-pages, which a client
// copying a folder copies from the pages folder itself. A folder is copied
// by the copy route, whole, unless Depth: 0 asks for the folder alone.
func (v *davView) copyNode(src, dstDir *davEntry, dstName string, dst *davEntry, shallow bool) (int, string) {
	dstKey := v.key(dstDir.path, dstName)
	if scratchTarget(dstName, dst) {
		if !src.isFile() || !v.canCreateIn(dstDir) {
			return http.StatusForbidden, "that name is reserved for client metadata files"
		}
		rc, _, err := v.open(src)
		if err != nil {
			return http.StatusNotFound, ""
		}
		defer rc.Close()
		if _, err := v.s.dav.putScratch(dstKey, rc, v.s.uploadCap()); err != nil {
			return davScratchStatus(err), err.Error()
		}
		return davMoved(dst != nil), ""
	}
	if dst != nil && dst.kind == davScratch {
		v.s.dav.dropScratch(dstKey)
		v.forget()
		dst = nil
	}
	replaced := dst != nil
	if src.isFile() {
		var undo func()
		if dst != nil && !dst.isFile() {
			var status int
			var msg string
			if undo, status, msg = v.trashForOverwrite(dst); status != 0 {
				return status, msg
			}
			dst = nil
		}
		rc, size, err := v.open(src)
		if err != nil {
			if undo != nil {
				undo()
			}
			return http.StatusNotFound, ""
		}
		defer rc.Close()
		status, msg, _ := v.write(dstDir, dstName, dst, davWrite{body: rc, length: size})
		if status >= 300 {
			if undo != nil {
				undo()
			}
			return status, msg
		}
		return davMoved(replaced), ""
	}

	spaceID, parentID, pages, ok := dstDir.container()
	if !ok || pages {
		return http.StatusForbidden, "a folder cannot be copied here"
	}
	if spaceID != src.node.SpaceID {
		return http.StatusForbidden, "a folder cannot be copied to another space"
	}
	var undo func()
	if dst != nil {
		var status int
		var msg string
		if undo, status, msg = v.trashForOverwrite(dst); status != 0 {
			return status, msg
		}
	}
	fail := func(res *bufferedResponse, made string) (int, string) {
		if made != "" {
			// What was made so far goes; what was replaced comes back.
			if v.call(http.MethodDelete, made, nil, nil).ok() {
				v.call(http.MethodDelete, made+"/purge", nil, nil)
			}
		}
		if undo != nil {
			undo()
		}
		return davStatus(res), res.detail()
	}
	defer v.forget()
	if shallow {
		res := v.callJSON(http.MethodPost, "/spaces/"+uuidString(spaceID)+"/dirs", map[string]string{
			"name": dstName, "parent_id": uuidString(parentID),
		})
		if !res.ok() {
			return fail(res, "")
		}
		return davMoved(replaced), ""
	}
	// The copy route copies into a folder, or beside the original when it
	// names none — never to the space's top level, so a copy bound there
	// is made beside the original and then moved.
	res := v.callJSON(http.MethodPost, "/nodes/"+uuidString(src.node.ID)+"/copy", map[string]string{"parent_id": uuidString(parentID)})
	if !res.ok() {
		return fail(res, "")
	}
	var created nodeDTO
	if json.Unmarshal(res.body.Bytes(), &created) != nil {
		return http.StatusInternalServerError, ""
	}
	id := "/nodes/" + created.ID
	if !parentID.Valid && src.node.ParentID.Valid {
		if res := v.callJSON(http.MethodPatch, id+"/move", map[string]string{"parent_id": ""}); !res.ok() {
			return fail(res, id)
		}
	}
	if created.Name != dstName {
		if res := v.callJSON(http.MethodPatch, id+"/rename", map[string]string{"name": dstName}); !res.ok() {
			return fail(res, id)
		}
	}
	return davMoved(replaced), ""
}

// davScratchStatus is what a failed scratch write answers: 507 when the
// scratch bounds are reached.
func davScratchStatus(err error) int {
	if errors.Is(err, errDavScratchFull) {
		return webdav.StatusInsufficientStorage
	}
	return http.StatusInternalServerError
}

// scratchTarget reports whether writing name, which dst holds now, writes a
// scratch file. A real file wins its name even when it looks like scratch: a
// "notes.tmp" uploaded from the browser stays a file.
func scratchTarget(name string, dst *davEntry) bool {
	return isDavScratch(name) && (dst == nil || dst.kind == davScratch)
}
