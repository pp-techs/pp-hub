package users

import (
	"encoding/json"
	"testing"
)

// The preference document is the one place per-account settings accumulate,
// so the invariants it has to keep are: a patch touches only what it names,
// a key this binary does not know survives the write, a malformed column is
// still usable, and a locale tag can never smuggle a second query parameter
// into the OnlyOffice editor URL it is interpolated into.
func TestPreferencesMergeIsNonDestructive(t *testing.T) {
	// A locale-only patch must leave every other key exactly as it was —
	// including one this binary has no field for, which is what makes the
	// column safe to grow.
	stored := []byte(`{"locale":"en","theme":"dark","futureThing":{"a":1}}`)
	vi := "vi"
	blob, err := mergePreferences(stored, PreferencesPatch{Locale: &vi})
	if err != nil {
		t.Fatal(err)
	}
	var round map[string]any
	if err := json.Unmarshal(blob, &round); err != nil {
		t.Fatal(err)
	}
	if round["locale"] != "vi" {
		t.Fatalf("locale = %v, want vi", round["locale"])
	}
	if round["theme"] != "dark" {
		t.Fatalf("theme = %v, want dark preserved", round["theme"])
	}
	if round["futureThing"] == nil {
		t.Fatal("an unknown key was dropped; a newer client's preference would be erased")
	}

	// An empty patch changes nothing.
	same, err := mergePreferences(stored, PreferencesPatch{})
	if err != nil {
		t.Fatal(err)
	}
	var before, after map[string]any
	_ = json.Unmarshal(stored, &before)
	_ = json.Unmarshal(same, &after)
	if len(before) != len(after) || after["locale"] != "en" {
		t.Fatalf("empty patch altered the document: %s", same)
	}

	// Junk and absence are "never chosen", not errors.
	for _, raw := range [][]byte{nil, {}, []byte(`{`), []byte(`[]`), []byte(`"x"`)} {
		if got := decodePreferences(raw).Locale; got != "" {
			t.Errorf("decodePreferences(%q) locale = %q, want empty", raw, got)
		}
		if _, err := mergePreferences(raw, PreferencesPatch{Locale: &vi}); err != nil {
			t.Errorf("mergePreferences(%q) errored: %v", raw, err)
		}
	}

	for _, tag := range []string{"", "en", "vi", "vi-VN", "zh-Hans"} {
		if !ValidLocale(tag) {
			t.Errorf("ValidLocale(%q) = false, want true", tag)
		}
	}
	// The tag travels into the OnlyOffice editor URL, so anything that could
	// smuggle another query parameter has to be refused.
	for _, tag := range []string{"en&ui=x", "../../etc", "e", "toolongalanguagetag", "en_US", "<script>"} {
		if ValidLocale(tag) {
			t.Errorf("ValidLocale(%q) = true, want false", tag)
		}
	}
}
