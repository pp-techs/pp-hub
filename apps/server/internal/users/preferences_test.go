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
		if got := DecodePreferences(raw).Locale; got != "" {
			t.Errorf("DecodePreferences(%q) locale = %q, want empty", raw, got)
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

func TestWantsShareEmail(t *testing.T) {
	trueVal := true
	falseVal := false

	empty := DecodePreferences([]byte(`{}`))
	if !empty.WantsShareEmail() {
		t.Error("expected WantsShareEmail to be true for empty preferences")
	}

	explicitTrue := DecodePreferences([]byte(`{"email_on_share":true}`))
	if !explicitTrue.WantsShareEmail() {
		t.Error("expected WantsShareEmail to be true for email_on_share=true")
	}

	explicitFalse := DecodePreferences([]byte(`{"email_on_share":false}`))
	if explicitFalse.WantsShareEmail() {
		t.Error("expected WantsShareEmail to be false for email_on_share=false")
	}

	// Merging EmailOnShare keeps unknown key and locale
	stored := []byte(`{"locale":"vi","custom_key":"custom_val"}`)
	merged, err := mergePreferences(stored, PreferencesPatch{EmailOnShare: &falseVal})
	if err != nil {
		t.Fatalf("mergePreferences failed: %v", err)
	}
	var round map[string]any
	if err := json.Unmarshal(merged, &round); err != nil {
		t.Fatalf("unmarshal merged failed: %v", err)
	}
	if round["email_on_share"] != false {
		t.Errorf("expected email_on_share to be false, got %v", round["email_on_share"])
	}
	if round["locale"] != "vi" {
		t.Errorf("expected locale vi preserved, got %v", round["locale"])
	}
	if round["custom_key"] != "custom_val" {
		t.Errorf("expected custom_key preserved, got %v", round["custom_key"])
	}

	// Merging with trueVal
	merged2, err := mergePreferences(merged, PreferencesPatch{EmailOnShare: &trueVal})
	if err != nil {
		t.Fatalf("mergePreferences 2 failed: %v", err)
	}
	p2 := DecodePreferences(merged2)
	if !p2.WantsShareEmail() {
		t.Error("expected WantsShareEmail to be true after updating to true")
	}
	if p2.Locale != "vi" {
		t.Errorf("expected locale vi preserved, got %s", p2.Locale)
	}
}
