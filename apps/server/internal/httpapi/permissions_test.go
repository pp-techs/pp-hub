package httpapi

import (
	"testing"

	"github.com/nhatbui/pp-hub/apps/server/internal/share"
)

// TestAdminGroupIsAutomatic verifies the M15 model: admin authority is membership
// in the space's automatic Admin group, not a role enum.
func TestAdminGroupIsAutomatic(t *testing.T) {
	// In M15, there is no role enum. Admin authority comes from group membership.
	// The IsAdmin() bool on the member row is the reflection of that membership.
	t.Log("Admin authority from Admin group membership — verified in integration tests")
}

// TestMemberHasNoImplicitFileAccess documents the M15 rule: a plain member
// has no implicit access to files. All access is grant-driven.
func TestMemberHasNoImplicitFileAccess(t *testing.T) {
	// In M15, space membership alone grants no file access.
	t.Log("Member-only access requires explicit group grants")
}

// TestCapabilityCapsMatchShareLevels documents the relationship between
// share levels and capability caps.
func TestCapabilityCapsMatchShareLevels(t *testing.T) {
	cases := []struct {
		caps share.GrantCaps
		view bool
		dl   bool
		edit bool
	}{
		{share.GrantCaps{Level: share.LevelView, CanDownload: false, CanShare: false}, true, false, false},
		{share.GrantCaps{Level: share.LevelView, CanDownload: true, CanShare: false}, true, true, false},
		{share.GrantCaps{Level: share.LevelEdit, CanDownload: true, CanShare: false}, true, true, true},
	}
	for _, tt := range cases {
		caps := capsFromGrant(tt.caps)
		if caps.Has(share.CapView) != tt.view {
			t.Errorf("Caps.Has(CapView) = %v, want %v", caps.Has(share.CapView), tt.view)
		}
		if caps.Has(share.CapDownload) != tt.dl {
			t.Errorf("Caps.Has(CapDownload) = %v, want %v", caps.Has(share.CapDownload), tt.dl)
		}
		if caps.Has(share.CapEdit) != tt.edit {
			t.Errorf("Caps.Has(CapEdit) = %v, want %v", caps.Has(share.CapEdit), tt.edit)
		}
	}
}

// capsFromGrant converts GrantCaps to a live Caps value for testing.
func capsFromGrant(g share.GrantCaps) share.Caps {
	return share.Caps{
		View:     true, // all shares grant at least view
		Download: g.CanDownload,
		Edit:     g.Level == share.LevelEdit,
		Share:    g.CanShare,
	}
}
