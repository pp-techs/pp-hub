package httpapi

import (
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nhatbui/pp-hub/apps/server/internal/activity"
	"github.com/nhatbui/pp-hub/apps/server/internal/fs"
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
	// BuriedOwn counts the caller's own live nodes sitting inside this
	// tombstone. ReadOnly marks a row the caller can see *only* because of
	// them: somebody else trashed a folder their work was in, so the row is
	// there to explain where the work went, not to be restored or purged.
	BuriedOwn int64 `json:"buried_own,omitempty"`
	ReadOnly  bool  `json:"read_only,omitempty"`
}

// requireTrashAccess resolves the space and checks admin status.
// Returns (user, spaceID, ok).
func (s *Server) requireTrashAccess(w http.ResponseWriter, r *http.Request) (*store.User, pgtype.UUID, bool) {
	u := caller(r)
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
	u := caller(r)
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
// It answers what the client reports: the node as restored, and whether it
// had to land elsewhere (relocated) or under another name (renamed_from).
func (s *Server) handleRestoreNode(w http.ResponseWriter, r *http.Request) {
	u, node := s.requireTrashedNode(w, r, share.CapEdit)
	if u == nil {
		return
	}
	restored, outcome, err := s.fs.Restore(r.Context(), node.ID)
	if err != nil {
		s.nodeError(w, r, err)
		return
	}
	s.activity.Node(r.Context(), u.ID, node, activity.KindRestored, nil)
	var ver *store.NodeVersion
	if restored.Kind == store.NodeKindFile {
		if v, err := s.store.GetCurrentVersion(r.Context(), restored.ID); err == nil {
			ver = &v
		}
	}
	writeJSON(w, http.StatusOK, restoreResultDTO{
		Node:        nodeToDTO(restored, ver),
		Relocated:   outcome.Relocated,
		RenamedFrom: outcome.RenamedFrom,
	})
}

type restoreResultDTO struct {
	Node        nodeDTO `json:"node"`
	Relocated   bool    `json:"relocated"`
	RenamedFrom string  `json:"renamed_from,omitempty"`
}

// handlePurgeNode permanently deletes a trashed node. Requires CapEdit.
//
// Answers 200 with how many subtrees survived, because a purge that quietly
// spared somebody else's files needs to say so: the folder is gone, their
// files are at their own top level now.
func (s *Server) handlePurgeNode(w http.ResponseWriter, r *http.Request) {
	u, node := s.requireTrashedNode(w, r, share.CapEdit)
	if u == nil {
		return
	}
	rescued, err := s.fs.Purge(r.Context(), node.ID)
	s.recordRescues(r, u, rescued)
	if err != nil {
		s.nodeError(w, r, err)
		return
	}
	s.activity.Node(r.Context(), u.ID, node, activity.KindPurged, nil)
	writeJSON(w, http.StatusOK, map[string]int{"rescued": len(rescued)})
}

// handleEmptyTrash permanently deletes every trashed node in a space. Admin only.
func (s *Server) handleEmptyTrash(w http.ResponseWriter, r *http.Request) {
	u, spaceID, _ := s.requireTrashAccess(w, r)
	if spaceID.Bytes == [16]byte{} {
		return
	}
	purged, rescued, err := s.fs.EmptyTrash(r.Context(), spaceID)
	s.recordRescues(r, u, rescued)
	if err != nil {
		Error(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"purged": purged, "rescued": len(rescued)})
}

// recordRescues leaves a trail on every subtree a purge lifted out. Its owner
// finds the folder at their top level with no explanation otherwise.
func (s *Server) recordRescues(r *http.Request, actor *store.User, rescued []fs.RescuedNode) {
	for _, item := range rescued {
		s.activity.Node(r.Context(), actor.ID, item.Node, activity.KindMoved,
			map[string]any{"reason": "rescued_from_purge"})
	}
}
