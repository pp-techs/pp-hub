package httpapi

import (
	"errors"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nhatbui/pp-hub/apps/server/internal/share"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
	"github.com/nhatbui/pp-hub/apps/server/internal/wopi"
)

// wopiClaims resolves the access_token from query and validates it.
func (s *Server) wopiClaims(w http.ResponseWriter, r *http.Request) (wopi.TokenClaims, bool) {
	token := r.URL.Query().Get("access_token")
	if token == "" {
		writeProblem(w, http.StatusUnauthorized, "Unauthorized", "missing access_token")
		return wopi.TokenClaims{}, false
	}
	c, err := s.wopi.VerifyToken(token)
	if err != nil {
		writeProblem(w, http.StatusUnauthorized, "Unauthorized", "invalid token")
		return wopi.TokenClaims{}, false
	}
	urlNode, err := uuidParam(r, "nodeID")
	if err != nil || urlNode != c.NodeID {
		writeProblem(w, http.StatusForbidden, "Forbidden", "token does not match file")
		return wopi.TokenClaims{}, false
	}
	return c, true
}

// CheckFileInfo: GET /wopi/files/{nodeID}
func (s *Server) handleWopiCheckFileInfo(w http.ResponseWriter, r *http.Request) {
	c, ok := s.wopiClaims(w, r)
	if !ok {
		return
	}
	node, err := s.fs.Get(r.Context(), c.NodeID)
	if err != nil {
		s.nodeError(w, r, err)
		return
	}
	ver, err := s.store.GetCurrentVersion(r.Context(), node.ID)
	if err != nil {
		s.nodeError(w, r, err)
		return
	}
	user, err := s.store.GetUser(r.Context(), c.UserID)
	if err != nil {
		Error(w, r, err)
		return
	}
	uid, _ := user.ID.Value()
	oid, _ := node.CreatedBy.Value()

	writeJSON(w, http.StatusOK, map[string]any{
		"BaseFileName":               node.Name,
		"Size":                      ver.Size,
		"OwnerId":                   oid.(string),
		"UserId":                    uid.(string),
		"UserFriendlyName":           user.Name,
		"UserCanWrite":               c.PermRank >= 3, // token holds effective rank at mint time
		"Version":                   strconv.Itoa(int(ver.VersionNo)),
		"SupportsUpdate":            true,
		"SupportsLocks":             true,
		"SupportsExtendedLockLength": true,
	})
}

// GetFile: GET /wopi/files/{nodeID}/contents
func (s *Server) handleWopiGetFile(w http.ResponseWriter, r *http.Request) {
	c, ok := s.wopiClaims(w, r)
	if !ok {
		return
	}
	_, ver, rc, err := s.fs.OpenFile(r.Context(), c.NodeID)
	if err != nil {
		s.nodeError(w, r, err)
		return
	}
	defer rc.Close()
	w.Header().Set("Content-Type", ver.Mime)
	w.Header().Set("Content-Length", strconv.FormatInt(ver.Size, 10))
	_, _ = io.Copy(w, rc)
}

// PutFile: POST /wopi/files/{nodeID}/contents
func (s *Server) handleWopiPutFile(w http.ResponseWriter, r *http.Request) {
	c, ok := s.wopiClaims(w, r)
	if !ok {
		return
	}
	if c.PermRank < 3 {
		writeProblem(w, http.StatusForbidden, "Forbidden", "no write permission")
		return
	}
	node, err := s.fs.Get(r.Context(), c.NodeID)
	if err != nil {
		s.nodeError(w, r, err)
		return
	}
	if lockToken, err := s.wopi.CurrentLock(r.Context(), node.ID); err == nil && lockToken != "" {
		hdr := r.Header.Get("X-WOPI-Lock")
		if hdr != lockToken {
			w.Header().Set("X-WOPI-Lock", lockToken)
			writeProblem(w, http.StatusConflict, "Conflict", "locked by another session")
			return
		}
	}
	if expected := r.Header.Get("X-WOPI-ExpectedVersion"); expected != "" {
		cur, _ := s.store.GetCurrentVersion(r.Context(), node.ID)
		if strconv.Itoa(int(cur.VersionNo)) != expected {
			writeProblem(w, http.StatusConflict, "Conflict", "version mismatch")
			return
		}
	}
	_, ver, err := s.fs.PutFile(r.Context(), node.SpaceID, pgtype.UUID{}, node.ID, node.Name, r.Body, c.UserID)
	if err != nil {
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
	if c.PermRank < 3 {
		writeProblem(w, http.StatusForbidden, "Forbidden", "no write permission")
		return
	}
	nodeID := c.NodeID
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

func (s *Server) handleEditSession(w http.ResponseWriter, r *http.Request) {
	u, node, caps := s.requireNodeCaps(w, r, share.CapEdit)
	if u == nil {
		return
	}
	if node.Kind != store.NodeKindFile {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "not a file")
		return
	}
	ver, err := s.store.GetCurrentVersion(r.Context(), node.ID)
	if err != nil {
		Error(w, r, err)
		return
	}
	if !isOfficeMime(ver.Mime) {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "file type not supported by editor")
		return
	}
	// M15: effective caps computed by requireNodeCaps above. Token encodes a
	// permission rank for WOPI compatibility (w1 format): rank 3 = edit, 1 = view.
	permRank := 1
	if caps.Has(share.CapEdit) {
		permRank = 3
	}
	token, err := s.wopi.SignToken(u.ID, node.ID, permRank)
	if err != nil {
		Error(w, r, err)
		return
	}
	nodeIDStr, _ := node.ID.Value()
	wopiSrc := s.wopiServerURL + "/wopi/files/" + nodeIDStr.(string)
	editorURL := s.onlyOfficeURL + "/hosting/wopi/" + wopiDocumentType(node.Name) + "/edit?WOPISrc=" + url.QueryEscape(wopiSrc)
	writeJSON(w, http.StatusOK, map[string]any{
		"editor_url":        editorURL,
		"wopi_src":          wopiSrc,
		"token":             token,
		"access_token_ttl":  time.Now().Add(wopi.TokenTTL).UnixMilli(),
	})
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
		"application/octet-stream":
		return true
	default:
		return false
	}
}

// wopiDocumentType maps a file name to the OnlyOffice WOPI document type
// (word/cell/slide).
func wopiDocumentType(name string) string {
	switch strings.ToLower(path.Ext(name)) {
	case ".xlsx", ".xls", ".xlsm", ".xlt", ".xltx", ".xltm", ".ods", ".fods", ".ots", ".csv":
		return "cell"
	case ".pptx", ".ppt", ".odp", ".fodp", ".otp", ".ppsx", ".pps", ".ppsm", ".pot", ".potx", ".potm":
		return "slide"
	default:
		return "word"
	}
}
