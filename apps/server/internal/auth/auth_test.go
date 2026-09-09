package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nhatbui/pp-hub/apps/server/internal/config"
)

func testService(t *testing.T) *Service {
	t.Helper()
	return &Service{cfg: &config.Config{SessionKey: []byte("test-key-32-bytes-test-key-32byt")}}
}

func testUUID(t *testing.T) pgtype.UUID {
	t.Helper()
	var id pgtype.UUID
	if err := id.Scan("018f3c2e-7b1a-7f2a-9c4d-2e8f1a0b3c4d"); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestSessionRoundTrip(t *testing.T) {
	s := testService(t)
	id := testUUID(t)

	cookie, err := s.signSession(id)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.verifySession(cookie)
	if err != nil {
		t.Fatal(err)
	}
	if got != id {
		t.Fatalf("want %v, got %v", id, got)
	}
}

func TestSessionTampered(t *testing.T) {
	s := testService(t)
	cookie, err := s.signSession(testUUID(t))
	if err != nil {
		t.Fatal(err)
	}
	// Flip a character inside the signature segment.
	parts := strings.Split(cookie, ".")
	sig := []byte(parts[3])
	sig[0] ^= 0xFF
	if _, err := s.verifySession(strings.Join(parts[:3], ".") + "." + string(sig)); err == nil {
		t.Fatal("tampered session must not verify")
	}
}

func TestSessionExpired(t *testing.T) {
	s := testService(t)
	id := testUUID(t)

	// Rebuild a cookie with an expiry in the past, signed with the real key.
	raw, _ := id.Value()
	var expBuf [8]byte
	binary.BigEndian.PutUint64(expBuf[:], uint64(time.Now().Add(-time.Hour).Unix()))
	payload := raw.(string) + "." + base64.RawURLEncoding.EncodeToString(expBuf[:])
	mac := hmac.New(sha256.New, s.cfg.SessionKey)
	mac.Write([]byte(payload))
	cookie := "v1." + payload + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))

	if _, err := s.verifySession(cookie); err == nil {
		t.Fatal("expired session must not verify")
	}
}

func TestSessionWrongKey(t *testing.T) {
	s1 := testService(t)
	s2 := &Service{cfg: &config.Config{SessionKey: []byte("other-key-32-bytes-other-key-32b")}}
	cookie, err := s1.signSession(testUUID(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s2.verifySession(cookie); err == nil {
		t.Fatal("session signed with another key must not verify")
	}
}
