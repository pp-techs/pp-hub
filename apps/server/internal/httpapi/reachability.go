package httpapi

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

// reachability answers "may this user see this node" for a whole page of rows.
//
// The cross-space feeds — search, starred, recent, activity — used to scope
// themselves with `JOIN space_members`, which was exactly right while belonging
// to a space meant reading all of it. It is exactly wrong now: it would show a
// member every file in their company regardless of what they were granted, so
// a folder an admin had just closed off would keep surfacing in search.
//
// Resolving each row on its own would mean one recursive walk per result. This
// does it in two queries for the whole page instead: one NodeAccessRanks call
// covering every node id at once, and one role lookup per distinct space.
type reachability struct {
	ranks   map[pgtype.UUID]int
	adminIn map[pgtype.UUID]bool
}

// newReachability resolves a page of (node, space) pairs for one user.
func (s *Server) newReachability(ctx context.Context, userID pgtype.UUID, nodeIDs, spaceIDs []pgtype.UUID) (*reachability, error) {
	rc := &reachability{adminIn: make(map[pgtype.UUID]bool)}

	ranks, err := s.share.AccessRanks(ctx, dedupe(nodeIDs), userID)
	if err != nil {
		return nil, err
	}
	rc.ranks = ranks

	// An admin reaches everything in the space they administer, and holds no
	// grants saying so, so the role has to be consulted separately.
	for _, spaceID := range dedupe(spaceIDs) {
		role, err := s.spaces.RoleOf(ctx, spaceID, userID)
		if err != nil {
			continue // not a member: no floor, grants alone decide
		}
		rc.adminIn[spaceID] = roleFloor(role) >= 1
	}
	return rc, nil
}

// can reports whether the user may see a node at all (rank >= 1).
func (rc *reachability) can(nodeID, spaceID pgtype.UUID) bool {
	if rc.adminIn[spaceID] {
		return true
	}
	return rc.ranks[nodeID] >= 1
}

// filterRows keeps only the rows the user can reach. The accessors pull the
// node and space id out of whichever row type the caller has, since each feed
// has its own generated struct.
func filterRows[T any](rc *reachability, rows []T, nodeOf, spaceOf func(T) pgtype.UUID) []T {
	out := rows[:0:0]
	for _, row := range rows {
		if rc.can(nodeOf(row), spaceOf(row)) {
			out = append(out, row)
		}
	}
	return out
}

// reachableRows is the whole pattern in one call: collect the ids, resolve, filter.
func reachableRows[T any](ctx context.Context, s *Server, userID pgtype.UUID, rows []T, nodeOf, spaceOf func(T) pgtype.UUID) ([]T, error) {
	if len(rows) == 0 {
		return rows, nil
	}
	nodeIDs := make([]pgtype.UUID, 0, len(rows))
	spaceIDs := make([]pgtype.UUID, 0, len(rows))
	for _, row := range rows {
		nodeIDs = append(nodeIDs, nodeOf(row))
		spaceIDs = append(spaceIDs, spaceOf(row))
	}
	rc, err := s.newReachability(ctx, userID, nodeIDs, spaceIDs)
	if err != nil {
		return nil, err
	}
	return filterRows(rc, rows, nodeOf, spaceOf), nil
}

func dedupe(ids []pgtype.UUID) []pgtype.UUID {
	seen := make(map[pgtype.UUID]struct{}, len(ids))
	out := make([]pgtype.UUID, 0, len(ids))
	for _, id := range ids {
		if !id.Valid {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

// isSpaceAdmin is the single-space form, for handlers that already know which
// space they are in.
func (s *Server) isSpaceAdmin(ctx context.Context, spaceID, userID pgtype.UUID) bool {
	role, err := s.spaces.RoleOf(ctx, spaceID, userID)
	return err == nil && role == adminRole
}

const adminRole = store.MemberRoleAdmin

// reachableActivity filters an activity feed.
//
// Activity needs its own rule because a row's node_id is nullable: purging a
// file sets it NULL (ON DELETE SET NULL) while node_name stays denormalised on
// the row, precisely so the feed can still say the file existed and was
// deleted. Such a row cannot be resolved to a node, so it cannot be permission
// checked the usual way.
//
// The rule: an admin sees their space's whole feed; anyone else sees an event
// about a node they can reach, plus their own actions — you may always see
// what you yourself did, including to something now purged.
func reachableActivity[T any](
	ctx context.Context, s *Server, userID pgtype.UUID, rows []T,
	nodeOf, spaceOf, actorOf func(T) pgtype.UUID,
) ([]T, error) {
	if len(rows) == 0 {
		return rows, nil
	}
	nodeIDs := make([]pgtype.UUID, 0, len(rows))
	spaceIDs := make([]pgtype.UUID, 0, len(rows))
	for _, row := range rows {
		if id := nodeOf(row); id.Valid {
			nodeIDs = append(nodeIDs, id)
		}
		spaceIDs = append(spaceIDs, spaceOf(row))
	}
	rc, err := s.newReachability(ctx, userID, nodeIDs, spaceIDs)
	if err != nil {
		return nil, err
	}
	out := rows[:0:0]
	for _, row := range rows {
		if activityVisible(rc, userID, nodeOf(row), spaceOf(row), actorOf(row)) {
			out = append(out, row)
		}
	}
	return out, nil
}

// activityVisible is the rule itself, split out so it can be tested directly.
func activityVisible(rc *reachability, userID, nodeID, spaceID, actorID pgtype.UUID) bool {
	switch {
	case rc.adminIn[spaceID]:
		return true
	case actorID == userID:
		// You may always see what you yourself did, including to a file that
		// has since been purged and can no longer be resolved.
		return true
	default:
		return nodeID.Valid && rc.ranks[nodeID] >= 1
	}
}
