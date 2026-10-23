package office

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

// The Document Server is the only consumer and it answers a malformed token
// with the same opaque `-8` as a wrong secret, so a padding or header mistake
// here would surface in production as "office thumbnails stopped working"
// with nothing to go on. This asserts the wire shape a verifier will check.
func TestSignJWTIsVerifiable(t *testing.T) {
	secret := []byte("a-shared-onlyoffice-secret-value")
	claims := map[string]string{"c": "forcesave", "key": "wopi_cachekey"}

	token, err := SignJWT(secret, claims)
	if err != nil {
		t.Fatal(err)
	}

	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("token has %d segments, want 3: %q", len(parts), token)
	}
	// Unpadded base64url throughout: a JWT with '=' or '+' is rejected by
	// every verifier, and encoding/base64's default alphabet produces both.
	if strings.ContainsAny(token, "=+/") {
		t.Errorf("token is not base64url-unpadded: %q", token)
	}

	var header struct {
		Alg string `json:"alg"`
		Typ string `json:"typ"`
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		t.Fatalf("decode header: %v", err)
	}
	if err := json.Unmarshal(raw, &header); err != nil {
		t.Fatalf("header is not JSON: %v", err)
	}
	if header.Alg != "HS256" || header.Typ != "JWT" {
		t.Errorf("header = %+v, want HS256/JWT", header)
	}

	body, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	var round map[string]string
	if err := json.Unmarshal(body, &round); err != nil {
		t.Fatalf("payload is not the claims: %v", err)
	}
	if round["c"] != claims["c"] || round["key"] != claims["key"] {
		t.Errorf("payload = %+v, want the signed claims", round)
	}

	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(parts[0] + "." + parts[1]))
	want := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if parts[2] != want {
		t.Errorf("signature does not cover header.payload")
	}
}

// A different secret must produce a different signature — otherwise the
// Document Server would accept a token this deployment did not mint.
func TestSignJWTBindsTheSecret(t *testing.T) {
	claims := map[string]string{"url": "http://host/doc.docx"}
	a, err := SignJWT([]byte("secret-one-is-thirty-two-bytes!!"), claims)
	if err != nil {
		t.Fatal(err)
	}
	b, err := SignJWT([]byte("secret-two-is-thirty-two-bytes!!"), claims)
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatal("two secrets produced the same token")
	}
}
