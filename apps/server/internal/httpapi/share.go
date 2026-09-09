package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nhatbui/pp-hub/apps/server/internal/share"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

type shareDTO struct {
	ID        string `json:"id"`
	NodeID    string `json:"node_id"`
	Kind      string `json:"kind"`
	Perm      string `json:"perm"`
	Grantee   string `json:"grantee_email,omitempty"`
	URL       string `json:"url,omitempty"` // link only, only at creation
	ExpiresAt string `json:"expires_at,omitempty"`
	CreatedAt string `json:"created_at"`
}

func shareToDTO(sh store.Share, externalURL, token string) shareDTO {
	id, _ := sh.ID.Value()
	nid, _ := sh.NodeID.Value()
	d := shareDTO{
		ID:        id.(string),
		NodeID:    nid.(string),
		Kind:      string(sh.Kind),
		Perm:      string(sh.Perm),
		CreatedAt: sh.CreatedAt.Time.Format(time.RFC3339),
	}
	if sh.ExpiresAt.Valid {
		d.ExpiresAt = sh.ExpiresAt.Time.Format(time.RFC3339)
	}
	if token != "" {
		d.URL = externalURL + "/s/" + token
	}
	return d
}

// handleCreateShare creates a direct or link share on a node.
// Caller must hold edit (editor role or edit share) on the node.
func (s *Server) handleCreateShare(w http.ResponseWriter, r *http.Request) {
	u, node := s.requireNodeAccess(w, r, 3) // edit rank
	if u == nil {
		return
	}
	var body struct {
		Kind      string `json:"kind"`       // "link" | "direct"
		Perm      string `json:"perm"`       // "view" | "download" | "edit"
		Email     string `json:"email"`      // direct only
		ExpiresIn int64  `json:"expires_in"` // seconds, 0 = never
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "invalid body")
		return
	}
	perm := store.SharePerm(body.Perm)
	if perm != share.PermView && perm != share.PermDownload && perm != share.PermEdit {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "perm must be view|download|edit")
		return
	}
	var expires pgtype.Timestamptz
	if body.ExpiresIn > 0 {
		expires = share.TokenExpiry(time.Duration(body.ExpiresIn) * time.Second)
	}

	switch body.Kind {
	case "link":
		sh, token, err := s.share.CreateLink(r.Context(), node.ID, perm, expires, u.ID)
		if err != nil {
			Error(w, r, err)
			return
		}
		writeJSON(w, http.StatusCreated, shareToDTO(sh, s.externalURL, token))
	case "direct":
		grantee, err := s.store.GetUserByEmail(r.Context(), body.Email)
		if err != nil {
			writeProblem(w, http.StatusNotFound, "Not Found", "no user with that email")
			return
		}
		sh, err := s.share.CreateDirect(r.Context(), node.ID, grantee.ID, perm, expires, u.ID)
		if err != nil {
			if errors.Is(err, share.ErrSelfShare) {
				writeProblem(w, http.StatusBadRequest, "Bad Request", err.Error())
				return
			}
			Error(w, r, err)
			return
		}
		dto := shareToDTO(sh, s.externalURL, "")
		dto.Grantee = grantee.Email
		writeJSON(w, http.StatusCreated, dto)
	default:
		writeProblem(w, http.StatusBadRequest, "Bad Request", "kind must be link|direct")
	}
}

func (s *Server) handleListShares(w http.ResponseWriter, r *http.Request) {
	u, node := s.requireNodeAccess(w, r, 1)
	if u == nil {
		return
	}
	shares, err := s.share.ListForNode(r.Context(), node.ID)
	if err != nil {
		Error(w, r, err)
		return
	}
	out := make([]shareDTO, 0, len(shares))
	for _, sh := range shares {
		d := shareToDTO(sh, s.externalURL, "")
		if sh.Kind == store.ShareKindDirect && sh.GranteeUserID.Valid {
			if g, err := s.store.GetUser(r.Context(), sh.GranteeUserID); err == nil {
				d.Grantee = g.Email
			}
		}
		out = append(out, d)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleRevokeShare(w http.ResponseWriter, r *http.Request) {
	u, node := s.requireNodeAccess(w, r, 3)
	if u == nil {
		return
	}
	var shareID pgtype.UUID
	if err := shareID.Scan(chi.URLParam(r, "shareID")); err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "invalid share id")
		return
	}
	_ = node // ownership verified via node access above
	if err := s.share.Revoke(r.Context(), shareID); err != nil {
		Error(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- anonymous link access ---

// resolveLinkNode validates the token and returns node + share, or writes error.
func (s *Server) resolveLinkNode(w http.ResponseWriter, r *http.Request) (store.Share, store.Node) {
	token := chi.URLParam(r, "token")
	sh, err := s.share.ResolveLink(r.Context(), token)
	if err != nil {
		writeProblem(w, http.StatusNotFound, "Not Found", "invalid or expired share link")
		return store.Share{}, store.Node{}
	}
	node, err := s.fs.Get(r.Context(), sh.NodeID)
	if err != nil {
		writeProblem(w, http.StatusNotFound, "Not Found", "file no longer exists")
		return store.Share{}, store.Node{}
	}
	return sh, node
}

// handleShareInfo returns node metadata for the share landing page.
func (s *Server) handleShareInfo(w http.ResponseWriter, r *http.Request) {
	sh, node := s.resolveLinkNode(w, r)
	if !sh.ID.Valid {
		return
	}
	dto := nodeToDTO(node, nil)
	if node.Kind == store.NodeKindFile {
		if ver, err := s.store.GetCurrentVersion(r.Context(), node.ID); err == nil {
			dto = nodeToDTO(node, &ver)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"node": dto,
		"perm": sh.Perm,
	})
}

// handleShareDownload streams the shared file without authentication.
func (s *Server) handleShareDownload(w http.ResponseWriter, r *http.Request) {
	sh, node := s.resolveLinkNode(w, r)
	if !sh.ID.Valid {
		return
	}
	rank, _ := share.LinkPerm(sh)
	if rank < 2 { // need at least download
		writeProblem(w, http.StatusForbidden, "Forbidden", "link does not allow download")
		return
	}
	n, ver, rc, err := s.fs.OpenFile(r.Context(), node.ID)
	if err != nil {
		s.nodeError(w, r, err)
		return
	}
	defer rc.Close()
	w.Header().Set("Content-Type", ver.Mime)
	w.Header().Set("Content-Disposition", "attachment; filename=\""+strconv.Quote(n.Name)+"\"")
	w.Header().Set("Content-Length", strconv.FormatInt(ver.Size, 10))
	_, _ = io.Copy(w, rc)
}
