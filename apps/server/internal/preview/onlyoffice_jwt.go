package preview

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
)

// Minimal HS256 signing for OnlyOffice's JWT, which is how the Document
// Server tells a request from this deployment apart from a request from
// anyone who can reach its port.
//
// Hand-rolled rather than pulled in as a dependency, and that is only safe
// because this file **signs and never verifies**. Verification is where JWT
// libraries earn their keep — algorithm confusion, `alg: none`, key
// substitution — and none of that applies to producing a fixed-algorithm
// token. If anything here ever needs to *check* a JWT, take the dependency.
//
// The Document Server accepts the token two ways and the docs are not
// consistent about which endpoints honour which, so signOnlyOffice produces
// both: the request body gains a `token` claim over the request itself, and
// the Authorization header carries the same request wrapped in `payload`.
// https://api.onlyoffice.com/docs/docs-api/additional-api/signature/
func signOnlyOfficeJWT(secret []byte, claims any) (string, error) {
	// {"alg":"HS256","typ":"JWT"} — constant, so it is written out rather
	// than marshalled on every call.
	const header = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9"

	body, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	signing := header + "." + base64.RawURLEncoding.EncodeToString(body)
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(signing))
	return signing + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}
