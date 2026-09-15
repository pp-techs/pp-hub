package httpapi

import (
	"strings"
	"testing"
)

// The slug appears in URLs, so it accepts only what a URL can carry without
// escaping, and cannot start or end with a dash.
func TestSlugPattern(t *testing.T) {
	valid := []string{"marketing", "q1-2026", "a", "a-b-c", "team42"}
	for _, s := range valid {
		if !slugPattern.MatchString(s) {
			t.Errorf("%q should be a valid slug", s)
		}
	}

	invalid := []string{
		"", "-lead", "trail-", "Marketing", "with space", "under_score",
		"double--dash", "emoji🙂", "slash/es", "dot.ted",
	}
	for _, s := range invalid {
		if slugPattern.MatchString(s) {
			t.Errorf("%q should be rejected as a slug", s)
		}
	}
}

// Only the hash is stored, so a database leak cannot be replayed into
// memberships. The token itself must be long enough not to be guessed.
func TestInviteTokenIsHashedNotStored(t *testing.T) {
	token, hash, err := newInviteToken()
	if err != nil {
		t.Fatalf("newInviteToken: %v", err)
	}
	if len(token) != 64 {
		t.Errorf("token length = %d, want 64 hex chars (32 bytes)", len(token))
	}
	if hash == token {
		t.Error("the stored hash must not equal the token handed out")
	}
	if hash != hashToken(token) {
		t.Error("hashToken must reproduce the stored hash")
	}

	// Two calls must not collide.
	other, otherHash, _ := newInviteToken()
	if token == other || hash == otherHash {
		t.Error("tokens must be unique per call")
	}
}

func TestHashTokenIsStable(t *testing.T) {
	const token = "abc123"
	first := hashToken(token)
	if first != hashToken(token) {
		t.Error("hashToken must be deterministic")
	}
	if len(first) != 64 {
		t.Errorf("hash length = %d, want 64 hex chars (sha256)", len(first))
	}
	if strings.Contains(first, token) {
		t.Error("the hash must not contain the plaintext")
	}
}
