package httpapi

import (
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nhatbui/pp-hub/apps/server/internal/activity"
	"github.com/nhatbui/pp-hub/apps/server/internal/share"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

// timeLayout is the wire format for every timestamp the API emits.
const timeLayout = "2006-01-02T15:04:05Z07:00"

type trashNodeDTO struct {
	ID         string `json:"id"`
	SpaceID    string `json:"space_id"`
	ParentID   string `json:"parent_id,omitempty"`
	Kind       string `json:"kind"`
	Name       string `json:"name"`
	Size       int64  `json:"size,omitempty"`
	Mime       string `json:"mime,omitempty"`
	Version    int32  `json:"version"`
	ModifiedAt string `json:"modified_at"`
	DeletedAt  string `json:"deleted_at,omitempty"`
	PurgesAt   string `json:"purge_at,omitempty"`
}

// requireTrashAccess resolves the space and checks admin status.
// Returns (user, spaceID, ok).
func (s *Server) requireTrashAccess(w http.ResponseWriter, r *http.Request) (*store.User, pgtype.UUID, bool) {
	u := s.requireUser(w, r)
	if u == nil {
		return nil, pgtype.UUID{}, false
	}
	spaceID, err := uuidParam(r, "spaceID")
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "invalid space id")
		return nil, pgtype.UUID{}, false
	}
	if err := s.spaces.RequireAdmin(r.Context(), spaceID, u.ID); err != nil {
		s.spaceError(w, r, err)
		return nil, pgtype.UUID{}, false
	}
	return u, spaceID, true
}

// requireTrashedNode resolves a trashed node and enforces the given capability.
// Returns (user, node).
func (s *Server) requireTrashedNode(w http.ResponseWriter, r *http.Request, need share.Cap) (*store.User, store.Node) {
	u := s.requireUser(w, r)
	if u == nil {
		return nil, store.Node{}
	}
	nodeID, err := uuidParam(r, "nodeID")
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "invalid node id")
		return nil, store.Node{}
	}
	node, err := s.fs.GetAny(r.Context(), nodeID)
	if err != nil {
		s.nodeError(w, r, err)
		return nil, store.Node{}
	}
	caps, err := s.share.EffectiveCapsForNode(r.Context(), node, u.ID)
	if err != nil {
		Error(w, r, err)
		return nil, store.Node{}
	}
	if !caps.Has(need) {
		writeProblem(w, http.StatusForbidden, "Forbidden", "insufficient permission")
		return nil, store.Node{}
	}
	return u, node
}

// handleRestoreNode takes a trashed node out of the trash. Requires CapEdit.
func (s *Server) handleRestoreNode(w http.ResponseWriter, r *http.Request) {
	u, node := s.requireTrashedNode(w, r, share.CapEdit)
	if u == nil {
		return
	}
	_, _, err := s.fs.Restore(r.Context(), node.ID)
	if err != nil {
		s.nodeError(w, r, err)
		return
	}
	s.activity.Node(r.Context(), u.ID, node, activity.KindRestored, nil)
	w.WriteHeader(http.StatusNoContent)
}

// handlePurgeNode permanently deletes a trashed node. Requires CapEdit.
func (s *Server) handlePurgeNode(w http.ResponseWriter, r *http.Request) {
	u, node := s.requireTrashedNode(w, r, share.CapEdit)
	if u == nil {
		return
	}
	if err := s.fs.Purge(r.Context(), node.ID); err != nil {
		s.nodeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleEmptyTrash permanently deletes every trashed node in a space. Admin only.
func (s *Server) handleEmptyTrash(w http.ResponseWriter, r *http.Request) {
	_, spaceID, _ := s.requireTrashAccess(w, r)
	if spaceID.Bytes == [16]byte{} {
		return
	}
	_, err := s.fs.EmptyTrash(r.Context(), spaceID)
	if err != nil {
		Error(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
