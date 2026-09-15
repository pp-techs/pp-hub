package share

import (
	"testing"

	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

func TestPermRank(t *testing.T) {
	tests := []struct {
		perm Perm
		want int
	}{
		{PermView, 1},
		{PermDownload, 2},
		{PermEdit, 3},
		{Perm(""), 0},
	}
	for _, tt := range tests {
		if got := permRank(tt.perm); got != tt.want {
			t.Errorf("permRank(%q) = %d, want %d", tt.perm, got, tt.want)
		}
	}
}

// clampRank guards the join between the two scales: the caller supplies a
// space-role floor, and nothing downstream should ever see a rank outside
// 0..3, whatever it passes.
func TestClampRank(t *testing.T) {
	tests := []struct{ in, want int }{
		{-1, 0}, {0, 0}, {1, 1}, {3, 3}, {4, 3}, {99, 3},
	}
	for _, tt := range tests {
		if got := clampRank(tt.in); got != tt.want {
			t.Errorf("clampRank(%d) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

// A link never carries edit, however the share row was written. Edit-via-link
// is post-MVP, and a link is handed to whoever holds the URL.
func TestLinkPermClampsToDownload(t *testing.T) {
	for _, perm := range []Perm{PermView, PermDownload, PermEdit} {
		rank, err := LinkPerm(store.Share{Kind: store.ShareKindLink, Perm: perm})
		if err != nil {
			t.Fatalf("LinkPerm(%q): %v", perm, err)
		}
		if rank > 2 {
			t.Errorf("LinkPerm(%q) = %d — a link must never grant edit", perm, rank)
		}
	}
	if _, err := LinkPerm(store.Share{Kind: store.ShareKindGroup, Perm: PermEdit}); err == nil {
		t.Error("LinkPerm accepted a group grant; it is only defined for link shares")
	}
}
