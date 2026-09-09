package httpapi

import (
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/jackc/pgx/v5/pgtype"

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
	// Token's node must match the URL node.
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
		"Size":                       ver.Size,
		"OwnerId":                    oid.(string),
		"UserId":                     uid.(string),
		"UserFriendlyName":           user.Name,
		"UserCanWrite":               c.PermRank >= 3,
		"Version":                    strconv.Itoa(int(ver.VersionNo)),
		"SupportsUpdate":             true,
		"SupportsLocks":              true,
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
	// Lock check: if a lock exists, X-WOPI-Lock header must match.
	if lockToken, err := s.wopi.CurrentLock(r.Context(), node.ID); err == nil && lockToken != "" {
		hdr := r.Header.Get("X-WOPI-Lock")
		if hdr != lockToken {
			w.Header().Set("X-WOPI-Lock", lockToken)
			writeProblem(w, http.StatusConflict, "Conflict", "locked by another session")
			return
		}
	}
	// Version check (optimistic concurrency): X-WOPI-ExpectedVersion if provided.
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

// handleEditSession returns the OnlyOffice editor URL + WOPI token for a node.
func (s *Server) handleEditSession(w http.ResponseWriter, r *http.Request) {
	u, node := s.requireNodeAccess(w, r, 3)
	if u == nil {
		return
	}
	if node.Kind != store.NodeKindFile {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "not a file")
		return
	}
	// Resolve effective rank for token claims.
	roleRank := 0
	if role, err := s.spaces.RoleOf(r.Context(), node.SpaceID, u.ID); err == nil {
		switch role {
		case "viewer":
			roleRank = 1
		case "editor":
			roleRank = 2
		case "owner":
			roleRank = 3
		}
	}
	eff, err := s.share.EffectivePerm(r.Context(), node, u.ID, roleRank)
	if err != nil {
		Error(w, r, err)
		return
	}
	token, err := s.wopi.SignToken(u.ID, node.ID, eff)
	if err != nil {
		Error(w, r, err)
		return
	}
	// OnlyOffice Document Server editor URL. wopiSrc must be reachable from the
	// OnlyOffice container (PP_WOPI_SERVER_URL), the browser loads editor from
	// PP_ONLYOFFICE_URL.
	nodeID, _ := node.ID.Value()
	wopiSrc := s.wopiServerURL + "/wopi/files/" + nodeID.(string)
	editorURL := s.onlyOfficeURL + "/web-apps/apps/api/documents/index.html?wopiSrc=" + wopiSrc + "&access_token=" + token
	writeJSON(w, http.StatusOK, map[string]string{
		"editor_url": editorURL,
		"wopi_src":   wopiSrc,
		"token":      token,
	})
}
