package auth

import (
	"encoding/json"
	"testing"
)

// safeNext is the only thing standing between /auth/login?next=… and an open
// redirect, and the interesting cases are all the ones that look like a path
// without being one.
func TestSafeNext(t *testing.T) {
	cases := map[string]string{
		"/drive/abc":                    "/drive/abc",
		"/drive/abc?dir=x#y":            "/drive/abc?dir=x#y",
		"/":                             "/",
		"":                              "",
		"drive/abc":                     "",
		"//evil.example":                "",
		"//evil.example/drive":          "",
		`/\evil.example`:                "",
		`/\/evil.example`:               "",
		"https://evil.example":          "",
		"http://evil.example/drive":     "",
		"javascript:alert(1)":           "",
		"/drive\r\nSet-Cookie: a=b":     "",
		"/drive\x00":                    "",
		"////evil.example":              "",
		"/redirect?to=https://evil.com": "/redirect?to=https://evil.com",
	}
	for in, want := range cases {
		if got := safeNext(in); got != want {
			t.Errorf("safeNext(%q) = %q, want %q", in, got, want)
		}
	}
}

// A backslash is a forward slash to a browser in the authority position, so
// "/\evil.example" is a protocol-relative URL even though net/url parses it as
// a path. This is the case net/url alone gets wrong.
func TestSafeNextRejectsBackslashAuthority(t *testing.T) {
	if got := safeNext(`/\evil.example/path`); got != "" {
		t.Fatalf("backslash authority accepted as %q", got)
	}
}

// Providers disagree about whether email_verified is a bool or a string, and
// encoding/json failing on the string form would take every other claim in the
// token down with it.
func TestBoolishAcceptsBothSpellings(t *testing.T) {
	cases := map[string]bool{
		`{"email_verified": true}`:    true,
		`{"email_verified": false}`:   false,
		`{"email_verified": "true"}`:  true,
		`{"email_verified": "false"}`: false,
	}
	for raw, want := range cases {
		var c idClaims
		if err := json.Unmarshal([]byte(raw), &c); err != nil {
			t.Fatalf("%s: %v", raw, err)
		}
		if bool(c.EmailVerified) != want {
			t.Errorf("%s: got %v, want %v", raw, bool(c.EmailVerified), want)
		}
	}
}

// An absent claim must read false, not fail: it is how "this provider does not
// say" reaches the account-adoption decision, and adopting on a claim nobody
// made is exactly the hijack the check exists to prevent.
func TestBoolishAbsentIsFalse(t *testing.T) {
	var c idClaims
	if err := json.Unmarshal([]byte(`{"email": "a@b.c"}`), &c); err != nil {
		t.Fatal(err)
	}
	if bool(c.EmailVerified) {
		t.Fatal("absent email_verified read as true")
	}
}

func TestBoolishRejectsNonsense(t *testing.T) {
	var c idClaims
	if err := json.Unmarshal([]byte(`{"email_verified": "yes please"}`), &c); err == nil {
		t.Fatal("unparseable email_verified accepted")
	}
}

// In a split-horizon deployment discovery answers on an internal hostname, so
// the authorization URL it hands back is one the browser cannot reach.
func TestRebaseOrigin(t *testing.T) {
	const internal = "http://logto:3001/oidc"
	const external = "https://id.example.com/oidc"

	got := rebaseOrigin("http://logto:3001/oidc/auth?x=1", internal, external)
	if want := "https://id.example.com/oidc/auth?x=1"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	// Same origin on both sides — the overwhelmingly common case — must be a
	// no-op rather than a reconstruction of the URL.
	if got := rebaseOrigin("http://x/y?a=b", internal, internal); got != "http://x/y?a=b" {
		t.Errorf("identity rebase changed the URL: %q", got)
	}

	// A URL from some third origin is left alone: rebasing it would point the
	// browser somewhere the IdP never advertised.
	if got := rebaseOrigin("https://elsewhere.example/auth", internal, external); got != "https://elsewhere.example/auth" {
		t.Errorf("unrelated origin rewritten: %q", got)
	}

	if got := rebaseOrigin("", internal, external); got != "" {
		t.Errorf("empty URL became %q", got)
	}
}

// The transaction cookie is the state/nonce/verifier triple, and a callback
// cannot validate anything if it does not survive the round trip intact.
func TestOIDCTxRoundTrip(t *testing.T) {
	in := oidcTx{State: "s-value", Nonce: "n-value", Verifier: "v-value", Next: "/drive/x"}
	blob, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out oidcTx
	if err := json.Unmarshal(blob, &out); err != nil {
		t.Fatal(err)
	}
	if out != in {
		t.Fatalf("got %+v, want %+v", out, in)
	}
}
