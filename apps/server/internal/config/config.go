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
	BlobRoot         string // local CAS blob directory
	OnlyOfficeURL    string // OnlyOffice Document Server base URL (browser-facing)
	WopiServerURL    string // base URL OnlyOffice uses to call this server (container network)
}

func Load() (*Config, error) {
	key, err := sessionKey()
	if err != nil {
		return nil, err
	}
	c := &Config{
		Addr:             env("PP_ADDR", ":8080"),
		DatabaseURL:      env("PP_DATABASE_URL", "postgres://postgres:postgres@localhost:5432/pphub?sslmode=disable"),
		OIDCIssuer:       env("PP_OIDC_ISSUER", "http://localhost:9080/realms/pphub"),
		OIDCIssuerExt:    os.Getenv("PP_OIDC_ISSUER_EXTERNAL"),
		OIDCClientID:     env("PP_OIDC_CLIENT_ID", "pphub-server"),
		OIDCClientSecret: env("PP_OIDC_CLIENT_SECRET", "pphub-server-secret"),
		ExternalURL:      env("PP_EXTERNAL_URL", "http://localhost:8080"),
		SessionKey:       key,
		BlobRoot:         env("PP_BLOB_ROOT", "./data/blobs"),
		OnlyOfficeURL:    env("PP_ONLYOFFICE_URL", "http://localhost:9980"),
		WopiServerURL:    env("PP_WOPI_SERVER_URL", "http://localhost:8080"),
	}
	if c.OIDCIssuerExt == "" {
		c.OIDCIssuerExt = c.OIDCIssuer
	}
	return c, nil
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func sessionKey() ([]byte, error) {
	if v := os.Getenv("PP_SESSION_KEY"); v != "" {
		b, err := base64.StdEncoding.DecodeString(v)
		if err != nil || len(b) < 32 {
			return nil, fmt.Errorf("PP_SESSION_KEY must be base64 of >=32 bytes")
		}
		return b, nil
	}
	if env("PP_DEV_INSECURE", "") == "1" {
		slog.Warn("PP_DEV_INSECURE=1: using fixed development session key")
		return []byte("pphub-dev-session-key-32bytes!!"), nil
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	slog.Warn("PP_SESSION_KEY unset: generated ephemeral key, sessions will not survive restarts")
	return key, nil
}
