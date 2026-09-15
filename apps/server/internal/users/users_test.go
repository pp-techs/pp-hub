package users

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

// toProfile used to assert pgtype.Timestamptz.Value() to a string. Value()
// returns a time.Time, so every profile with a last_seen — which is every user
// who has signed in — panicked, and /api/v1/users/me answered 500.
func TestToProfileFormatsTimestampsWithoutPanicking(t *testing.T) {
	var id pgtype.UUID
	if err := id.Scan("11111111-1111-1111-1111-111111111111"); err != nil {
		t.Fatalf("scan uuid: %v", err)
	}
	at := time.Date(2026, 9, 15, 10, 30, 0, 0, time.UTC)

	p := toProfile(store.User{
		ID:        id,
		Email:     "someone@example.com",
		CreatedAt: pgtype.Timestamptz{Time: at, Valid: true},
		LastSeen:  pgtype.Timestamptz{Time: at, Valid: true},
	})

	if p.ID != "11111111-1111-1111-1111-111111111111" {
		t.Errorf("id = %q", p.ID)
	}
	if p.CreatedAt != "2026-09-15T10:30:00Z" {
		t.Errorf("created_at = %q, want RFC3339", p.CreatedAt)
	}
	if p.LastSeen != "2026-09-15T10:30:00Z" {
		t.Errorf("last_seen = %q, want RFC3339", p.LastSeen)
	}
}

// A user who has never signed in has a NULL last_seen, which must come back as
// an empty string rather than a zero time.
func TestToProfileLeavesNullLastSeenEmpty(t *testing.T) {
	var id pgtype.UUID
	_ = id.Scan("22222222-2222-2222-2222-222222222222")
	p := toProfile(store.User{
		ID:        id,
		CreatedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true},
	})
	if p.LastSeen != "" {
		t.Errorf("last_seen = %q, want empty for a user who never signed in", p.LastSeen)
	}
}
