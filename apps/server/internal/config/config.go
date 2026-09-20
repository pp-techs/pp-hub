package config

import (
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"log/slog"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Addr        string
	DatabaseURL string
	// OIDCIssuer is where *this process* fetches the discovery document and
	// calls the token, userinfo and JWKS endpoints. OIDCIssuerExt is the
	// issuer the browser is sent to and the value that must appear in the
	// `iss` claim. They differ only in a split-horizon deployment, where the
	// IdP answers on a container hostname internally and a public one
	// externally; otherwise the external one defaults to this.
	OIDCIssuer       string
	OIDCIssuerExt    string
	OIDCClientID     string
	OIDCClientSecret string
	ExternalURL      string // public base URL of this server
	// SecretKey is the master secret. Nothing signs with it directly — every
	// consumer takes a purpose-bound subkey through DeriveKey, so a token
	// minted for one purpose can never verify as another and the two can be
	// reasoned about separately. Sessions no longer use it at all: they are
	// rows in `sessions`, so rotating this key does not sign anyone out.
	SecretKey []byte
	// SecretKeyShared is false when the key was generated at startup rather
	// than supplied. Anything that signs a token another process must verify
	// — the worker's office conversion fetch — has to refuse in that case.
	SecretKeyShared  bool
	BlobRoot         string // local CAS blob directory
	BlobBackend      string // "local" | "s3"
	S3               S3Settings
	OnlyOfficeURL    string // OnlyOffice Document Server base URL (browser-facing)
	OnlyOfficeIntURL string // OnlyOffice as reachable from the server/worker (container network)
	WopiServerURL    string // base URL OnlyOffice uses to call this server (container network)
	// OnlyOfficeJWTSecret is the Document Server's JWT_SECRET. It is its own
	// variable rather than a DeriveKey subkey because the other holder is a
	// third-party container configured from compose, which cannot run our
	// derivation. Empty means the Document Server is unsigned: anything that
	// can reach its port can drive it, including asking it to fetch an
	// arbitrary URL.
	OnlyOfficeJWTSecret []byte
	// MaxUploadBytes caps a single request body that becomes file content.
	// A space's quota is checked only after the bytes have landed — the
	// request's declared length is not trustworthy — so without a ceiling
	// here one caller could fill the disk, and a space's quota defaults to
	// unlimited. 0 disables it.
	MaxUploadBytes int64
	// BootstrapAdminEmail promotes whoever signs in with this address to
	// instance admin, every time they sign in. It exists because there was
	// no other way to create the first one: `users.is_admin` defaults to
	// false and its only writer sits behind an admin-only endpoint, so a
	// fresh install had no administrator and no way to appoint one short of
	// `UPDATE users SET is_admin = true` in psql. An install step that
	// requires a psql session is not an install step.
	//
	// Lowercased on load; comparison is case-insensitive, because an IdP is
	// free to return a different case than the operator typed.
	BootstrapAdminEmail string
	// AuditRetention is how long security_events rows are kept. One row is
	// written on every sign-in and every failed attempt, so "forever" is a
	// slow leak and, eventually, a console nobody can read.
	AuditRetention time.Duration
	// TrustedProxies are the CIDRs whose X-Forwarded-For this server will
	// believe. Empty — the default, and correct for a server reached
	// directly — means no forwarded header is trusted at all and the TCP
	// peer is the client address.
	//
	// This is not optional bookkeeping. The per-IP rate limits and the
	// address recorded on a session are only worth anything if the address
	// cannot be chosen by the caller, and behind a reverse proxy with this
	// unset every request appears to come from the proxy, so one caller can
	// spend the whole instance's budget.
	TrustedProxies []string
}

func Load() (*Config, error) {
	key, shared, err := secretKey()
	if err != nil {
		return nil, err
	}
	maxUpload, err := bytesEnv("PP_MAX_UPLOAD_BYTES", 5<<30)
	if err != nil {
		return nil, err
	}
	c := &Config{
		Addr:             env("PP_ADDR", ":8080"),
		DatabaseURL:      env("PP_DATABASE_URL", "postgres://pphub:pphub@localhost:5432/pphub?sslmode=disable"),
		OIDCIssuer:       env("PP_OIDC_ISSUER", "http://localhost:9080"),
		OIDCIssuerExt:    os.Getenv("PP_OIDC_ISSUER_EXTERNAL"),
		OIDCClientID:     env("PP_OIDC_CLIENT_ID", "pphub-server"),
		OIDCClientSecret: env("PP_OIDC_CLIENT_SECRET", "pphub-server-secret"),
		ExternalURL:      env("PP_EXTERNAL_URL", "http://localhost:8080"),
		SecretKey:        key,
		SecretKeyShared:  shared,
		BlobRoot:         env("PP_BLOB_ROOT", "./data/blobs"),
		BlobBackend:      env("PP_BLOB_BACKEND", "local"),
		S3: S3Settings{
			Bucket:    os.Getenv("PP_S3_BUCKET"),
			Region:    os.Getenv("PP_S3_REGION"),
			Endpoint:  os.Getenv("PP_S3_ENDPOINT"),
			AccessKey: os.Getenv("PP_S3_ACCESS_KEY_ID"),
			SecretKey: os.Getenv("PP_S3_SECRET_ACCESS_KEY"),
			PathStyle: env("PP_S3_PATH_STYLE", "1") == "1",
		},
		OnlyOfficeURL:    env("PP_ONLYOFFICE_URL", "http://localhost:9980"),
		OnlyOfficeIntURL: os.Getenv("PP_ONLYOFFICE_INTERNAL_URL"),
		WopiServerURL:    env("PP_WOPI_SERVER_URL", "http://localhost:8080"),

		OnlyOfficeJWTSecret: []byte(os.Getenv("PP_ONLYOFFICE_JWT_SECRET")),
		MaxUploadBytes:      maxUpload,
		BootstrapAdminEmail: strings.ToLower(strings.TrimSpace(os.Getenv("PP_BOOTSTRAP_ADMIN_EMAIL"))),
		AuditRetention:      time.Duration(daysEnv("PP_AUDIT_RETENTION_DAYS", 365)) * 24 * time.Hour,
	}
	// Validated here rather than at the router, because chi's
	// ClientIPFromXFF panics on a bad prefix at startup and a typo in an
	// operator's CIDR list deserves a sentence, not a stack trace.
	for _, raw := range strings.Split(os.Getenv("PP_TRUSTED_PROXIES"), ",") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		// A bare address is the single-host form of a prefix, which is what
		// an operator naming one proxy means.
		if !strings.Contains(raw, "/") {
			addr, err := netip.ParseAddr(raw)
			if err != nil {
				return nil, fmt.Errorf("PP_TRUSTED_PROXIES: %q is neither an address nor a CIDR", raw)
			}
			raw = netip.PrefixFrom(addr, addr.BitLen()).String()
		} else if _, err := netip.ParsePrefix(raw); err != nil {
			return nil, fmt.Errorf("PP_TRUSTED_PROXIES: %q is not a CIDR", raw)
		}
		c.TrustedProxies = append(c.TrustedProxies, raw)
	}
	if c.OIDCIssuerExt == "" {
		c.OIDCIssuerExt = c.OIDCIssuer
	}
	if c.OnlyOfficeIntURL == "" {
		// In a single-host dev setup the browser-facing URL works for
		// server-to-server calls too. In compose they differ: the browser
		// reaches OnlyOffice on localhost:9980, the worker on http://onlyoffice.
		c.OnlyOfficeIntURL = c.OnlyOfficeURL
	}
	if len(c.OnlyOfficeJWTSecret) == 0 {
		slog.Warn("PP_ONLYOFFICE_JWT_SECRET unset: the Document Server is unsigned",
			"effect", "anything that can reach its port can drive it, including asking it to fetch an arbitrary URL")
	}
	if err := c.validateBlobBackend(); err != nil {
		return nil, err
	}
	return c, nil
}

// S3Settings configures the S3-compatible blob backend.
type S3Settings struct {
	Bucket    string
	Region    string
	Endpoint  string
	AccessKey string
	SecretKey string
	// PathStyle addresses buckets as <endpoint>/<bucket>/<key>. Defaults on,
	// because the self-host target is MinIO; set PP_S3_PATH_STYLE=0 for AWS.
	PathStyle bool
}

// validateBlobBackend fails fast on a half-configured backend.
//
// Silently falling back to local disk would be the worst outcome: the server
// would start, accept uploads, and write them somewhere the operator believes
// is empty — with no error until a restart loses them.
func (c *Config) validateBlobBackend() error {
	switch c.BlobBackend {
	case "local":
		return nil
	case "s3":
		if c.S3.Bucket == "" {
			return fmt.Errorf("PP_BLOB_BACKEND=s3 requires PP_S3_BUCKET")
		}
		return nil
	default:
		return fmt.Errorf("PP_BLOB_BACKEND must be \"local\" or \"s3\", got %q", c.BlobBackend)
	}
}

// daysEnv reads a day count. Unlike bytesEnv this falls back rather than
// erroring: a retention window is a policy knob, and refusing to start over a
// typo in it would be a worse outcome than keeping the default and saying so.
func daysEnv(key string, def int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return def
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		slog.Warn("ignoring unusable value", "key", key, "value", raw, "using", def)
		return def
	}
	return n
}

// bytesEnv reads a byte count, accepting a plain number or a K/M/G/T suffix.
//
// A wrong value is a startup error rather than a silent default: an operator
// who typed "5GB" meaning a ceiling should not get "no ceiling" for the
// trouble.
func bytesEnv(key string, def int64) (int64, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return def, nil
	}
	mult := int64(1)
	switch unit := raw[len(raw)-1]; unit {
	case 'K', 'k':
		mult = 1 << 10
	case 'M', 'm':
		mult = 1 << 20
	case 'G', 'g':
		mult = 1 << 30
	case 'T', 't':
		mult = 1 << 40
	}
	digits := raw
	if mult > 1 {
		// Not trimmed: "5 G" is a typo, and the point of erroring at all is
		// that a mistyped ceiling must not quietly become a different one.
		digits = raw[:len(raw)-1]
	}
	n, err := strconv.ParseInt(digits, 10, 64)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("%s must be a byte count, optionally suffixed K/M/G/T; got %q", key, raw)
	}
	return n * mult, nil
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// secretKey returns the master secret and whether every process in this
// deployment will derive the same one.
//
// The environment variable keeps its historical name. It no longer has
// anything to do with sessions — those are rows now — but it is written into
// every existing deployment's .env, and a rename that silently falls back to
// an ephemeral key would break office previews at runtime rather than at
// startup.
func secretKey() ([]byte, bool, error) {
	if v := os.Getenv("PP_SESSION_KEY"); v != "" {
		b, err := base64.StdEncoding.DecodeString(v)
		if err != nil || len(b) < 32 {
			return nil, false, fmt.Errorf("PP_SESSION_KEY must be base64 of >=32 bytes")
		}
		return b, true, nil
	}
	if env("PP_DEV_INSECURE", "") == "1" {
		slog.Warn("PP_DEV_INSECURE=1: using fixed development secret key")
		return []byte("pphub-dev-secret-key-32-bytes!!!"), true, nil
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, false, err
	}
	slog.Warn("PP_SESSION_KEY unset: generated ephemeral secret key, office previews are disabled in a multi-process deployment")
	return key, false, nil
}

// DeriveKey returns a 32-byte subkey bound to purpose.
//
// Two token classes used to be signed with the master secret directly — the
// session cookie and the WOPI access token — which meant one leak compromised
// both and neither could be rotated without the other. Sessions have since
// moved to the database, but the principle stands for whatever signs next:
// ask for a subkey, never the master.
func (c *Config) DeriveKey(purpose string) []byte {
	key, err := hkdf.Key(sha256.New, c.SecretKey, nil, "pp-hub/"+purpose, 32)
	if err != nil {
		// HKDF fails only on an absurd key length; 32 bytes from SHA-256 is
		// always derivable, so this is unreachable rather than handled.
		panic("config: derive " + purpose + ": " + err.Error())
	}
	return key
}
