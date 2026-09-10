// Package auth implements generic OIDC login (Logto, or any compliant
// provider) and cookie sessions.
//
// The issuer URL is intentionally configurable: PP_OIDC_ISSUER_EXTERNAL is the
// issuer advertised to the browser and used for discovery, so both sides agree
// on the `iss` claim. Endpoints (auth, token, end_session) come from the
// provider's discovery document — nothing provider-specific is hardcoded.
package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"golang.org/x/oauth2"

	"github.com/nhatbui/pp-hub/apps/server/internal/config"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

const (
	stateCookie   = "pp_oidc_state"
	sessionCookie = "pp_session"
	sessionTTL    = 30 * 24 * time.Hour
)

type Service struct {
	cfg           *config.Config
	queries       *store.Queries
	provider      *oidc.Provider
	verifier      *oidc.IDTokenVerifier
	oauth         oauth2.Config
	endSessionURL string // from discovery doc, empty if unsupported
}

func NewService(ctx context.Context, cfg *config.Config, queries *store.Queries) (*Service, error) {
	provider, err := oidc.NewProvider(ctx, cfg.OIDCIssuerExt)
	if err != nil {
		return nil, fmt.Errorf("oidc discovery %s: %w", cfg.OIDCIssuerExt, err)
	}
	s := &Service{
		cfg:      cfg,
		queries:  queries,
		provider: provider,
		verifier: provider.Verifier(&oidc.Config{ClientID: cfg.OIDCClientID}),
		oauth: oauth2.Config{
			ClientID:     cfg.OIDCClientID,
			ClientSecret: cfg.OIDCClientSecret,
			Endpoint:     provider.Endpoint(),
			RedirectURL:  cfg.ExternalURL + "/auth/callback",
			Scopes:       []string{oidc.ScopeOpenID, "profile", "email"},
		},
	}
	var meta struct {
		EndSession string `json:"end_session_endpoint"`
	}
	if err := provider.Claims(&meta); err != nil {
		return nil, fmt.Errorf("oidc discovery claims: %w", err)
	}
	s.endSessionURL = meta.EndSession
	slog.Info("oidc ready", "issuer", cfg.OIDCIssuerExt)
	return s, nil
}

func (s *Service) HandleLogin(w http.ResponseWriter, r *http.Request) {
	state, err := randomToken()
	if err != nil {
		http.Error(w, "internal", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     stateCookie,
		Value:    state,
		Path:     "/",
		MaxAge:   600,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, s.oauth.AuthCodeURL(state), http.StatusFound)
}

func (s *Service) HandleCallback(w http.ResponseWriter, r *http.Request) {
	state, err := r.Cookie(stateCookie)
	if err != nil || state.Value == "" || r.URL.Query().Get("state") != state.Value {
		http.Error(w, "invalid state", http.StatusBadRequest)
		return
	}
	token, err := s.oauth.Exchange(r.Context(), r.URL.Query().Get("code"))
	if err != nil {
		slog.Error("oauth exchange", "err", err)
		http.Error(w, "exchange failed", http.StatusBadGateway)
		return
	}
	rawID, ok := token.Extra("id_token").(string)
	if !ok {
		http.Error(w, "no id_token", http.StatusBadGateway)
		return
	}
	idToken, err := s.verifier.Verify(r.Context(), rawID)
	if err != nil {
		slog.Error("id_token verify", "err", err)
		http.Error(w, "invalid id_token", http.StatusBadGateway)
		return
	}
	var claims struct {
		Email string `json:"email"`
		Name  string `json:"name"`
	}
	if err := idToken.Claims(&claims); err != nil {
		http.Error(w, "invalid claims", http.StatusBadGateway)
		return
	}

	// Some providers omit profile claims from the ID token; fall back to the
	// userinfo endpoint so the local user record always has email/name.
	if claims.Email == "" && claims.Name == "" {
		if info, err := s.provider.UserInfo(r.Context(), oauth2.StaticTokenSource(token)); err != nil {
			slog.Warn("userinfo fetch", "err", err)
		} else if err := info.Claims(&claims); err != nil {
			slog.Warn("userinfo claims", "err", err)
		}
	}

	user, err := s.queries.UpsertUser(r.Context(), store.UpsertUserParams{
		OidcSub: idToken.Subject,
		OidcIss: idToken.Issuer,
		Email:   claims.Email,
		Name:    claims.Name,
	})
	if err != nil {
		slog.Error("upsert user", "err", err)
		http.Error(w, "internal", http.StatusInternalServerError)
		return
	}

	cookie, err := s.signSession(user.ID)
	if err != nil {
		http.Error(w, "internal", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    cookie,
		Path:     "/",
		MaxAge:   int(sessionTTL.Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	clearCookie(w, stateCookie)
	http.Redirect(w, r, "/", http.StatusFound)
}

func (s *Service) HandleLogout(w http.ResponseWriter, r *http.Request) {
	clearCookie(w, sessionCookie)
	writeNoContent(w)
}

// CurrentUser resolves the session cookie to a user, or nil.
func (s *Service) CurrentUser(r *http.Request) *store.User {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return nil
	}
	id, err := s.verifySession(c.Value)
	if err != nil {
		return nil
	}
	user, err := s.queries.GetUser(r.Context(), id)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			slog.Error("get user", "err", err)
		}
		return nil
	}
	return &user
}

func (s *Service) EndSessionURL(w http.ResponseWriter, r *http.Request) string {
	if s.endSessionURL == "" {
		return "/"
	}
	u, err := url.Parse(s.endSessionURL)
	if err != nil {
		return "/"
	}
	q := u.Query()
	q.Set("post_logout_redirect_uri", s.cfg.ExternalURL)
	q.Set("client_id", s.cfg.OIDCClientID)
	u.RawQuery = q.Encode()
	return u.String()
}

// --- session cookies: value = "v1.<userID>.<expiryUnix>.<hmac>" ---

func (s *Service) signSession(id pgtype.UUID) (string, error) {
	raw, err := id.Value()
	if err != nil {
		return "", err
	}
	idStr, ok := raw.(string)
	if !ok {
		return "", fmt.Errorf("unexpected uuid value %T", raw)
	}
	exp := time.Now().Add(sessionTTL)
	var expBuf [8]byte
	binary.BigEndian.PutUint64(expBuf[:], uint64(exp.Unix()))
	payload := idStr + "." + base64.RawURLEncoding.EncodeToString(expBuf[:])
	mac := hmac.New(sha256.New, s.cfg.SessionKey)
	mac.Write([]byte(payload))
	return "v1." + payload + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func (s *Service) verifySession(value string) (pgtype.UUID, error) {
	var id pgtype.UUID
	parts := strings.Split(value, ".")
	if len(parts) != 4 || parts[0] != "v1" {
		return id, errors.New("bad session format")
	}
	mac := hmac.New(sha256.New, s.cfg.SessionKey)
	mac.Write([]byte(parts[1] + "." + parts[2]))
	expected, err := base64.RawURLEncoding.DecodeString(parts[3])
	if err != nil || !hmac.Equal(mac.Sum(nil), expected) {
		return id, errors.New("bad session signature")
	}
	expRaw, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || len(expRaw) != 8 {
		return id, errors.New("bad session expiry")
	}
	if time.Now().Unix() > int64(binary.BigEndian.Uint64(expRaw)) {
		return id, errors.New("session expired")
	}
	if err := id.Scan(parts[1]); err != nil {
		return id, err
	}
	return id, nil
}

func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func clearCookie(w http.ResponseWriter, name string) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: "", Path: "/", MaxAge: -1, HttpOnly: true})
}

func writeNoContent(w http.ResponseWriter) { w.WriteHeader(http.StatusNoContent) }
