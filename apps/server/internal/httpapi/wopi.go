package httpapi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5"

	"github.com/nhatbui/pp-hub/apps/server/internal/audit"
	"github.com/nhatbui/pp-hub/apps/server/internal/share"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
	"github.com/nhatbui/pp-hub/apps/server/internal/users"
	"github.com/nhatbui/pp-hub/apps/server/internal/wopi"
)

// wopiCaller is a validated WOPI request: the node, the person it acts for,
// and the token's permission rank **re-resolved against present-day access**.
//
// A WOPI access token is a bearer credential with a ten-hour life that
// OnlyOffice holds and replays, and the rank baked into it was the caller's
// rank at mint time. Trusting that rank alone meant a grant could be revoked,
// a member removed from the space, or an account suspended, and the editor
// would keep saving for the rest of the day — while every /api/v1 route
// refused the same person immediately. So the rank in the token is now a
// ceiling, not an authority: it is clamped to what the caller can do right
// now, and a caller who can no longer reach the file at all is refused.
type wopiCaller struct {
	claims wopi.TokenClaims
	node   store.Node
	// user is nil for a machine token — currently only the preview worker's
	// conversion fetch, which acts for nobody and reads bytes.
	user *store.User
}

// wopiClaims validates the access_token, loads the file, and re-resolves what
// the token's subject may do with it.
func (s *Server) wopiClaims(w http.ResponseWriter, r *http.Request) (wopiCaller, bool) {
	token := r.URL.Query().Get("access_token")
	if token == "" {
		writeProblem(w, http.StatusUnauthorized, "Unauthorized", "missing access_token")
		return wopiCaller{}, false
	}
	c, err := s.wopi.VerifyToken(token)
	if err != nil {
		writeProblem(w, http.StatusUnauthorized, "Unauthorized", "invalid token")
		return wopiCaller{}, false
	}
	urlNode, err := uuidParam(r, "nodeID")
	if err != nil || urlNode != c.NodeID {
		writeProblem(w, http.StatusForbidden, "Forbidden", "token does not match file")
		return wopiCaller{}, false
	}
	node, err := s.fs.Get(r.Context(), c.NodeID)
	if err != nil {
		s.nodeError(w, r, err)
		return wopiCaller{}, false
	}

	// A machine token carries no subject to resolve access for. It is minted
	// by this deployment, scoped to one node, read-only and short-lived, and
	// the key that signs it is the authorization.
	if c.UserID.String() == wopi.SystemSubject {
		return wopiCaller{claims: c, node: node}, true
	}

	user, err := s.store.GetUser(r.Context(), c.UserID)
	if err != nil {
		s.nodeError(w, r, err)
		return wopiCaller{}, false
	}
	if user.SuspendedAt.Valid {
		writeProblem(w, http.StatusForbidden, "Forbidden", "this account is suspended")
		return wopiCaller{}, false
	}
	caps, err := s.share.EffectiveCapsForNode(r.Context(), node, user.ID)
	if err != nil {
		Error(w, r, err)
		return wopiCaller{}, false
	}
	if rank := rankFromCaps(caps); rank < c.PermRank {
		c.PermRank = rank
	}
	if c.PermRank < 1 {
		writeProblem(w, http.StatusForbidden, "Forbidden", "no longer have access to this file")
		return wopiCaller{}, false
	}
	return wopiCaller{claims: c, node: node, user: &user}, true
}

// rankFromCaps collapses resolved capabilities onto the WOPI rank scale the
// w1 token format carries: 3 = edit, 2 = download, 1 = view, 0 = no access.
func rankFromCaps(c share.Caps) int {
	switch {
	case c.Has(share.CapEdit):
		return 3
	case c.Has(share.CapDownload):
		return 2
	case c.Has(share.CapView):
		return 1
	}
	return 0
}

// CheckFileInfo: GET /wopi/files/{nodeID}
func (s *Server) handleWopiCheckFileInfo(w http.ResponseWriter, r *http.Request) {
	c, ok := s.wopiClaims(w, r)
	if !ok {
		return
	}
	// Only an editor session opens a file, and only a person has one. A
	// machine token reaching here is a caller confusing the read endpoint
	// with the host endpoint, not an account to answer for.
	if c.user == nil {
		writeProblem(w, http.StatusForbidden, "Forbidden", "not an editor session")
		return
	}
	ver, err := s.store.GetCurrentVersion(r.Context(), c.node.ID)
	if err != nil {
		s.nodeError(w, r, err)
		return
	}
	uid, _ := c.user.ID.Value()
	oid, _ := c.node.CreatedBy.Value()

	writeJSON(w, http.StatusOK, map[string]any{
		"BaseFileName":     c.node.Name,
		"Size":             ver.Size,
		"OwnerId":          oid.(string),
		"UserId":           uid.(string),
		"UserFriendlyName": c.user.Name,
		// Present-day access, not the rank the token was minted with. A
		// reader whose edit grant was revoked mid-session is told the truth
		// on the next CheckFileInfo and the editor drops to read-only,
		// rather than letting them type into a save that will 403.
		"UserCanWrite":               c.claims.PermRank >= 3 && !c.node.DeletedAt.Valid,
		"Version":                    strconv.Itoa(int(ver.VersionNo)),
		"SupportsUpdate":             true,
		"SupportsLocks":              true,
		"SupportsExtendedLockLength": true,
		"DisablePrint":               c.claims.PermRank < 2,
		"HidePrintOption":            c.claims.PermRank < 2,
		"DisableExport":              c.claims.PermRank < 2,
		"DisableCopy":                c.claims.PermRank < 2,
	})
}

// GetFile: GET /wopi/files/{nodeID}/contents
func (s *Server) handleWopiGetFile(w http.ResponseWriter, r *http.Request) {
	c, ok := s.wopiClaims(w, r)
	if !ok {
		return
	}
	// OnlyOffice needs the file's bytes to render it on screen. Viewing is
	// rank 1 or above; a caller with no access at all is refused here.
	if c.claims.PermRank < 1 {
		writeProblem(w, http.StatusForbidden, "Forbidden", "view permission required")
		return
	}
	_, ver, rc, err := s.fs.OpenFile(r.Context(), c.node.ID)
	if err != nil {
		s.nodeError(w, r, err)
		return
	}
	defer rc.Close()
	w.Header().Set("Content-Type", ver.Mime)
	// The MIME is derived from the uploader's chosen extension, and unlike
	// every other byte-serving path this one used to send no disposition
	// either — leaving a sniffing client nothing but that MIME to go on.
	w.Header().Set("Content-Disposition", contentDisposition("attachment", c.node.Name))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Length", strconv.FormatInt(ver.Size, 10))
	_, _ = io.Copy(w, rc)
}

// PutFile: POST /wopi/files/{nodeID}/contents
func (s *Server) handleWopiPutFile(w http.ResponseWriter, r *http.Request) {
	c, ok := s.wopiClaims(w, r)
	if !ok {
		return
	}
	if c.claims.PermRank < 3 {
		writeProblem(w, http.StatusForbidden, "Forbidden", "no write permission")
		return
	}
	node := c.node
	// Committing a new version into a tombstone would resurrect content
	// nobody expects on the next restore. Reads still work, so an editor open
	// when the file was trashed can still show what it had.
	if node.DeletedAt.Valid {
		writeProblem(w, http.StatusConflict, "Conflict", "this file is in the trash")
		return
	}
	hdr := r.Header.Get("X-WOPI-Lock")
	if lockToken, err := s.wopi.CurrentLock(r.Context(), node.ID); err == nil && lockToken != "" {
		if hdr != lockToken {
			w.Header().Set("X-WOPI-Lock", lockToken)
			writeProblem(w, http.StatusConflict, "Conflict", "locked by another session")
			return
		}
	} else if hdr != "" {
		w.Header().Set("X-WOPI-Lock", "")
		writeProblem(w, http.StatusConflict, "Conflict", "file is not locked")
		return
	}
	if expected := r.Header.Get("X-WOPI-ExpectedVersion"); expected != "" {
		cur, _ := s.store.GetCurrentVersion(r.Context(), node.ID)
		if strconv.Itoa(int(cur.VersionNo)) != expected {
			writeProblem(w, http.StatusConflict, "Conflict", "version mismatch")
			return
		}
	}
	if !limitBody(w, r, s.uploadCap()) {
		return
	}
	commitCheck := func(ctx context.Context, q *store.Queries, n store.Node) error {
		l, err := q.GetLock(ctx, n.ID)
		if hdr != "" {
			if errors.Is(err, pgx.ErrNoRows) || l.LockToken != hdr {
				return wopi.ErrLockConflict
			}
		} else if err == nil && l.LockToken != "" {
			return wopi.ErrLockConflict
		}
		return nil
	}
	_, ver, err := s.fs.PutFileWithCheck(r.Context(), node.SpaceID, pgtype.UUID{}, node.ID, node.Name, r.Body, c.claims.UserID, commitCheck)
	if err != nil {
		if errors.Is(err, wopi.ErrLockConflict) {
			cur, _ := s.wopi.CurrentLock(r.Context(), node.ID)
			w.Header().Set("X-WOPI-Lock", cur)
			writeProblem(w, http.StatusConflict, "Conflict", "lock conflict")
			return
		}
		s.nodeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"Version": strconv.Itoa(int(ver.VersionNo)),
	})
}

// Lock ops: POST /wopi/files/{nodeID} with X-WOPI-Override
func (s *Server) handleWopiLockOps(w http.ResponseWriter, r *http.Request) {
	c, ok := s.wopiClaims(w, r)
	if !ok {
		return
	}
	if c.claims.PermRank < 3 {
		writeProblem(w, http.StatusForbidden, "Forbidden", "no write permission")
		return
	}
	nodeID := c.node.ID
	override := r.Header.Get("X-WOPI-Override")
	lockToken := r.Header.Get("X-WOPI-Lock")

	switch override {
	case "LOCK":
		if err := s.wopi.Lock(r.Context(), nodeID, lockToken); err != nil {
			s.wopiLockError(w, r, err, nodeID)
			return
		}
		w.WriteHeader(http.StatusOK)
	case "UNLOCK":
		if err := s.wopi.Unlock(r.Context(), nodeID, lockToken); err != nil {
			s.wopiLockError(w, r, err, nodeID)
			return
		}
		w.WriteHeader(http.StatusOK)
	case "REFRESH_LOCK":
		if err := s.wopi.Lock(r.Context(), nodeID, lockToken); err != nil {
			s.wopiLockError(w, r, err, nodeID)
			return
		}
		w.WriteHeader(http.StatusOK)
	case "GET_LOCK":
		token, _ := s.wopi.CurrentLock(r.Context(), nodeID)
		w.Header().Set("X-WOPI-Lock", token)
		w.WriteHeader(http.StatusOK)
	default:
		writeProblem(w, http.StatusBadRequest, "Bad Request", "unsupported X-WOPI-Override: "+override)
	}
}

func (s *Server) wopiLockError(w http.ResponseWriter, r *http.Request, err error, nodeID pgtype.UUID) {
	switch {
	case errors.Is(err, wopi.ErrLockConflict):
		cur, _ := s.wopi.CurrentLock(r.Context(), nodeID)
		w.Header().Set("X-WOPI-Lock", cur)
		writeProblem(w, http.StatusConflict, "Conflict", "lock conflict")
	case errors.Is(err, wopi.ErrNotLocked):
		writeProblem(w, http.StatusNotFound, "Not Found", "not locked")
	default:
		Error(w, r, err)
	}
}

// --- Editor session (authenticated, called by the SPA) ---

// handleEditSession mints a WOPI session for the SPA's office viewer.
//
// It requires **download**, not view. Opening a document in OnlyOffice means
// the Document Server fetches the whole file and the browser renders it, so
// there is no version of this that is "viewing without downloading" — the
// bytes leave the server either way. It used to accept CapView on the
// reasoning that rank 1 produced a read-only session, which was true about
// writing and irrelevant to reading: the session was a route around the
// no-download half of a grant.
//
// Viewing is rank 1 or above (mode="view"). Downloading adds rank 2, and editing
// adds rank 3 (mode="edit") which enables PutFile and file locking.
func (s *Server) handleEditSession(w http.ResponseWriter, r *http.Request) {
	u, node, caps := s.requireNodeCaps(w, r, share.CapView)
	if u == nil {
		return
	}
	if node.Kind != store.NodeKindFile {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "not a file")
		return
	}
	ver, err := s.store.GetCurrentVersion(r.Context(), node.ID)
	if err != nil {
		s.nodeError(w, r, err)
		return
	}
	if !isOfficeNode(node, ver.Mime) {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "file type not supported by editor")
		return
	}
	permRank, mode := 1, "view"
	if caps.Has(share.CapDownload) {
		permRank = 2
	}
	if caps.Has(share.CapEdit) {
		permRank, mode = 3, "edit"
	}
	token, err := s.wopi.SignToken(u.ID, node.ID, permRank)
	if err != nil {
		Error(w, r, err)
		return
	}
	nodeIDStr, _ := node.ID.Value()
	wopiSrc := s.wopiServerURL + "/wopi/files/" + nodeIDStr.(string)
	editorURL := s.onlyOfficeURL + "/hosting/wopi/" + wopiDocumentType(node.Name) + "/" + mode +
		"?WOPISrc=" + url.QueryEscape(wopiSrc)
	// `ui` is the WOPI discovery placeholder UI_LLCC resolved to a real value;
	// the Document Server reads it straight into editorConfig.lang. The caller
	// sends its active locale rather than the server reading users.locale,
	// because that column is empty for anyone who never chose one and the
	// browser's own detection is then the only truthful answer. Omitted when
	// absent or malformed, which leaves OnlyOffice on its en-US default.
	if ui := strings.TrimSpace(r.URL.Query().Get("ui")); ui != "" && users.ValidLocale(ui) {
		editorURL += "&ui=" + url.QueryEscape(ui)
	}
	// `thm` is the discovery placeholder THEME_ID. Same contract as `ui`: the
	// caller sends what it is displaying, because the theme lives in that
	// browser's localStorage — it is a per-browser choice, not an account
	// column — and "system" only resolves there.
	//
	// The values are numeric and read backwards from the name, which is why
	// they are written here rather than passed through: verified against
	// Document Server 9.4.0, thm=1 renders the light UI and thm=0 the dark
	// one. A word like "theme-dark" is accepted by the URL and silently
	// ignored, so anything but these two is left off entirely and the editor
	// keeps its own default.
	switch r.URL.Query().Get("theme") {
	case "light":
		editorURL += "&thm=1"
	case "dark":
		editorURL += "&thm=0"
	}
	s.audit.RecordRead(r, audit.Read{Kind: audit.KindFileOpened, User: u, Node: node, Detail: map[string]any{"mode": mode}})
	writeJSON(w, http.StatusOK, map[string]any{
		"editor_url":       editorURL,
		"wopi_src":         wopiSrc,
		"token":            token,
		"access_token_ttl": time.Now().Add(wopi.TokenTTL).UnixMilli(),
		"can_write":        permRank >= 3,
	})
}

// isOfficeNode reports whether a file can be opened by OnlyOffice, checking
// both the stored MIME type and standard office file extensions. PDFs are
// among them: Document Server's PDF editor views, annotates and fills them
// (`/hosting/wopi/pdf/{view,edit}` in its discovery).
func isOfficeNode(node store.Node, mime string) bool {
	if isOfficeMime(mime) {
		return true
	}
	ext := strings.ToLower(path.Ext(node.Name))
	switch ext {
	case ".docx", ".doc", ".odt", ".rtf", ".epub",
		".xlsx", ".xls", ".ods", ".csv",
		".pptx", ".ppt", ".odp",
		".pdf":
		return true
	}
	return false
}

// isOfficeMime returns true for MIME types OnlyOffice Document Server can handle.
func isOfficeMime(mime string) bool {
	switch strings.TrimSpace(strings.SplitN(mime, ";", 2)[0]) {
	case
		"application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		"application/msword",
		"application/vnd.oasis.opendocument.text",
		"application/rtf",
		"application/epub+zip",
		"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
		"application/vnd.ms-excel",
		"application/vnd.oasis.opendocument.spreadsheet",
		"text/csv",
		"application/vnd.openxmlformats-officedocument.presentationml.presentation",
		"application/vnd.ms-powerpoint",
		"application/vnd.oasis.opendocument.presentation",
		"application/pdf":
		// Deliberately not "application/octet-stream". That is what
		// fs.PutFile stores whenever Go's MIME table does not recognise the
		// extension, so allowing it turned this allowlist into "any file
		// with an unfamiliar name" — which is the opposite of an allowlist.
		return true
	default:
		return false
	}
}

// wopiDocumentType maps a file name to the OnlyOffice WOPI document type
// (word/cell/slide/pdf).
func wopiDocumentType(name string) string {
	switch strings.ToLower(path.Ext(name)) {
	case ".xlsx", ".xls", ".xlsm", ".xlt", ".xltx", ".xltm", ".ods", ".fods", ".ots", ".csv":
		return "cell"
	case ".pptx", ".ppt", ".odp", ".fodp", ".otp", ".ppsx", ".pps", ".ppsm", ".pot", ".potx", ".potm":
		return "slide"
	case ".pdf":
		return "pdf"
	default:
		return "word"
	}
}
