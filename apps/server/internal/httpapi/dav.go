package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"golang.org/x/net/webdav"

	"github.com/nhatbui/pp-hub/apps/server/internal/audit"
	"github.com/nhatbui/pp-hub/apps/server/internal/auth"
)

// WebDAV access: a person mounts their drive in Finder, Explorer or rclone
// at /dav/ and works on files as files — what a coding agent running on
// their machine needs to run pandoc or a script over a document. See
// docs/architecture/webdav.md.
//
// Nothing about access is written a second time here. Every write is a call
// to the /api/v1 handlers the web client uses, answered in process by the
// agent router (agent.go) as the token's owner: the capability checks, the
// editor locks, quota, the activity feed and the audit log are theirs.
// Reads list through the same service calls the listings make. What this
// file adds is the translation — paths to nodes, WebDAV methods to API
// calls — and what a mounted drive needs that a browser does not:
//
//   - a write whose base is no longer current lands beside the file as a
//     conflicted copy instead of overwriting someone's work (dav_write.go);
//   - a safe save — write a temporary copy, rename it over the original —
//     becomes a new version of the original, which keeps its history,
//     shares and comments (dav_write.go);
//   - scratch files never become nodes (dav_names.go).
//
// /dav/ sits outside /api/v1, like /mcp/: its credential is a personal token
// of purpose "dav" in an Authorization header (Basic, as Finder and Explorer
// send it, or Bearer), never the session cookie, so the same-origin check
// does not apply. A browser that cached the Basic credential cannot be made
// to use it across sites: every method here except GET and HEAD needs a CORS
// preflight this endpoint never answers, and POST is not served.

const davPrefix = "/dav"

func init() {
	// chi answers 405 to a method it has not heard of before reaching any
	// handler.
	for _, m := range []string{"PROPFIND", "PROPPATCH", "MKCOL", "COPY", "MOVE", "LOCK", "UNLOCK"} {
		chi.RegisterMethod(m)
	}
}

func (s *Server) davEnabled() bool { return s.sysconfig != nil && s.sysconfig.WebDAVEnabled() }

// davURL is the URL a person mounts, or "" while the instance does not
// allow WebDAV.
func (s *Server) davURL() string {
	if !s.davEnabled() {
		return ""
	}
	return strings.TrimRight(s.externalURL, "/") + davPrefix + "/"
}

// davRoutes mounts the WebDAV endpoint.
func (s *Server) davRoutes(r chi.Router) {
	// Per person: a mount stats every file it shows, and rclone checks
	// many at once. Refused credentials are limited per address, which is
	// what bounds guessing.
	s.davLimit = newLimiter(6000, 1200)
	s.davAuthFail = newLimiter(60, 20)
	s.dav = newDavState()
	h := s.davAuth(http.HandlerFunc(s.serveDAV))
	r.Handle(davPrefix, h)
	r.Handle(davPrefix+"/*", h)
}

type davClientKey struct{}

// davClient is the token a WebDAV request presented, as the audit log names
// it ("dav:<id>"); a remembered read belongs to it.
func davClient(ctx context.Context) string {
	c, _ := ctx.Value(davClientKey{}).(string)
	return c
}

// davCredential is the token in the Authorization header: a Basic password
// (the user name is not consulted — the token names its owner) or a Bearer
// token, which rclone can send.
func davCredential(r *http.Request) string {
	if _, pass, ok := r.BasicAuth(); ok {
		return strings.TrimSpace(pass)
	}
	if tok, ok := bearerToken(r); ok {
		return tok
	}
	return ""
}

// davAuth authenticates a WebDAV request, on every request.
func (s *Server) davAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.davEnabled() {
			http.Error(w, "WebDAV access is not enabled on this instance", http.StatusNotFound)
			return
		}
		challenge := func(msg string) {
			if ok, retry := s.davAuthFail.allow(limiterKey(r), time.Now()); !ok {
				w.Header().Set("Retry-After", strconv.Itoa(int(retry.Seconds())))
				http.Error(w, "slow down and try again shortly", http.StatusTooManyRequests)
				return
			}
			w.Header().Set("WWW-Authenticate", `Basic realm="pp-hub", charset="UTF-8"`)
			http.Error(w, msg, http.StatusUnauthorized)
		}
		raw := davCredential(r)
		if raw == "" || !auth.IsAgentToken(raw) {
			challenge("sign in with a WebDAV token from your pp-hub settings as the password")
			return
		}
		tok, err := s.auth.AuthenticateAgentToken(r.Context(), raw, auth.TokenPurposeDAV)
		switch {
		case errors.Is(err, auth.ErrInvalidAccessToken):
			challenge("this token is not valid for WebDAV")
			return
		case err != nil:
			slog.ErrorContext(r.Context(), "dav token lookup", "err", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		u := tok.User
		if u.SuspendedAt.Valid {
			http.Error(w, "this account is suspended", http.StatusForbidden)
			return
		}
		if ok, retry := s.davLimit.allow("user:"+u.ID.String(), time.Now()); !ok {
			w.Header().Set("Retry-After", strconv.Itoa(int(retry.Seconds())))
			http.Error(w, "slow down and try again shortly", http.StatusTooManyRequests)
			return
		}
		if rl, ok := r.Context().Value(logCtxKey).(*requestLog); ok {
			rl.userID = u.ID.String()
		}
		ctx := audit.WithAgent(r.Context(), tok.ClientID)
		ctx = context.WithValue(ctx, davClientKey{}, tok.ClientID)
		ctx = context.WithValue(ctx, sessionCtxKey, &auth.Session{User: u})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// serveDAV answers one WebDAV request.
func (s *Server) serveDAV(w http.ResponseWriter, r *http.Request) {
	s.dav.sweep(time.Now())
	v := s.newDavView(r)
	v.settle()
	p := davClean(strings.TrimPrefix(r.URL.Path, davPrefix))
	switch r.Method {
	case http.MethodOptions:
		w.Header().Set("DAV", "1, 2")
		w.Header().Set("MS-Author-Via", "DAV")
		w.Header().Set("Allow", "OPTIONS, GET, HEAD, PUT, DELETE, MKCOL, COPY, MOVE, PROPFIND, PROPPATCH, LOCK, UNLOCK")
		w.WriteHeader(http.StatusOK)
	case http.MethodGet, http.MethodHead:
		v.get(w, r, p)
	case http.MethodPut:
		v.put(w, r, p)
	case http.MethodDelete:
		v.delete(w, p)
	case "MKCOL":
		v.mkcol(w, r, p)
	case "COPY", "MOVE":
		v.copyMove(w, r, p)
	case "PROPFIND":
		switch r.Header.Get("Depth") {
		case "infinity":
			// The whole drive in one answer is a denial of service on the
			// person's own listing; RFC 4918 §9.1 lets a server refuse it.
			w.Header().Set("Content-Type", "application/xml; charset=utf-8")
			w.WriteHeader(http.StatusForbidden)
			_, _ = io.WriteString(w, `<?xml version="1.0" encoding="utf-8"?><D:error xmlns:D="DAV:"><D:propfind-finite-depth/></D:error>`)
			return
		case "":
			// Clients that omit Depth want a folder and its children.
			r.Header.Set("Depth", "1")
		}
		v.handler().ServeHTTP(w, r)
	case "PROPPATCH", "LOCK", "UNLOCK":
		v.handler().ServeHTTP(w, r)
	default:
		w.Header().Set("Allow", "OPTIONS, GET, HEAD, PUT, DELETE, MKCOL, COPY, MOVE, PROPFIND, PROPPATCH, LOCK, UNLOCK")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handler is x/net/webdav over this view, for the methods it answers.
func (v *davView) handler() *webdav.Handler {
	return &webdav.Handler{
		Prefix:     davPrefix,
		FileSystem: davFS{v},
		LockSystem: davLocks{v},
		Logger: func(r *http.Request, err error) {
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				slog.InfoContext(r.Context(), "dav", "method", r.Method, "path", r.URL.Path, "err", err)
			}
		},
	}
}

// davFail answers status with a short plain-text reason.
func davFail(w http.ResponseWriter, status int, msg string) {
	if msg == "" {
		msg = webdav.StatusText(status)
	}
	http.Error(w, msg, status)
}

// davResolveFail answers a path that did not resolve.
func davResolveFail(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, errDavNotFound) {
		davFail(w, http.StatusNotFound, "")
		return
	}
	slog.ErrorContext(r.Context(), "dav resolve", "path", r.URL.Path, "err", err)
	davFail(w, http.StatusInternalServerError, "")
}

// --- calls into the API ---

// call runs an /api/v1 request in process as the caller and buffers the
// answer. body is streamed, so an upload's bytes go straight through.
func (v *davView) call(method, p string, hdr http.Header, body io.Reader) *bufferedResponse {
	rec := &bufferedResponse{header: http.Header{}}
	if body == nil {
		body = http.NoBody
	}
	req, err := innerRequest(v.ctx, v.r, callerSession(v.r), method, p, body)
	if err != nil {
		rec.status = http.StatusInternalServerError
		return rec
	}
	for k, vals := range hdr {
		req.Header[k] = vals
	}
	v.s.agentRouter.ServeHTTP(rec, req)
	if rec.status == 0 {
		rec.status = http.StatusOK
	}
	return rec
}

// callJSON is call with a JSON body.
func (v *davView) callJSON(method, p string, body any) *bufferedResponse {
	raw, err := json.Marshal(body)
	if err != nil {
		return &bufferedResponse{header: http.Header{}, status: http.StatusInternalServerError}
	}
	return v.call(method, p, http.Header{"Content-Type": {"application/json"}}, bytes.NewReader(raw))
}

// ok reports a 2xx answer.
func (b *bufferedResponse) ok() bool { return b.status >= 200 && b.status < 300 }

// detail is the reason a problem+json answer gives.
func (b *bufferedResponse) detail() string {
	var p struct {
		Title  string `json:"title"`
		Detail string `json:"detail"`
	}
	if json.Unmarshal(b.body.Bytes(), &p) != nil {
		return ""
	}
	if p.Detail != "" {
		return p.Detail
	}
	return p.Title
}

// relay answers a failed API call with the status it means to a WebDAV
// client and the API's reason.
func relay(w http.ResponseWriter, res *bufferedResponse) {
	davFail(w, davStatus(res), res.detail())
}

// --- GET, HEAD ---

// get serves an entry's bytes. A file goes through /nodes/{id}/download,
// so the read is authorised and audited as a download there; a block
// document open in the editor is flushed first, so the bytes are what its
// editors see. The version handed over is remembered as the base the
// client's next write of the file is checked against.
func (v *davView) get(w http.ResponseWriter, r *http.Request, p string) {
	e, err := v.resolve(p)
	if err != nil {
		davResolveFail(w, r, err)
		return
	}
	switch {
	case e.isDir():
		davFail(w, http.StatusMethodNotAllowed, "a folder has no content; list it with PROPFIND")
	case e.kind == davScratch:
		v.getScratch(w, r, e)
	case e.isFile():
		v.getFile(w, r, e)
	default:
		davFail(w, http.StatusNotFound, "")
	}
}

func (v *davView) getFile(w http.ResponseWriter, r *http.Request, e *davEntry) {
	if e.isMarkdown() {
		if err := v.s.collab.Flush(v.ctx, e.node.ID); err != nil {
			slog.ErrorContext(v.ctx, "dav flush", "node", e.node.ID, "err", err)
			davFail(w, http.StatusInternalServerError, "")
			return
		}
	}
	ver, err := v.s.store.GetCurrentVersion(v.ctx, e.node.ID)
	if err != nil {
		davFail(w, http.StatusNotFound, "")
		return
	}
	w.Header().Set("ETag", strconv.Quote(ver.BlobHash))
	if r.Method == http.MethodHead {
		w.Header().Set("Content-Type", ver.Mime)
		w.Header().Set("Content-Length", strconv.FormatInt(ver.Size, 10))
		w.Header().Set("Last-Modified", ver.CreatedAt.Time.UTC().Format(http.TimeFormat))
		w.Header().Set("Accept-Ranges", "bytes")
		w.WriteHeader(http.StatusOK)
		return
	}
	req, err := innerRequest(v.ctx, v.r, callerSession(v.r), http.MethodGet, "/nodes/"+uuidString(e.node.ID)+"/download", http.NoBody)
	if err != nil {
		davFail(w, http.StatusInternalServerError, "")
		return
	}
	for _, h := range []string{"Range", "If-Range", "If-Match", "If-None-Match", "If-Modified-Since", "If-Unmodified-Since"} {
		if val := r.Header.Get(h); val != "" {
			req.Header.Set(h, val)
		}
	}
	rec := &statusRecorder{ResponseWriter: w}
	v.s.agentRouter.ServeHTTP(rec, req)
	if rec.status == 0 || rec.status == http.StatusOK || rec.status == http.StatusPartialContent || rec.status == http.StatusNotModified {
		v.s.dav.rememberRead(v.client, e.node.ID, ver.BlobHash)
	}
}

func (v *davView) getScratch(w http.ResponseWriter, r *http.Request, e *davEntry) {
	if e.scratch.alias.Valid {
		v.getAlias(w, r, e)
		return
	}
	rc, size, err := v.open(e)
	if err != nil {
		davFail(w, http.StatusNotFound, "")
		return
	}
	defer rc.Close()
	if rs, ok := rc.(io.ReadSeeker); ok {
		http.ServeContent(w, r, e.name, e.mod, rs)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = io.Copy(w, rc)
	}
}

// getAlias serves a set-aside's version: authorised and audited as a
// download of the file it names.
func (v *davView) getAlias(w http.ResponseWriter, r *http.Request, e *davEntry) {
	rc, node, err := v.openAlias(e.scratch)
	if errors.Is(err, errDavNotFound) {
		davFail(w, http.StatusNotFound, "")
		return
	}
	if err != nil {
		slog.ErrorContext(v.ctx, "dav alias", "node", e.scratch.alias, "err", err)
		davFail(w, http.StatusInternalServerError, "")
		return
	}
	defer rc.Close()
	if r.Method != http.MethodHead {
		v.s.audit.RecordRead(r, audit.Read{Kind: audit.KindFileDownloaded, User: v.u, Node: node})
	}
	w.Header().Set("ETag", strconv.Quote(e.scratch.aliasHash))
	if rs, ok := rc.(io.ReadSeeker); ok {
		http.ServeContent(w, r, e.name, e.mod, rs)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.FormatInt(e.scratch.size, 10))
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = io.Copy(w, rc)
	}
}

// --- DELETE ---

// delete trashes a file or folder — WebDAV has no trash, the drive does.
// A trashed file is remembered for a minute: an editor that deletes a file
// and writes its replacement under the same name is saving it
// (dav_write.go).
func (v *davView) delete(w http.ResponseWriter, p string) {
	e, err := v.resolve(p)
	if err != nil {
		davResolveFail(w, v.r, err)
		return
	}
	switch {
	case e.kind == davScratch && e.scratch.pending:
		// The file was set aside and no save landed on its name: deleting
		// the set-aside name deletes the file.
		if res := v.call(http.MethodDelete, "/nodes/"+uuidString(e.scratch.alias), nil, nil); !res.ok() {
			relay(w, res)
			return
		}
		v.s.dav.dropScratch(e.scratch.key)
		v.s.dav.bury(v.key(e.scratch.key.dir, e.scratch.origin), e.scratch.alias, true)
		w.WriteHeader(http.StatusNoContent)
	case e.kind == davScratch:
		v.s.dav.dropScratch(e.scratch.key)
		w.WriteHeader(http.StatusNoContent)
	case e.kind == davNode:
		if v.lockedByOther(e, p) {
			davFail(w, webdav.StatusLocked, "someone else holds a lock on this")
			return
		}
		if res := v.call(http.MethodDelete, "/nodes/"+uuidString(e.node.ID), nil, nil); !res.ok() {
			relay(w, res)
			return
		}
		if e.isFile() {
			v.s.dav.bury(v.key(path.Dir(e.path), e.name), e.node.ID, true)
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		davFail(w, http.StatusForbidden, "a space, the root and a pages folder cannot be deleted here")
	}
}

// settle finishes the caller's set-asides whose save never landed. Word's
// ~WRL name is never one a person chooses, so the file simply stays where
// it was; a name~ rename was a rename after all, and is done now.
func (v *davView) settle() {
	for _, f := range v.s.dav.unsettled(v.u.ID, time.Now()) {
		if !davWordSetAside.MatchString(f.key.name) {
			v.callJSON(http.MethodPatch, "/nodes/"+uuidString(f.alias)+"/rename", map[string]string{"name": f.key.name})
		}
		v.s.dav.dropScratch(f.key)
		v.s.dav.unbury(v.key(f.key.dir, f.origin))
	}
}

// --- MKCOL ---

func (v *davView) mkcol(w http.ResponseWriter, r *http.Request, p string) {
	if r.ContentLength > 0 {
		davFail(w, http.StatusUnsupportedMediaType, "MKCOL takes no body")
		return
	}
	dir, name, err := v.resolveParent(p)
	if err != nil {
		davFail(w, http.StatusConflict, "the parent folder does not exist")
		return
	}
	if _, err := v.child(dir, name); err == nil {
		davFail(w, http.StatusMethodNotAllowed, "something already has this name")
		return
	}
	spaceID, parentID, pages, ok := dir.container()
	if !ok || pages || isDavScratch(name) {
		davFail(w, http.StatusForbidden, "a folder cannot be created here")
		return
	}
	if doc := v.pagesDocFor(dir, name); doc != nil {
		if !doc.caps.Edit {
			davFail(w, http.StatusForbidden, "insufficient permission")
			return
		}
		// The folder a document's sub-pages appear in exists as soon as
		// the document does; this only shows it before the first page.
		v.s.dav.addPagesDir(v.key(dir.path, name))
		w.WriteHeader(http.StatusCreated)
		return
	}
	if msg := v.caseClash(dir, name); msg != "" {
		davFail(w, http.StatusConflict, msg)
		return
	}
	res := v.callJSON(http.MethodPost, "/spaces/"+uuidString(spaceID)+"/dirs", map[string]string{
		"name": name, "parent_id": uuidString(parentID),
	})
	if !res.ok() {
		relay(w, res)
		return
	}
	w.WriteHeader(http.StatusCreated)
}

// pagesDocFor is the markdown document in dir whose sub-pages folder name
// would be, if there is one.
func (v *davView) pagesDocFor(dir *davEntry, name string) *davEntry {
	if !strings.HasSuffix(name, davPagesSuffix) {
		return nil
	}
	list, err := v.list(dir)
	if err != nil {
		return nil
	}
	for _, c := range list {
		if c.isMarkdown() && davPagesName(c.name) == name {
			return c
		}
	}
	return nil
}

// caseClash refuses a new name that differs only in letter case from one
// already in dir: macOS and Windows treat the two as one file, and a mount
// would show one of them and write to the other.
func (v *davView) caseClash(dir *davEntry, name string) string {
	list, err := v.list(dir)
	if err != nil {
		return ""
	}
	for _, c := range list {
		if c.name != name && strings.EqualFold(c.name, name) {
			return fmt.Sprintf("%q differs from %q only in letter case, which macOS and Windows cannot tell apart", name, c.name)
		}
	}
	return ""
}
