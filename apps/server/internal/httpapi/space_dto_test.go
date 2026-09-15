package httpapi

import (
	"encoding/json"
	"testing"

	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

// The settings form reads the current values out of this DTO and PATCHes the
// whole set back. A field missing here reads as undefined in the client, which
// the form then sends as an empty string — silently clearing the value that
// was just saved. So every settings field must round-trip, and must be present
// in the JSON even when empty (no `omitempty`).
func TestSpaceDTOCarriesSettingsFields(t *testing.T) {
	var space store.Space
	_ = space.ID.Scan("11111111-1111-1111-1111-111111111111")
	_ = space.OwnerID.Scan("22222222-2222-2222-2222-222222222222")
	space.Name = "Acme Core"
	space.Slug = "acme-core"
	space.Description = "Everything the core team works on"
	space.DefaultRole = store.MemberRoleMember
	space.QuotaBytes = 1 << 30

	dto := spaceToDTO(space, "admin")
	if dto.Slug != "acme-core" {
		t.Errorf("slug = %q, want acme-core", dto.Slug)
	}
	if dto.Description != "Everything the core team works on" {
		t.Errorf("description = %q", dto.Description)
	}
	if dto.DefaultRole != "member" {
		t.Errorf("default_role = %q, want member", dto.DefaultRole)
	}

	// Present-when-empty is the part that actually prevents the data loss.
	raw, err := json.Marshal(spaceToDTO(store.Space{}, "admin"))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, key := range []string{"slug", "description", "default_role"} {
		if _, ok := fields[key]; !ok {
			t.Errorf("%q missing from the JSON of an empty space — the settings form would clear it", key)
		}
	}
}
