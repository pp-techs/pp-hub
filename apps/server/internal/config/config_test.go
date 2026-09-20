package config

import (
	"testing"

	"github.com/nhatbui/pp-hub/apps/server/internal/mail"
)

// A ceiling an operator typed wrongly must stop the server, not silently
// become no ceiling. "5GB" is the plausible typo, and treating it as "5" — or
// as unlimited — is how a disk fills.
func TestBytesEnvRejectsNonsense(t *testing.T) {
	for _, bad := range []string{"5GB", "lots", "-1", "5 G", "1e6", ""} {
		if bad == "" {
			continue // empty means "use the default", which is not a typo
		}
		t.Setenv("PP_TEST_BYTES", bad)
		if _, err := bytesEnv("PP_TEST_BYTES", 99); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestBytesEnvParsesSuffixes(t *testing.T) {
	cases := map[string]int64{
		"1024": 1024,
		"1K":   1 << 10,
		"2k":   2 << 10,
		"5M":   5 << 20,
		"5G":   5 << 30,
		"1T":   1 << 40,
		"0":    0,
	}
	for in, want := range cases {
		t.Setenv("PP_TEST_BYTES", in)
		got, err := bytesEnv("PP_TEST_BYTES", -1)
		if err != nil {
			t.Errorf("%q: %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("%q = %d, want %d", in, got, want)
		}
	}
}

func TestBytesEnvUnsetUsesDefault(t *testing.T) {
	t.Setenv("PP_TEST_BYTES", "")
	got, err := bytesEnv("PP_TEST_BYTES", 4242)
	if err != nil || got != 4242 {
		t.Fatalf("got %d, %v; want the default", got, err)
	}
}

// DeriveKey is what keeps one leaked signing key from being every signing
// key. Two purposes must not collide, and the same purpose must be stable
// across processes — the worker and the server derive the WOPI key
// independently and have to agree.
func TestDeriveKeyIsPurposeBoundAndStable(t *testing.T) {
	c := &Config{SecretKey: []byte("master-secret-of-thirty-two-byte")}

	wopi := c.DeriveKey("wopi-token-v1")
	again := c.DeriveKey("wopi-token-v1")
	other := c.DeriveKey("something-else-v1")

	if len(wopi) != 32 {
		t.Errorf("subkey is %d bytes, want 32", len(wopi))
	}
	if string(wopi) != string(again) {
		t.Error("the same purpose derived two different keys; the worker and the server would disagree")
	}
	if string(wopi) == string(other) {
		t.Error("two purposes derived the same key, so a token for one verifies as the other")
	}
	if string(wopi) == string(c.SecretKey) {
		t.Error("the subkey is the master secret")
	}
}

// A different master must give a different subkey, or rotating the secret
// would not rotate anything.
func TestDeriveKeyFollowsTheMaster(t *testing.T) {
	a := (&Config{SecretKey: []byte("master-secret-of-thirty-two-byte")}).DeriveKey("p")
	b := (&Config{SecretKey: []byte("another-secret-of-thirty-two-byt")}).DeriveKey("p")
	if string(a) == string(b) {
		t.Fatal("two masters derived the same subkey")
	}
}

// A half-configured transport must fail startup rather than silently not
// sending: the operator who set PP_SMTP_HOST believes mail works.
func TestValidateSMTPRejectsAHalfConfiguredTransport(t *testing.T) {
	cases := map[string]mail.Settings{
		"host without from": {Host: "smtp.example.com", Port: 587, TLS: "starttls"},
		"bad TLS mode":      {Host: "smtp.example.com", Port: 587, From: "pp-hub@example.com", TLS: "sslv3"},
		"bad port":          {Host: "smtp.example.com", Port: 0, From: "pp-hub@example.com", TLS: "starttls"},
	}
	for name, s := range cases {
		c := &Config{SMTP: s}
		if err := c.validateSMTP(); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// Disabled (Host == "") is a supported state, not an error: the invitation
// handler falls back to returning the URL, which is the whole product before
// SMTP existed.
func TestValidateSMTPAcceptsDisabled(t *testing.T) {
	c := &Config{}
	if err := c.validateSMTP(); err != nil {
		t.Fatalf("disabled SMTP rejected: %v", err)
	}
}

// A fully-configured transport must pass — this is the shape Load() builds
// when every PP_SMTP_* variable is set.
func TestValidateSMTPAcceptsAFullyConfiguredTransport(t *testing.T) {
	c := &Config{SMTP: mail.Settings{
		Host: "smtp.example.com", Port: 587, From: "pp-hub@example.com", TLS: "starttls",
	}}
	if err := c.validateSMTP(); err != nil {
		t.Fatalf("valid SMTP rejected: %v", err)
	}
}
