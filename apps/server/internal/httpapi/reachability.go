package httpapi

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/nhatbui/pp-hub/apps/server/internal/share"
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
// does it in two queries for the whole page instead: one AccessCaps call covering
// every node id at once, and one IsAdmin check per distinct space.
type reachability struct {
	resolved map[pgtype.UUID]share.Resolved
	adminIn  map[pgtype.UUID]bool
	userID   pgtype.UUID
}

// newReachability resolves a page of (node, space) pairs for one user.
func (s *Server) newReachability(ctx context.Context, userID pgtype.UUID, nodeIDs, spaceIDs []pgtype.UUID) (*reachability, error) {
	rc := &reachability{adminIn: make(map[pgtype.UUID]bool), userID: userID}

	resolved, err := s.share.AccessCaps(ctx, dedupe(nodeIDs), userID)
	if err != nil {
		return nil, err
	}
	rc.resolved = resolved

	// Admin group membership gives space-administration powers (member management,
	// settings) but no implicit file access. For reachability purposes, we still
	// record it here so that admins can be distinguished from plain members in
	// feeds — but it does NOT set a permission floor for can().
	for _, spaceID := range dedupe(spaceIDs) {
		rc.adminIn[spaceID] = s.spaces.IsAdmin(ctx, spaceID, userID)
	}
	return rc, nil
}

// caps is the caller's whole resolved capability set for a node on this page.
func (rc *reachability) caps(nodeID pgtype.UUID) share.Caps {
	return share.EffectiveCaps(rc.resolved[nodeID])
}

// can reports whether the user may see a node at all: they own it or hold a
// grant on it or an ancestor. There is no role floor.
func (rc *reachability) can(nodeID, spaceID pgtype.UUID) bool {
	return rc.caps(nodeID).Has(share.CapView)
}

// stamp copies the resolution onto a DTO. The cross-space feeds resolve caps
// to filter themselves, and that resolution is exactly what a client needs to
// know which actions to offer — so every row carries it, at no extra query.
func (rc *reachability) stamp(d *nodeDTO, nodeID pgtype.UUID) {
	c := rc.caps(nodeID)
	d.Caps = &c
}

// hideParents blanks the parent name on every row whose parent the caller
// cannot open. A row they reach through a grant on the node itself — an
// outsider's share root, a member's file shared alone — would otherwise name
// the folder around it, which may itself say more than the file does. One
// more AccessCaps batch for the page's parents.
func hideParents[T any](ctx context.Context, s *Server, rc *reachability, rows []T,
	parentOf func(T) pgtype.UUID, blank func(*T)) error {
	var ids []pgtype.UUID
	for _, row := range rows {
		if p := parentOf(row); p.Valid {
			if _, known := rc.resolved[p]; !known {
				ids = append(ids, p)
			}
		}
	}
	if len(ids) > 0 {
		more, err := s.share.AccessCaps(ctx, dedupe(ids), rc.userID)
		if err != nil {
			return err
		}
		if rc.resolved == nil {
			rc.resolved = make(map[pgtype.UUID]share.Resolved, len(more))
		}
		for id, r := range more {
			rc.resolved[id] = r
		}
	}
	for i := range rows {
		if p := parentOf(rows[i]); p.Valid && !rc.caps(p).View {
			blank(&rows[i])
		}
	}
	return nil
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

// reachableRows is the whole pattern in one call: collect the ids, resolve,
// filter. The resolution comes back with the rows so the caller can stamp it
// onto the DTOs it builds.
func reachableRows[T any](ctx context.Context, s *Server, userID pgtype.UUID, rows []T, nodeOf, spaceOf func(T) pgtype.UUID) ([]T, *reachability, error) {
	if len(rows) == 0 {
		return rows, &reachability{userID: userID}, nil
	}
	nodeIDs := make([]pgtype.UUID, 0, len(rows))
	spaceIDs := make([]pgtype.UUID, 0, len(rows))
	for _, row := range rows {
		nodeIDs = append(nodeIDs, nodeOf(row))
		spaceIDs = append(spaceIDs, spaceOf(row))
	}
	rc, err := s.newReachability(ctx, userID, nodeIDs, spaceIDs)
	if err != nil {
		return nil, nil, err
	}
	return filterRows(rc, rows, nodeOf, spaceOf), rc, nil
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

// isSpaceAdmin reports whether the user is in the space's Admin group.
func (s *Server) isSpaceAdmin(ctx context.Context, spaceID, userID pgtype.UUID) bool {
	return s.spaces.IsAdmin(ctx, spaceID, userID)
}

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
//
// An admin sees their space's entire feed. Otherwise: visible if the node is
// reachable, or the actor is the current user (you always see your own actions,
// even on nodes since purged).
func activityVisible(rc *reachability, userID, nodeID, spaceID, actorID pgtype.UUID) bool {
	switch {
	case actorID == userID:
		return true
	case nodeID.Valid:
		return rc.can(nodeID, spaceID)
	default:
		// A purged row: node_id is NULL by ON DELETE SET NULL. Only an admin
		// of this space can see it — they see the whole feed anyway.
		return rc.adminIn[spaceID]
	}
}
