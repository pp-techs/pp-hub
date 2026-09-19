package config

import (
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"log/slog"
	"os"
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
}

func Load() (*Config, error) {
	key, shared, err := secretKey()
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
