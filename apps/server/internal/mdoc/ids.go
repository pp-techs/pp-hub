package mdoc

import "crypto/rand"

// idAlphabet and idLength are slash-editor's generateBlockId: 12 characters,
// each a random byte mod 64 into this alphabet.
const (
	idAlphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz-_"
	idLength   = 12
)

func newBlockID() string {
	var b [idLength]byte
	_, _ = rand.Read(b[:]) // crypto/rand.Read never fails (Go 1.24+).
	for i, c := range b {
		b[i] = idAlphabet[c%64]
	}
	return string(b[:])
}

// assignIDs gives every id-bearing node under n (n included) a fresh id.
func assignIDs(n *Node) {
	if hasIDAttr(n.Type) {
		if n.Attrs == nil {
			n.Attrs = map[string]any{}
		}
		n.Attrs["id"] = newBlockID()
	}
	for i := range n.Content {
		assignIDs(&n.Content[i])
	}
}
