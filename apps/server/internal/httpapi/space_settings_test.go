package httpapi

import "testing"

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
