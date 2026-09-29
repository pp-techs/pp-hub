package share

import (
	"strings"
	"testing"
)

func TestCapsHas(t *testing.T) {
	cases := []struct {
		caps Caps
		cap  Cap
		want bool
	}{
		{Caps{View: true}, CapView, true},
		{Caps{View: true}, CapDownload, false},
		{Caps{View: true, Download: true}, CapDownload, true},
		{Caps{View: true, Download: true, Share: true}, CapShare, true},
		{Caps{}, CapView, false},
	}
	for _, tt := range cases {
		if got := tt.caps.Has(tt.cap); got != tt.want {
			t.Errorf("caps.Has(%v) = %v, want %v", tt.cap, got, tt.want)
		}
	}
}

func TestCapsOwner(t *testing.T) {
	if !CapsOwner.Has(CapView) {
		t.Error("CapsOwner should have CapView")
	}
	if !CapsOwner.Has(CapDownload) {
		t.Error("CapsOwner should have CapDownload")
	}
	if !CapsOwner.Has(CapEdit) {
		t.Error("CapsOwner should have CapEdit")
	}
	if !CapsOwner.Has(CapShare) {
		t.Error("CapsOwner should have CapShare")
	}
}

func TestGrantCapsNormalized(t *testing.T) {
	cases := []struct {
		in   GrantCaps
		want GrantCaps
	}{
		{GrantCaps{Level: LevelEdit, CanDownload: false, CanShare: false},
			GrantCaps{Level: LevelEdit, CanDownload: true, CanShare: false}},
		{GrantCaps{Level: LevelEdit, CanDownload: true, CanShare: true},
			GrantCaps{Level: LevelEdit, CanDownload: true, CanShare: true}},
		{GrantCaps{Level: LevelView, CanDownload: false, CanShare: false},
			GrantCaps{Level: LevelView, CanDownload: false, CanShare: false}},
		{GrantCaps{Level: LevelView, CanDownload: true, CanShare: false},
			GrantCaps{Level: LevelView, CanDownload: true, CanShare: false}},
	}
	for _, tt := range cases {
		if got := tt.in.Normalized(); got != tt.want {
			t.Errorf("GrantCaps(%+v).Normalized() = %+v, want %+v", tt.in, got, tt.want)
		}
	}
}

func TestNewLinkTokenShape(t *testing.T) {
	seen := map[string]bool{}
	for range 200 {
		token, err := newLinkToken()
		if err != nil {
			t.Fatalf("newLinkToken: %v", err)
		}
		if len(token) != linkTokenLength {
			t.Fatalf("token %q is %d characters, want %d", token, len(token), linkTokenLength)
		}
		for _, c := range token {
			if !strings.ContainsRune(linkTokenAlphabet, c) {
				t.Fatalf("token %q holds %q, outside base62", token, c)
			}
		}
		seen[token] = true
	}
	// A generator that returned one value, or a short alphabet, would pass
	// everything above; 200 draws repeating is the failure this catches.
	if len(seen) != 200 {
		t.Errorf("200 draws produced %d distinct tokens, want 200", len(seen))
	}
}

func TestCapsCovers(t *testing.T) {
	cases := []struct {
		caps Caps
		want GrantCaps
		ok   bool
	}{
		{CapsOwner, GrantCaps{Level: LevelEdit, CanDownload: true, CanShare: true}, true},
		{Caps{View: true}, GrantCaps{Level: LevelEdit, CanDownload: true, CanShare: true}, false},
		{Caps{View: true}, GrantCaps{Level: LevelView, CanDownload: false, CanShare: false}, true},
		{Caps{View: true, Download: true}, GrantCaps{Level: LevelView, CanDownload: true, CanShare: false}, true},
		{Caps{View: true, Download: true}, GrantCaps{Level: LevelView, CanDownload: false, CanShare: false}, true},
		{Caps{View: true, Download: true, Edit: true, Share: true}, GrantCaps{Level: LevelEdit, CanDownload: true, CanShare: true}, true},
		// Download is required but not held → denied
		{Caps{View: true, Edit: true, Share: true}, GrantCaps{Level: LevelEdit, CanDownload: true, CanShare: true}, false},
	}
	for _, tt := range cases {
		if got := tt.caps.Covers(tt.want); got != tt.ok {
			t.Errorf("Caps(%+v).Covers(%+v) = %v, want %v", tt.caps, tt.want, got, tt.ok)
		}
	}
}
