package httpapi

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
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
	nodeGranted   = uid(1) // member holds a grant reaching this one
	nodeUnreached = uid(2) // no grant reaches it
	spaceA        = uid(10)
	spaceB        = uid(11) // the caller administers this one
	me            = uid(20)
	someoneElse   = uid(21)
)

func newTestReach() *reachability {
	return &reachability{
		ranks:   map[pgtype.UUID]int{nodeGranted: 1},
		adminIn: map[pgtype.UUID]bool{spaceA: false, spaceB: true},
	}
}

// The cross-space feeds are fed by SQL that still scopes only by membership, so
// this filter is the thing standing between a member and every file in their
// company. If it ever returns true for an ungranted node, search and starred
// start listing folders an admin has closed off.
func TestReachabilityCan(t *testing.T) {
	rc := newTestReach()

	if !rc.can(nodeGranted, spaceA) {
		t.Error("a node the member holds a grant on must be reachable")
	}
	if rc.can(nodeUnreached, spaceA) {
		t.Error("a node with no grant leaked through — this is the whole model failing")
	}
	// An admin holds no grant rows saying so; the role is the authority.
	if !rc.can(nodeUnreached, spaceB) {
		t.Error("an admin must reach every node in the space they administer")
	}
}

func TestFilterRowsDropsUnreachable(t *testing.T) {
	type row struct{ node, space pgtype.UUID }
	rows := []row{
		{nodeGranted, spaceA},
		{nodeUnreached, spaceA}, // must be dropped
		{nodeUnreached, spaceB}, // kept: admin there
	}
	got := filterRows(newTestReach(), rows,
		func(r row) pgtype.UUID { return r.node },
		func(r row) pgtype.UUID { return r.space })

	if len(got) != 2 {
		t.Fatalf("kept %d rows, want 2", len(got))
	}
	for _, r := range got {
		if r.node == nodeUnreached && r.space == spaceA {
			t.Error("kept a row for an ungranted node in a space the caller does not administer")
		}
	}
}

// filterRows must not scribble over the caller's backing array: the handlers
// re-assign the result and then keep formatting rows from it.
func TestFilterRowsDoesNotAliasInput(t *testing.T) {
	type row struct{ node, space pgtype.UUID }
	rows := []row{{nodeUnreached, spaceA}, {nodeGranted, spaceA}}
	_ = filterRows(newTestReach(), rows,
		func(r row) pgtype.UUID { return r.node },
		func(r row) pgtype.UUID { return r.space })

	if rows[0].node != nodeUnreached {
		t.Error("filterRows overwrote the input slice")
	}
}

// Activity has a rule of its own because node_id is nullable: purging a file
// sets it NULL while node_name stays on the row, so such an event cannot be
// resolved to a node at all.
func TestReachableActivityRules(t *testing.T) {
	type ev struct{ node, space, actor pgtype.UUID }
	null := pgtype.UUID{} // a purged node

	tests := []struct {
		name string
		row  ev
		want bool
	}{
		{"event on a node I can reach", ev{nodeGranted, spaceA, someoneElse}, true},
		{"event on a node I cannot reach", ev{nodeUnreached, spaceA, someoneElse}, false},
		{"my own action, node purged", ev{null, spaceA, me}, true},
		{"someone else's action on a purged node", ev{null, spaceA, someoneElse}, false},
		{"my own action on a node I cannot reach", ev{nodeUnreached, spaceA, me}, true},
		{"admin sees everything in their space", ev{nodeUnreached, spaceB, someoneElse}, true},
	}

	rc := newTestReach()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := activityVisible(rc, me, tt.row.node, tt.row.space, tt.row.actor)
			if got != tt.want {
				t.Errorf("visible = %v, want %v", got, tt.want)
			}
		})
	}
}
