package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/nhatbui/pp-hub/apps/server/internal/audit"
	"github.com/nhatbui/pp-hub/apps/server/internal/fs"
	"github.com/nhatbui/pp-hub/apps/server/internal/publish"
	"github.com/nhatbui/pp-hub/apps/server/internal/share"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

// Publishing (M50, docs/architecture/publishing.md): the management routes
// under /api/v1/nodes/{id}/publication and the anonymous viewer API under
// /p/{token}. The bytes themselves are served by the artifact host
// (artifact.go), never on this origin.

type publicationDTO struct {
	ID          string  `json:"id"`
	NodeID      string  `json:"node_id"`
	URL         string  `json:"url"`
	Title       string  `json:"title"`
	EntryPath   string  `json:"entry_path"`
	Rev         int32   `json:"rev"`
	PublishedAt string  `json:"published_at"`
	ExpiresAt   *string `json:"expires_at"`
	HasPassword bool    `json:"has_password"`
	FileCount   int64   `json:"file_count"`
	TotalBytes  int64   `json:"total_bytes"`
	Stale       bool    `json:"stale"`
	PublishedBy struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"published_by"`
}

func (s *Server) publicationToDTO(r *http.Request, pub store.Publication, node store.Node) (publicationDTO, error) {
	token, err := s.publish.Token(pub)
	if err != nil {
		return publicationDTO{}, err
	}
	sum, err := s.publish.Summarize(r.Context(), pub, node)
	if err != nil {
		return publicationDTO{}, err
	}
	d := publicationDTO{
		ID:          uuidString(pub.ID),
		NodeID:      uuidString(pub.NodeID),
		URL:         strings.TrimRight(s.externalURL, "/") + "/p/" + token,
		Title:       pub.Title,
		EntryPath:   pub.EntryPath,
		Rev:         pub.Rev,
		PublishedAt: pub.PublishedAt.Time.UTC().Format(time.RFC3339),
		HasPassword: pub.PasswordHash.Valid,
		FileCount:   sum.FileCount,
		TotalBytes:  sum.TotalBytes,
		Stale:       sum.Stale,
	}
	if pub.ExpiresAt.Valid {
		e := pub.ExpiresAt.Time.UTC().Format(time.RFC3339)
		d.ExpiresAt = &e
	}
	d.PublishedBy.ID = uuidString(pub.CreatedBy)
	if u, err := s.store.GetUser(r.Context(), pub.CreatedBy); err == nil {
		d.PublishedBy.Name = displayName(u)
	}
	return d, nil
}

// requirePublishRights is what creating a public link on node needs
// (handleCreateShare: share on the node, download because a publication
// hands the bytes out, the space's allow_public_links) plus the instance
// offering publishing. It answers the refusal itself.
func (s *Server) requirePublishRights(w http.ResponseWriter, r *http.Request, node store.Node, caps share.Caps) bool {
	if !s.publish.Enabled() {
		writeProblem(w, http.StatusForbidden, "Forbidden", "publishing is not available on this instance")
		return false
	}
	if !caps.Has(share.CapShare) {
		writeProblem(w, http.StatusForbidden, "Forbidden", "insufficient permission")
		return false
	}
	if !caps.Has(share.CapDownload) {
		writeProblem(w, http.StatusForbidden, "Forbidden", share.ErrEscalation.Error())
		return false
	}
	sp, err := s.store.GetSpace(r.Context(), node.SpaceID)
	if err != nil {
		Error(w, r, err)
		return false
	}
	if !sp.AllowPublicLinks {
		writeProblem(w, http.StatusForbidden, "Forbidden", "public links are turned off for this space")
		return false
	}
	return true
}

// existingPublication loads the node's publication for reading, changing
// or ending it: allowed to whoever could publish the node, and always to
// the publication's creator — even with the source in the trash or the
// switches turned off, so a publisher can always take their page down.
func (s *Server) existingPublication(w http.ResponseWriter, r *http.Request) (*store.User, store.Node, store.Publication, bool) {
	u := caller(r)
	nodeID, err := uuidParam(r, "nodeID")
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "invalid node id")
		return nil, store.Node{}, store.Publication{}, false
	}
	if s.publish == nil {
		writeProblem(w, http.StatusNotFound, "Not Found", "this file or folder is not published")
		return nil, store.Node{}, store.Publication{}, false
	}
	pub, err := s.publish.ForNode(r.Context(), nodeID)
	if err != nil && !errors.Is(err, publish.ErrNotFound) {
		Error(w, r, err)
		return nil, store.Node{}, store.Publication{}, false
	}
	found := err == nil
	if found && pub.CreatedBy == u.ID {
		node, err := s.fs.GetAny(r.Context(), nodeID)
		if err != nil {
			s.nodeError(w, r, err)
			return nil, store.Node{}, store.Publication{}, false
		}
		return u, node, pub, true
	}
	_, node, caps := s.requireNodeCaps(w, r, share.CapView)
	if !node.ID.Valid {
		return nil, store.Node{}, store.Publication{}, false
	}
	if !s.requirePublishRights(w, r, node, caps) {
		return nil, store.Node{}, store.Publication{}, false
	}
	if !found {
		writeProblem(w, http.StatusNotFound, "Not Found", "this file or folder is not published")
		return nil, store.Node{}, store.Publication{}, false
	}
	return u, node, pub, true
}

// publicationBody is the POST/PATCH body. password and expires_at tell
// absent (keep) from null (clear), so they are read raw.
type publicationBody struct {
	EntryPath string
	Patch     publish.Patch
}

func parsePublicationBody(r *http.Request) (publicationBody, error) {
	var raw map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		return publicationBody{}, errors.New("invalid body")
	}
	var b publicationBody
	if v, ok := raw["entry_path"]; ok && string(v) != "null" {
		if err := json.Unmarshal(v, &b.EntryPath); err != nil {
			return publicationBody{}, errors.New("entry_path must be a string")
		}
	}
	if v, ok := raw["password"]; ok {
		o := &publish.Optional[string]{}
		if string(v) != "null" {
			var pw string
			if err := json.Unmarshal(v, &pw); err != nil {
				return publicationBody{}, errors.New("password must be a string or null")
			}
			o.Value = &pw
		}
		b.Patch.Password = o
	}
	if v, ok := raw["expires_at"]; ok {
		o := &publish.Optional[time.Time]{}
		if string(v) != "null" {
			var str string
			if err := json.Unmarshal(v, &str); err != nil {
				return publicationBody{}, errors.New("expires_at must be an RFC 3339 time or null")
			}
			t, err := time.Parse(time.RFC3339, str)
			if err != nil {
				return publicationBody{}, errors.New("expires_at must be an RFC 3339 time or null")
			}
			if !t.After(time.Now()) {
				return publicationBody{}, errors.New("expires_at must be in the future")
			}
			o.Value = &t
		}
		b.Patch.ExpiresAt = o
	}
	return b, nil
}

// publishError maps the model's refusals.
func (s *Server) publishError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, publish.ErrNotFound):
		writeProblem(w, http.StatusNotFound, "Not Found", "this file or folder is not published")
	case errors.Is(err, publish.ErrNotPublishable), errors.Is(err, publish.ErrNoEntry),
		errors.Is(err, publish.ErrBadEntry), errors.Is(err, publish.ErrBadPassword):
		writeProblem(w, http.StatusUnprocessableEntity, "Unprocessable Entity", err.Error())
	case errors.Is(err, publish.ErrTooManyFiles), errors.Is(err, publish.ErrTooLarge), errors.Is(err, publish.ErrQuota):
		writeProblem(w, http.StatusRequestEntityTooLarge, "Payload Too Large", err.Error())
	case errors.Is(err, fs.ErrNotFound):
		s.nodeError(w, r, err)
	default:
		Error(w, r, err)
	}
}

func (s *Server) handleGetPublication(w http.ResponseWriter, r *http.Request) {
	_, node, pub, ok := s.existingPublication(w, r)
	if !ok {
		return
	}
	dto, err := s.publicationToDTO(r, pub, node)
	if err != nil {
		Error(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, dto)
}

// handlePublish publishes the node, or republishes it: a new snapshot, the
// same URL, rev bumped so every unlock of the old one ends.
func (s *Server) handlePublish(w http.ResponseWriter, r *http.Request) {
	u, node, caps := s.requireNodeCaps(w, r, share.CapView)
	if u == nil {
		return
	}
	if !s.requirePublishRights(w, r, node, caps) {
		return
	}
	body, err := parsePublicationBody(r)
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request", err.Error())
		return
	}
	pub, created, err := s.publish.Publish(r.Context(), node, u.ID, body.EntryPath, body.Patch)
	if err != nil {
		s.publishError(w, r, err)
		return
	}
	kind, status := audit.KindPublicationUpdated, http.StatusOK
	if created {
		kind, status = audit.KindPublicationCreated, http.StatusCreated
	}
	s.audit.Record(r, audit.Event{
		Kind: kind, UserID: u.ID, Subject: u.Email, NodeID: node.ID,
		Detail: map[string]any{
			"node": node.ID.String(), "name": node.Name, "publication": pub.ID.String(),
			"rev": pub.Rev, "has_password": pub.PasswordHash.Valid, "expires": pub.ExpiresAt.Valid,
		},
	})
	dto, err := s.publicationToDTO(r, pub, node)
	if err != nil {
		Error(w, r, err)
		return
	}
	writeJSON(w, status, dto)
}

// handleUpdatePublication changes password and expiry; no snapshot.
func (s *Server) handleUpdatePublication(w http.ResponseWriter, r *http.Request) {
	u, node, pub, ok := s.existingPublication(w, r)
	if !ok {
		return
	}
	body, err := parsePublicationBody(r)
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request", err.Error())
		return
	}
	pub, err = s.publish.UpdateSettings(r.Context(), pub, body.Patch)
	if err != nil {
		s.publishError(w, r, err)
		return
	}
	s.audit.Record(r, audit.Event{
		Kind: audit.KindPublicationUpdated, UserID: u.ID, Subject: u.Email, NodeID: node.ID,
		Detail: map[string]any{
			"node": node.ID.String(), "name": node.Name, "publication": pub.ID.String(),
			"has_password": pub.PasswordHash.Valid, "expires": pub.ExpiresAt.Valid,
		},
	})
	dto, err := s.publicationToDTO(r, pub, node)
	if err != nil {
		Error(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, dto)
}

func (s *Server) handleUnpublish(w http.ResponseWriter, r *http.Request) {
	u, node, pub, ok := s.existingPublication(w, r)
	if !ok {
		return
	}
	if err := s.publish.Unpublish(r.Context(), pub); err != nil {
		s.publishError(w, r, err)
		return
	}
	s.audit.Record(r, audit.Event{
		Kind: audit.KindPublicationDeleted, UserID: u.ID, Subject: u.Email, NodeID: node.ID,
		Detail: map[string]any{"node": node.ID.String(), "name": node.Name, "publication": pub.ID.String()},
	})
	w.WriteHeader(http.StatusNoContent)
}

// --- the viewer API (anonymous) ---

type publicationInfoDTO struct {
	Title         string  `json:"title"`
	EntryPath     string  `json:"entry_path"`
	Rev           int32   `json:"rev"`
	PublishedAt   string  `json:"published_at"`
	PublishedBy   string  `json:"published_by"`
	ExpiresAt     *string `json:"expires_at"`
	NeedsPassword bool    `json:"needs_password"`
	ContentURL    string  `json:"content_url,omitempty"`
}

// resolvePublication answers 404 for every reason a token does not serve.
func (s *Server) resolvePublication(w http.ResponseWriter, r *http.Request) (publish.Served, string, bool) {
	token := chi.URLParam(r, "token")
	sv, err := s.publish.Resolve(r.Context(), token)
	if err != nil {
		if !errors.Is(err, publish.ErrNotFound) {
			Error(w, r, err)
			return publish.Served{}, "", false
		}
		writeProblem(w, http.StatusNotFound, "Not Found", "this page is not published, or no longer")
		return publish.Served{}, "", false
	}
	return sv, token, true
}

func (s *Server) handlePublicationInfo(w http.ResponseWriter, r *http.Request) {
	sv, token, ok := s.resolvePublication(w, r)
	if !ok {
		return
	}
	p := sv.Publication
	d := publicationInfoDTO{
		Title:         p.Title,
		EntryPath:     p.EntryPath,
		Rev:           p.Rev,
		PublishedAt:   p.PublishedAt.Time.UTC().Format(time.RFC3339),
		PublishedBy:   sv.CreatorName,
		NeedsPassword: sv.NeedsPassword(),
	}
	if p.ExpiresAt.Valid {
		e := p.ExpiresAt.Time.UTC().Format(time.RFC3339)
		d.ExpiresAt = &e
	}
	if !d.NeedsPassword {
		d.ContentURL = s.publish.ContentURL(token, "", p.EntryPath)
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, d)
}

// handlePublicationUnlock trades the password for a content URL carrying a
// grant. A wrong password is 422, not 401: the client reads 401 as "your
// session is gone".
func (s *Server) handlePublicationUnlock(w http.ResponseWriter, r *http.Request) {
	sv, token, ok := s.resolvePublication(w, r)
	if !ok {
		return
	}
	var body struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "invalid body")
		return
	}
	grant, err := s.publish.Unlock(sv, body.Password)
	if err != nil {
		writeProblem(w, http.StatusUnprocessableEntity, "Unprocessable Entity", "wrong password")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]string{
		"content_url": s.publish.ContentURL(token, grant, sv.Publication.EntryPath),
	})
}
