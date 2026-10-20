package publish

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

// Artifact paths are attacker-chosen. Anything that could name a file
// outside the snapshot is refused rather than cleaned into something else.
func TestCleanPath(t *testing.T) {
	for _, tc := range []struct {
		in      string
		want    string
		dir, ok bool
	}{
		{"", "", true, true},
		{"index.html", "index.html", false, true},
		{"css/app.css", "css/app.css", false, true},
		{"docs/", "docs", true, true},
		{"./a.js", "a.js", false, true},
		{"a%20b.html", "a b.html", false, true},
		{"%252e%252e/x", "%2e%2e/x", false, true}, // decoded once: a literal name
		{"..", "", false, false},
		{"../secret", "", false, false},
		{"a/../../b", "", false, false},
		{"%2e%2e/x", "", false, false},
		{"a/%2E%2E/b", "", false, false},
		{"/etc/passwd", "", false, false},
		{"a//b", "", false, false},
		{"a\\b", "", false, false},
		{"a%5c..%5cb", "", false, false},
		{"a%00.html", "", false, false},
		{"bad%zz", "", false, false},
	} {
		got, dir, ok := CleanPath(tc.in)
		if ok != tc.ok || (ok && (got != tc.want || dir != tc.dir)) {
			t.Errorf("CleanPath(%q) = %q, %v, %v; want %q, %v, %v", tc.in, got, dir, ok, tc.want, tc.dir, tc.ok)
		}
	}
}

func TestPasswordHash(t *testing.T) {
	h, err := hashPassword("correct horse")
	if err != nil {
		t.Fatal(err)
	}
	if !checkPassword(h, "correct horse") {
		t.Fatal("the right password was refused")
	}
	if checkPassword(h, "correct hors") || checkPassword(h, "") || checkPassword("garbage", "correct horse") {
		t.Fatal("a wrong password or a malformed hash passed")
	}
}

// A grant opens one publication at one rev until it expires.
func TestGrantBindsPublicationAndRev(t *testing.T) {
	now := time.Now()
	s := &Service{grantKey: []byte("k"), now: func() time.Time { return now }}
	pub := store.Publication{ID: pgtype.UUID{Bytes: [16]byte{1}, Valid: true}, Rev: 3}
	g := s.mintGrant(pub, now.Add(GrantTTL))
	if !s.CheckGrant(pub, g) {
		t.Fatal("a fresh grant was refused")
	}
	republished := pub
	republished.Rev = 4
	other := pub
	other.ID = pgtype.UUID{Bytes: [16]byte{2}, Valid: true}
	if s.CheckGrant(republished, g) || s.CheckGrant(other, g) {
		t.Fatal("a grant opened another rev or another publication")
	}
	if (&Service{grantKey: []byte("other"), now: s.now}).CheckGrant(pub, g) {
		t.Fatal("a grant verified under another key")
	}
	later := &Service{grantKey: s.grantKey, now: func() time.Time { return now.Add(GrantTTL + time.Second) }}
	if later.CheckGrant(pub, g) {
		t.Fatal("an expired grant was accepted")
	}
	if s.CheckGrant(pub, g[:len(g)-2]+"AA") {
		t.Fatal("a tampered grant was accepted")
	}
}

func TestSealedTokenRoundTrips(t *testing.T) {
	key := make([]byte, 32)
	sealed, err := seal(key, "abc123")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := open(key, sealed); err != nil || got != "abc123" {
		t.Fatalf("open = %q, %v", got, err)
	}
	other := make([]byte, 32)
	other[0] = 1
	if _, err := open(other, sealed); err == nil {
		t.Fatal("opened under another key")
	}
}
