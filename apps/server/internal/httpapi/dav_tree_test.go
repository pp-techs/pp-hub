package httpapi

import (
	"testing"

	"golang.org/x/text/unicode/norm"
)

// Finder sends a name decomposed (NFD) where the listing showed it composed
// (NFC), and the other way round for a name stored decomposed. Either form
// must reach the same entry.
func TestDavResolveIgnoresUnicodeForm(t *testing.T) {
	const name = "Bùi Đức Nhật"
	for _, tc := range []struct{ name, stored, asked string }{
		{"composed stored, decomposed asked", norm.NFC.String(name), norm.NFD.String(name)},
		{"decomposed stored, composed asked", norm.NFD.String(name), norm.NFC.String(name)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := &davEntry{kind: davRoot, path: "/"}
			stored := &davEntry{kind: davShared, path: "/" + tc.stored, name: tc.stored}
			v := &davView{lists: map[string][]*davEntry{"/": {stored}}}

			got, err := v.resolve("/" + tc.asked)
			if err != nil || got != stored {
				t.Fatalf("resolve(%q) = %v, %v; want the entry stored as %q", tc.asked, got, err, tc.stored)
			}
			if got, err := v.child(root, tc.asked); err != nil || got != stored {
				t.Fatalf("child(%q) = %v, %v; want the stored entry", tc.asked, got, err)
			}
		})
	}
}
