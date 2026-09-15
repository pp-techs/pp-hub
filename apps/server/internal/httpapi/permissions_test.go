package httpapi

import (
	"testing"

	"github.com/nhatbui/pp-hub/apps/server/internal/spaces"
)

// The whole permission model turns on this mapping: if a member ever resolves
// above 0, every group grant in the product becomes decorative, because the
// floor alone would already open every node in the space.
func TestRoleFloor(t *testing.T) {
	tests := []struct {
		name string
		role spaces.Role
		want int
	}{
		{"admin holds edit everywhere in their space", spaces.RoleAdmin, 3},
		{"a member is granted nothing by their role alone", spaces.RoleMember, 0},
		{"an unknown role grants nothing", spaces.Role("editor"), 0},
		{"the zero role grants nothing", spaces.Role(""), 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := roleFloor(tt.role); got != tt.want {
				t.Errorf("roleFloor(%q) = %d, want %d", tt.role, got, tt.want)
			}
		})
	}
}

// "editor" and "viewer" are gone from the enum as of migration 0011. A stray
// string literal left behind in a handler would compile fine and silently
// resolve to rank 0 — this pins the fact that only two roles exist.
func TestOnlyTwoRolesExist(t *testing.T) {
	for _, role := range []spaces.Role{"owner", "editor", "viewer"} {
		if roleFloor(role) != 0 {
			t.Errorf("legacy role %q still carries rank — the role scale was not fully collapsed", role)
		}
	}
	if roleFloor(spaces.RoleAdmin) == roleFloor(spaces.RoleMember) {
		t.Error("admin and member resolve to the same rank — the scale does nothing")
	}
}
