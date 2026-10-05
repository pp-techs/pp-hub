// Package activity records what happened in a space: who did what, to which
// node, when. It backs the Home feed, the Recent listing and the per-space
// timeline.
//
// Recording is deliberately best-effort. An activity row is a description of
// work that already succeeded; failing the user's upload because the audit
// line could not be written would trade a real outcome for a cosmetic one. The
// Record* methods therefore swallow their errors after logging them, and
// callers do not check a return value.
package activity

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

// Kind is the verb of an event. Text rather than a database enum, so adding a
// verb is a code change rather than a migration.
type Kind string

const (
	KindUploaded Kind = "uploaded"
	KindCreated  Kind = "created"
	KindRenamed  Kind = "renamed"
	KindMoved    Kind = "moved"
	KindTrashed  Kind = "trashed"
	KindRestored Kind = "restored"
	KindPurged   Kind = "purged"
	KindShared   Kind = "shared"
	KindUnshared Kind = "unshared"
	KindReverted Kind = "reverted"
	KindInvited  Kind = "invited"
	KindJoined   Kind = "joined"
)

type Service struct {
	q *store.Queries
}

func NewService(q *store.Queries) *Service {
	return &Service{q: q}
}

// Event is one thing that happened.
type Event struct {
	SpaceID pgtype.UUID
	ActorID pgtype.UUID
	// NodeID may be zero for events that are not about a node (a member
	// joining, say).
	NodeID pgtype.UUID
	Kind   Kind
	// NodeName is stored alongside the id so the feed still reads correctly
	// after the node is renamed or purged.
	NodeName string
	// Detail carries verb-specific extras: the old name on a rename, the
	// destination on a move.
	Detail map[string]any
}

// Record writes an event, logging and swallowing any failure.
func (s *Service) Record(ctx context.Context, e Event) {
	detail := []byte("{}")
	if len(e.Detail) > 0 {
		if encoded, err := json.Marshal(e.Detail); err == nil {
			detail = encoded
		}
	}

	err := s.q.RecordActivity(ctx, store.RecordActivityParams{
		SpaceID:  e.SpaceID,
		ActorID:  e.ActorID,
		NodeID:   e.NodeID,
		Kind:     string(e.Kind),
		NodeName: e.NodeName,
		Detail:   detail,
	})
	if err != nil {
		slog.WarnContext(ctx, "activity not recorded", "kind", e.Kind, "err", err)
	}
}

// Node is the common case: an event about a node in its own space.
func (s *Service) Node(ctx context.Context, actorID pgtype.UUID, node store.Node, kind Kind, detail map[string]any) {
	s.Record(ctx, Event{
		SpaceID:  node.SpaceID,
		ActorID:  actorID,
		NodeID:   node.ID,
		Kind:     kind,
		NodeName: node.Name,
		Detail:   detail,
	})
}

// ForSpace returns the latest events in one space.
func (s *Service) ForSpace(ctx context.Context, spaceID pgtype.UUID, limit int32) ([]store.ListSpaceActivityRow, error) {
	return s.q.ListSpaceActivity(ctx, store.ListSpaceActivityParams{SpaceID: spaceID, Limit: limit})
}

// ForUser returns the latest events across every space the user belongs to.
func (s *Service) ForUser(ctx context.Context, userID pgtype.UUID, limit int32) ([]store.ListMyActivityRow, error) {
	return s.q.ListMyActivity(ctx, store.ListMyActivityParams{UserID: userID, Limit: limit})
}

// Recent returns nodes the user themselves last touched, newest first. An
// invalid spaceID spans every space the user belongs to; a set one scopes the
// listing to that space.
func (s *Service) Recent(ctx context.Context, userID, spaceID pgtype.UUID) ([]store.ListRecentNodesRow, error) {
	return s.q.ListRecentNodes(ctx, store.ListRecentNodesParams{ActorID: userID, SpaceID: spaceID})
}
