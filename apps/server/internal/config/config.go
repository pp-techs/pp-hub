package config

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"log/slog"
	"os"
)

type Config struct {
	Addr             string
	DatabaseURL      string
	OIDCIssuer       string // issuer URL as reachable by the server
	OIDCIssuerExt    string // issuer URL as reachable by the user's browser (defaults to OIDCIssuer)
	OIDCClientID     string
	OIDCClientSecret string
	ExternalURL      string // public base URL of this server
	SessionKey       []byte // HMAC key for session cookies
	// SessionKeyShared is false when the key was generated at startup rather
	// than supplied. Anything that signs a token another process must verify
	// — the worker's office conversion fetch — has to refuse in that case.
	SessionKeyShared bool
	BlobRoot         string // local CAS blob directory
	BlobBackend      string // "local" | "s3"
	S3               S3Settings
	OnlyOfficeURL    string // OnlyOffice Document Server base URL (browser-facing)
	OnlyOfficeIntURL string // OnlyOffice as reachable from the server/worker (container network)
	WopiServerURL    string // base URL OnlyOffice uses to call this server (container network)
}

func Load() (*Config, error) {
	key, shared, err := sessionKey()
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
		SessionKey:       key,
		SessionKeyShared: shared,
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

// sessionKey returns the HMAC key and whether every process in this
// deployment will derive the same one.
func sessionKey() ([]byte, bool, error) {
	if v := os.Getenv("PP_SESSION_KEY"); v != "" {
		b, err := base64.StdEncoding.DecodeString(v)
		if err != nil || len(b) < 32 {
			return nil, false, fmt.Errorf("PP_SESSION_KEY must be base64 of >=32 bytes")
		}
		return b, true, nil
	}
	if env("PP_DEV_INSECURE", "") == "1" {
		slog.Warn("PP_DEV_INSECURE=1: using fixed development session key")
		return []byte("pphub-dev-session-key-32bytes!!"), true, nil
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, false, err
	}
	slog.Warn("PP_SESSION_KEY unset: generated ephemeral key, sessions will not survive restarts")
	return key, false, nil
}
