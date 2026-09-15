package httpapi

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nhatbui/pp-hub/apps/server/internal/activity"
	"github.com/nhatbui/pp-hub/apps/server/internal/fs"
	"github.com/nhatbui/pp-hub/apps/server/internal/spaces"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

// timeLayout is the wire format for every timestamp the API emits.
const timeLayout = time.RFC3339

// trashNodeDTO is a node listed in the trash: the normal node shape plus the
// three things you need in order to decide whether to restore it — when it was
// deleted, where it came from, and (for a folder) how much comes back with it.
type trashNodeDTO struct {
	nodeDTO
	DeletedAt string `json:"deleted_at"`
	// Ancestor names joined with " / ", empty when the node sat at the top
	// level of its space.
	Location string `json:"location"`
	// Direct children, for folders. Files report 0.
	ChildCount int64 `json:"child_count"`
	// Who moved it to the trash. Empty for items deleted before the column
	// existed — there is no honest way to attribute those.
	DeletedBy string `json:"deleted_by,omitempty"`
	// When automatic purging will remove it. Empty when the space keeps its
	// trash indefinitely, which is the default.
	PurgesAt string `json:"purges_at,omitempty"`
}

type restoreResultDTO struct {
	Node nodeDTO `json:"node"`
	// Relocated is true when the original folder was itself in the trash and
	// the node came back at the top level instead.
	Relocated bool `json:"relocated"`
	// RenamedFrom is set when a live sibling had taken the name.
	RenamedFrom string `json:"renamed_from,omitempty"`
}

// requireTrashAccess resolves the space from the URL and demands owner rank.
//
// Emptying the trash is irreversible and space-wide, so it sits with the owner.
// It is gated at the space level rather than through requireNodeAccess because
// the nodes involved are deleted, and the ancestor walk that widens permission
// by direct share cannot be trusted to terminate on live rows.
func (s *Server) requireTrashAccess(w http.ResponseWriter, r *http.Request) (*store.User, pgtype.UUID) {
	u := s.requireUser(w, r)
	if u == nil {
		return nil, pgtype.UUID{}
	}
	spaceID, err := uuidParam(r, "spaceID")
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "invalid space id")
		return nil, pgtype.UUID{}
	}
	if err := s.spaces.RequireRole(r.Context(), spaceID, u.ID, spaces.RoleOwner); err != nil {
		s.spaceError(w, r, err)
		return nil, pgtype.UUID{}
	}
	return u, spaceID
}

// requireTrashedNode resolves a node by id even though it is soft-deleted, then
// checks the caller against the node's own space at `min`.
//
// Restoring takes editor, matching delete: whoever can put something in the
// trash must be able to take it back out, or the bin becomes a one-way door.
// Purging takes owner, because it is irreversible.
func (s *Server) requireTrashedNode(w http.ResponseWriter, r *http.Request, min spaces.Role) (*store.User, store.Node) {
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
	if err := s.spaces.RequireRole(r.Context(), node.SpaceID, u.ID, min); err != nil {
		s.spaceError(w, r, err)
		return nil, store.Node{}
	}
	return u, node
}

func (s *Server) handleRestoreNode(w http.ResponseWriter, r *http.Request) {
	u, node := s.requireTrashedNode(w, r, spaces.RoleEditor)
	if u == nil {
		return
	}
	restored, outcome, err := s.fs.Restore(r.Context(), node.ID)
	if err != nil {
		s.trashError(w, r, err)
		return
	}
	s.activity.Node(r.Context(), u.ID, restored, activity.KindRestored, nil)
	s.recount(r, node.SpaceID)
	writeJSON(w, http.StatusOK, restoreResultDTO{
		Node:        nodeToDTO(restored, nil),
		Relocated:   outcome.Relocated,
		RenamedFrom: outcome.RenamedFrom,
	})
}

func (s *Server) handlePurgeNode(w http.ResponseWriter, r *http.Request) {
	u, node := s.requireTrashedNode(w, r, spaces.RoleOwner)
	if u == nil {
		return
	}
	if err := s.fs.Purge(r.Context(), node.ID); err != nil {
		s.trashError(w, r, err)
		return
	}
	// Recorded before the node id goes away: activity_events.node_id is
	// ON DELETE SET NULL, so the row survives with its denormalised name.
	s.activity.Node(r.Context(), u.ID, node, activity.KindPurged, nil)
	s.recount(r, node.SpaceID)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleEmptyTrash(w http.ResponseWriter, r *http.Request) {
	u, spaceID := s.requireTrashAccess(w, r)
	if u == nil {
		return
	}
	purged, err := s.fs.EmptyTrash(r.Context(), spaceID)
	if err != nil {
		s.trashError(w, r, err)
		return
	}
	s.recount(r, spaceID)
	writeJSON(w, http.StatusOK, map[string]int{"purged": purged})
}

// trashError adds the "not actually in the trash" case to the node mapping.
func (s *Server) trashError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, fs.ErrNotDeleted) {
		writeProblem(w, http.StatusConflict, "Conflict", err.Error())
		return
	}
	s.nodeError(w, r, err)
}

// trashRowToDTO maps a trash listing row onto the wire shape.
func trashRowToDTO(n store.ListDeletedWithMetaRow) trashNodeDTO {
	id, _ := n.ID.Value()
	sid, _ := n.SpaceID.Value()
	dto := nodeDTO{
		ID:        id.(string),
		SpaceID:   sid.(string),
		Kind:      string(n.Kind),
		Name:      n.Name,
		CreatedAt: n.CreatedAt.Time.Format(timeLayout),
	}
	if n.ParentID.Valid {
		if p, _ := n.ParentID.Value(); p != nil {
			dto.ParentID = p.(string)
		}
	}
	if n.VID.Valid {
		dto.Size = n.VSize.Int64
		dto.Mime = n.Mime.String
		dto.Version = n.VersionNo.Int32
		dto.ModifiedAt = n.VCreatedAt.Time.Format(timeLayout)
	}
	if n.Kind == store.NodeKindDir && dto.ModifiedAt == "" {
		dto.ModifiedAt = n.CreatedAt.Time.Format(timeLayout)
	}
	if n.OwnerName.Valid && n.OwnerName.String != "" {
		dto.Owner = n.OwnerName.String
	} else if n.OwnerEmail.Valid {
		dto.Owner = n.OwnerEmail.String
	}
	if n.OwnerUid.Valid {
		if o, _ := n.OwnerUid.Value(); o != nil {
			dto.OwnerID = o.(string)
		}
	}
	out := trashNodeDTO{
		nodeDTO:    dto,
		Location:   n.Location,
		ChildCount: n.ChildCount,
		DeletedBy:  n.DeletedByName,
	}
	if n.DeletedAt.Valid {
		out.DeletedAt = n.DeletedAt.Time.Format(timeLayout)
	}
	if n.PurgesAt.Valid {
		out.PurgesAt = n.PurgesAt.Time.Format(timeLayout)
	}
	return out
}

// recount refreshes a space's used_bytes after anything that changes the total.
//
// Best-effort, like activity: the user's upload or delete has already
// succeeded, and a stale meter is a far smaller problem than a failed write
// that looks like the operation itself failed.
func (s *Server) recount(r *http.Request, spaceID pgtype.UUID) {
	if err := s.fs.RecountUsage(r.Context(), spaceID); err != nil {
		slog.Warn("space usage not recounted", "space", spaceID, "err", err)
	}
}
