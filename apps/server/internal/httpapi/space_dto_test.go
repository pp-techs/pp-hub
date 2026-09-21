package httpapi

import (
	"encoding/json"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
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
	space.DefaultAsAdmin = false
	space.QuotaBytes = 1 << 30

	dto := spaceToDTO(space, true, 2)
	if dto.Slug != "acme-core" {
		t.Errorf("slug = %q, want acme-core", dto.Slug)
	}
	if dto.Description != "Everything the core team works on" {
		t.Errorf("description = %q", dto.Description)
	}

	// Present-when-empty is the part that actually prevents data loss.
	raw, err := json.Marshal(spaceToDTO(store.Space{}, false, 0))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, key := range []string{"slug", "description"} {
		if _, ok := fields[key]; !ok {
			t.Errorf("%q missing from the JSON of an empty space — the settings form would clear it", key)
		}
	}
	// admin_count has no omitempty for the same class of reason: zero is the
	// value that matters most, and omitting it reads as "unknown" to a
	// client that must warn the space has no administrator.
	if count, ok := fields["admin_count"]; !ok || count != float64(0) {
		t.Errorf("admin_count = %v (present=%v), want 0 on the wire", count, ok)
	}
}

// An unlinked space mirrors no organization, and the client uses the absence
// of the field to say so. A linked one must carry the id.
func TestSpaceDTOReportsTheOrganizationLink(t *testing.T) {
	var space store.Space
	if dto := spaceToDTO(space, false, 0); dto.IdpOrgID != "" {
		t.Errorf("unlinked space reported organization %q", dto.IdpOrgID)
	}
	space.IdpOrgID = pgtype.Text{String: "org_abc", Valid: true}
	if dto := spaceToDTO(space, false, 1); dto.IdpOrgID != "org_abc" {
		t.Errorf("idp_org_id = %q, want org_abc", dto.IdpOrgID)
	}
}
