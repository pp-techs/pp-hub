package config

import (
	"testing"
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

// A half-configured tenant must fail startup. The alternative is discovering
// it at the first membership change an admin attempts — the least convenient
// moment, and the hardest to attribute to a missing variable.
func TestLogtoRejectsAHalfConfiguredTenant(t *testing.T) {
	cases := map[string]LogtoSettings{
		"credentials without endpoint": {ClientID: "m2m", ClientSecret: "s"},
		"endpoint without secret":      {Endpoint: "https://tenant.logto.app", ClientID: "m2m"},
		"endpoint is not a URL":        {Endpoint: "tenant.logto.app", ClientID: "m2m", ClientSecret: "s"},
		// A signing key with nothing to act through: the delivery would be
		// verified and then dropped, which reads as working and is not.
		"webhook key without credentials": {Endpoint: "https://tenant.logto.app", WebhookKey: "k"},
	}
	for name, l := range cases {
		if err := l.validate(); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// An endpoint with no credentials is a real mode, not a mistake: the sign-in
// path reconciles from the ID token's organization claims and makes no
// outbound call, so organizations can own membership before anybody has
// created a machine-to-machine application.
func TestLogtoAcceptsAnEndpointWithoutCredentials(t *testing.T) {
	l := LogtoSettings{Endpoint: "http://localhost:3001", AdminRole: "admin"}
	if err := l.validate(); err != nil {
		t.Fatalf("claim-only tenant rejected: %v", err)
	}
	if !l.Configured() {
		t.Error("an endpoint alone does not make organizations authoritative")
	}
	if l.Enabled() {
		t.Error("the Management API reports itself reachable with no credentials")
	}
}

// Unset is a supported state, not an error: an install with no Logto tenant
// still serves files, it simply changes no memberships.
func TestLogtoAcceptsUnset(t *testing.T) {
	var l LogtoSettings
	if err := l.validate(); err != nil {
		t.Fatalf("unset tenant rejected: %v", err)
	}
	if l.Configured() || l.Enabled() {
		t.Error("an unset tenant reports itself configured or enabled")
	}
}

// The shape Load() builds when every PP_LOGTO_* variable is set. A missing
// webhook key warns but does not refuse: the hourly sweep still converges.
func TestLogtoAcceptsAFullyConfiguredTenant(t *testing.T) {
	l := LogtoSettings{
		Endpoint:     "https://tenant.logto.app",
		ClientID:     "m2m",
		ClientSecret: "secret",
		APIResource:  "https://tenant.logto.app/api",
		AdminRole:    "admin",
	}
	if err := l.validate(); err != nil {
		t.Fatalf("valid tenant rejected: %v", err)
	}
	if !l.Enabled() {
		t.Error("a fully configured tenant reports itself disabled")
	}
}

func TestSMTPRejectsAHalfConfiguredRelay(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
	}{
		{
			name: "host without from",
			env: map[string]string{
				"PP_SMTP_HOST": "smtp.example.com",
				"PP_SMTP_FROM": "",
			},
		},
		{
			name: "bad tls",
			env: map[string]string{
				"PP_SMTP_HOST": "smtp.example.com",
				"PP_SMTP_FROM": "noreply@example.com",
				"PP_SMTP_TLS":  "bad",
			},
		},
		{
			name: "port abc",
			env: map[string]string{
				"PP_SMTP_PORT": "abc",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			if _, err := Load(); err == nil {
				t.Errorf("%s: accepted half-configured relay", tc.name)
			}
		})
	}
}

func TestSMTPAcceptsUnset(t *testing.T) {
	t.Setenv("PP_SMTP_HOST", "")
	t.Setenv("PP_SMTP_PORT", "587")
	c, err := Load()
	if err != nil {
		t.Fatalf("Load failed with unset SMTP: %v", err)
	}
	if c.SMTP.Enabled() {
		t.Error("expected SMTP to be disabled when host is unset")
	}
}
