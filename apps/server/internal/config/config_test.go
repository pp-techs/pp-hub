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

// Logto is the only identity provider, so a server without every piece of it
// must refuse to start. The alternative is discovering it at the first
// membership change or suspension an admin attempts — the least convenient
// moment, and the hardest to attribute to a missing variable.
func TestLogtoRefusesAnIncompleteTenant(t *testing.T) {
	cases := map[string]LogtoSettings{
		"unset":                        {},
		"endpoint only":                {Endpoint: "https://tenant.logto.app"},
		"endpoint without secret":      {Endpoint: "https://tenant.logto.app", ClientID: "m2m"},
		"credentials without endpoint": {ClientID: "m2m", ClientSecret: "s"},
		"endpoint is not a URL":        {Endpoint: "tenant.logto.app", ClientID: "m2m", ClientSecret: "s"},
	}
	for name, l := range cases {
		if err := l.Validate(); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// A missing webhook key warns but does not refuse: the hourly sweep and the
// next sign-in still converge.
func TestLogtoAcceptsAFullyConfiguredTenantWithoutAWebhookKey(t *testing.T) {
	l := LogtoSettings{
		Endpoint:     "https://tenant.logto.app",
		ClientID:     "m2m",
		ClientSecret: "secret",
		APIResource:  "https://tenant.logto.app/api",
		AdminRole:    "admin",
	}
	if err := l.Validate(); err != nil {
		t.Fatalf("valid tenant rejected: %v", err)
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
