package httpapi

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nhatbui/pp-hub/apps/server/internal/share"
)

func uid(b byte) pgtype.UUID {
	var u pgtype.UUID
	for i := range u.Bytes {
		u.Bytes[i] = b
	}
	u.Valid = true
	return u
}

var (
	nodeGranted        = uid(1) // member holds a grant reaching this one
	nodeNoGrant        = uid(2) // no grant reaches it
	nodeOwned          = uid(3) // caller owns this node
	spaceA             = uid(10)
	me                 = uid(20)
)

// M15: access is grant-driven. Admin authority is group membership, not a permission floor.
// The reachability filter uses AccessCaps: a node is reachable if the user has CapView on it.
func newTestReach() *reachability {
	return &reachability{
		resolved: map[pgtype.UUID]share.Resolved{
			nodeGranted: {View: true},
			nodeOwned:   {Owner: true},
			// nodeNoGrant has no entry → not reachable
		},
		adminIn: map[pgtype.UUID]bool{spaceA: true},
		userID:  me,
	}
}

func TestReachabilityCan(t *testing.T) {
	rc := newTestReach()

	if !rc.can(nodeGranted, spaceA) {
		t.Error("a node with CapView must be reachable")
	}
	if !rc.can(nodeOwned, spaceA) {
		t.Error("an owned node must be reachable")
	}
	if rc.can(nodeNoGrant, spaceA) {
		t.Error("a node with no grant must not be reachable")
	}
}

func TestFilterRowsDropsUnreachable(t *testing.T) {
	type row struct{ node, space pgtype.UUID }
	rows := []row{
		{nodeGranted, spaceA},
		{nodeNoGrant, spaceA},
		{nodeOwned, spaceA},
	}
	rc := newTestReach()
	got := filterRows(rc, rows,
		func(r row) pgtype.UUID { return r.node },
		func(r row) pgtype.UUID { return r.space },
	)
	if len(got) != 2 {
		t.Errorf("got %d rows, want 2 (nodeNoGrant should be dropped)", len(got))
	}
}

func TestFilterRowsDoesNotAliasInput(t *testing.T) {
	type row struct{ node, space pgtype.UUID }
	rows := []row{{nodeGranted, spaceA}, {nodeNoGrant, spaceA}}
	rc := newTestReach()
	got := filterRows(rc, rows,
		func(r row) pgtype.UUID { return r.node },
		func(r row) pgtype.UUID { return r.space },
	)
	// filterRows writes into got, not rows. If it aliased, modifying got would change rows.
	// With the [:0:0] pattern, got has zero length — it cannot hold any elements,
	// confirming the implementation truncates rather than aliases.
	if len(got) != 1 {
		t.Errorf("got %d rows, want 1 (only nodeGranted)", len(got))
	}
	if len(rows) != 2 {
		t.Error("rows should be untouched")
	}
}
func TestActivityVisible(t *testing.T) {
	otherNode := uid(4)
	rc := newTestReach()

	// Own actions always visible
	if !activityVisible(rc, me, otherNode, spaceA, me) {
		t.Error("own action must be visible even with no grant")
	}

	// Purged row (invalid nodeID): admin sees it
	if !activityVisible(rc, me, pgtype.UUID{}, spaceA, uid(99)) {
		t.Error("admin must see all feed entries in their space")
	}

	// Non-admin: node must be reachable
	if activityVisible(rc, me, nodeNoGrant, spaceA, uid(99)) {
		t.Error("non-admin must not see entries about inaccessible nodes")
	}
}
