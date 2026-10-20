package publish

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

// tokenAAD binds a sealed token to its column: a ciphertext copied into
// another sealed field under the same key would not open.
const tokenAAD = "publication_token"

// seal encrypts a token with AES-GCM, as sysconfig seals the SMTP password.
func seal(key []byte, plaintext string) (string, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(gcm.Seal(nonce, nonce, []byte(plaintext), []byte(tokenAAD))), nil
}

func open(key []byte, sealed string) (string, error) {
	data, err := base64.StdEncoding.DecodeString(sealed)
	if err != nil {
		return "", err
	}
	gcm, err := newGCM(key)
	if err != nil {
		return "", err
	}
	if len(data) < gcm.NonceSize() {
		return "", errors.New("sealed token too short")
	}
	plain, err := gcm.Open(nil, data[:gcm.NonceSize()], data[gcm.NonceSize():], []byte(tokenAAD))
	if err != nil {
		return "", fmt.Errorf("open publication token: %w", err)
	}
	return string(plain), nil
}

func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// Passwords: PBKDF2-HMAC-SHA256 at OWASP's iteration count, stored with
// its parameters so they can be raised without invalidating old rows.
const (
	pbkdf2Iterations = 600_000
	pbkdf2KeyLen     = 32
	passwordScheme   = "pbkdf2-sha256"
)

func hashPassword(pw string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	dk, err := pbkdf2.Key(sha256.New, pw, salt, pbkdf2Iterations, pbkdf2KeyLen)
	if err != nil {
		return "", err
	}
	enc := base64.RawStdEncoding
	return passwordScheme + "$" + strconv.Itoa(pbkdf2Iterations) + "$" + enc.EncodeToString(salt) + "$" + enc.EncodeToString(dk), nil
}

// checkPassword compares in constant time; a malformed stored hash fails.
func checkPassword(stored, pw string) bool {
	parts := strings.Split(stored, "$")
	if len(parts) != 4 || parts[0] != passwordScheme {
		return false
	}
	iter, err := strconv.Atoi(parts[1])
	if err != nil || iter < 1 {
		return false
	}
	enc := base64.RawStdEncoding
	salt, err1 := enc.DecodeString(parts[2])
	want, err2 := enc.DecodeString(parts[3])
	if err1 != nil || err2 != nil || len(want) == 0 {
		return false
	}
	got, err := pbkdf2.Key(sha256.New, pw, salt, iter, len(want))
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(got, want) == 1
}

// GrantTTL is how long an unlock lasts.
const GrantTTL = 12 * time.Hour

// A grant is base64url(pub_id[16] | rev[4] | exp[8] | HMAC-SHA256[32]). It
// lives in the artifact path, not a cookie: a sandboxed frame has an opaque
// origin, and relative URLs keep the prefix.
const grantBody = 16 + 4 + 8

func (s *Service) mintGrant(pub store.Publication, exp time.Time) string {
	buf := make([]byte, grantBody, grantBody+sha256.Size)
	copy(buf[:16], pub.ID.Bytes[:])
	binary.BigEndian.PutUint32(buf[16:20], uint32(pub.Rev))
	binary.BigEndian.PutUint64(buf[20:28], uint64(exp.Unix()))
	mac := hmac.New(sha256.New, s.grantKey)
	mac.Write(buf)
	return base64.RawURLEncoding.EncodeToString(mac.Sum(buf))
}

// CheckGrant reports whether grant was minted for this publication at its
// current rev and has not expired. A republish bumps rev and so ends every
// grant.
func (s *Service) CheckGrant(pub store.Publication, grant string) bool {
	raw, err := base64.RawURLEncoding.DecodeString(grant)
	if err != nil || len(raw) != grantBody+sha256.Size {
		return false
	}
	mac := hmac.New(sha256.New, s.grantKey)
	mac.Write(raw[:grantBody])
	if !hmac.Equal(mac.Sum(nil), raw[grantBody:]) {
		return false
	}
	var id pgtype.UUID
	copy(id.Bytes[:], raw[:16])
	id.Valid = true
	if id != pub.ID || binary.BigEndian.Uint32(raw[16:20]) != uint32(pub.Rev) {
		return false
	}
	exp := time.Unix(int64(binary.BigEndian.Uint64(raw[20:28])), 0)
	return s.now().Before(exp)
}

// CleanPath turns the escaped path after /<token>/[~<grant>/] into a
// snapshot path. It is percent-decoded exactly once and refused (ok false)
// when it could name anything outside the snapshot: a `..` segment, a
// backslash, a NUL, or an empty segment (`//`, which some resolvers read as
// an absolute path). `.` segments are dropped. dir reports a path that ends
// in `/` (or is empty): its index.html is what is served.
func CleanPath(escaped string) (p string, dir bool, ok bool) {
	raw, err := url.PathUnescape(escaped)
	if err != nil {
		return "", false, false
	}
	return CleanRelPath(raw)
}

// CleanRelPath is CleanPath for a path that is already decoded — one an
// agent names a file by.
func CleanRelPath(raw string) (p string, dir bool, ok bool) {
	if strings.ContainsAny(raw, "\\\x00") {
		return "", false, false
	}
	if raw == "" {
		return "", true, true
	}
	dir = strings.HasSuffix(raw, "/")
	raw = strings.TrimSuffix(raw, "/")
	var out []string
	for _, seg := range strings.Split(raw, "/") {
		switch seg {
		case "", "..":
			return "", false, false
		case ".":
			continue
		}
		out = append(out, seg)
	}
	if len(out) == 0 {
		return "", true, true
	}
	return strings.Join(out, "/"), dir, true
}

func urlPathEscape(seg string) string { return url.PathEscape(seg) }
