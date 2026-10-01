package auth

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nhatbui/pp-hub/apps/server/internal/audit"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

// Agent access (M41). An AI agent presents an OAuth access token that Logto
// minted for the MCP resource; this process is only a resource server and
// never issues or refreshes one. See docs/architecture/agent-access.md.

var (
	// ErrInvalidAccessToken is a token that is not a live JWT this
	// deployment's issuer minted for the audience asked about.
	ErrInvalidAccessToken = errors.New("invalid access token")
	// ErrUnknownAgentUser is a valid token for a person who has no account
	// here. The MCP path never creates one: signing in to pp-hub does.
	ErrUnknownAgentUser = errors.New("no pp-hub account for this identity")
)

// AgentToken is a verified access token and the account it acts for.
type AgentToken struct {
	User store.User
	// ClientID is the OAuth client holding the token — for a CIMD client,
	// the URL of its metadata document.
	ClientID string
	Expiry   time.Time
}

// Issuer is the issuer the browser and agents are sent to, which is what
// the `iss` of every token carries.
func (s *Service) Issuer() string { return s.cfg.OIDCIssuerExt }

// VerifyAccessToken checks a bearer token against the IdP's keys — signature,
// issuer, expiry, and that `aud` names audience — and resolves the account
// behind its subject.
//
// The audience check is what keeps every other token this IdP mints out:
// pp-hub's own ID tokens are addressed to its client id, a token for another
// API resource to that resource, and neither names the MCP resource.
// Suspension is the caller's to check, as it is for a session.
func (s *Service) VerifyAccessToken(ctx context.Context, raw, audience string) (AgentToken, error) {
	tok, err := s.provider.Verifier(&oidc.Config{ClientID: audience}).Verify(ctx, raw)
	if err != nil {
		return AgentToken{}, fmt.Errorf("%w: %v", ErrInvalidAccessToken, err)
	}
	var claims struct {
		ClientID string `json:"client_id"`
	}
	if err := tok.Claims(&claims); err != nil {
		return AgentToken{}, fmt.Errorf("%w: claims: %v", ErrInvalidAccessToken, err)
	}
	user, err := s.queries.GetUserByIdentity(ctx, store.GetUserByIdentityParams{
		OidcIss: tok.Issuer,
		OidcSub: tok.Subject,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return AgentToken{}, ErrUnknownAgentUser
	}
	if err != nil {
		return AgentToken{}, err
	}
	return AgentToken{User: user, ClientID: claims.ClientID, Expiry: tok.Expiry}, nil
}

// --- personal access tokens ---
//
// A coding agent configured from a file, a CI job or a script has no browser
// to sign in with, so a person can mint a token here and paste it into an
// Authorization header instead of going through Logto. It acts for its
// creator on /mcp/ only, with exactly their access, and like a session only
// its SHA-256 is stored. Unlike a session it does not slide: it dies on the
// date chosen at creation, at most MaxAgentTokenTTL away.

// AgentTokenPrefix marks a personal access token, so the bearer middleware
// can tell one from a Logto JWT without trying both, and a secret scanner can
// recognise one in a leaked file.
const AgentTokenPrefix = "pph_"

// MaxAgentTokenTTL is the longest a token may live — the absolute cap a
// session has, for the same reason: a credential nobody revokes should still
// end.
const MaxAgentTokenTTL = SessionAbsoluteTTL

// agentTokenTouchAfter is how stale last_used_at may get before a use
// writes it again: an agent makes many calls a minute, and "last used" needs
// minutes, not milliseconds.
const agentTokenTouchAfter = 5 * time.Minute

// ErrBadAgentToken is a name or lifetime the caller has to correct.
var ErrBadAgentToken = errors.New("invalid token request")

// IsAgentToken reports whether a bearer credential is a personal access
// token rather than an OAuth access token.
func IsAgentToken(raw string) bool { return strings.HasPrefix(raw, AgentTokenPrefix) }

func agentTokenHash(raw string) []byte {
	sum := sha256.Sum256([]byte(raw))
	return sum[:]
}

// CreateAgentToken mints a token for user, returning the row and the token —
// which exists in plaintext only in this return value.
func (s *Service) CreateAgentToken(r *http.Request, user store.User, name string, ttl time.Duration) (store.AgentToken, string, error) {
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > 100 {
		return store.AgentToken{}, "", fmt.Errorf("%w: a name of 1 to 100 characters is required", ErrBadAgentToken)
	}
	if ttl <= 0 || ttl > MaxAgentTokenTTL {
		return store.AgentToken{}, "", fmt.Errorf("%w: a token lives at most %d days", ErrBadAgentToken, int(MaxAgentTokenTTL.Hours()/24))
	}
	secret, err := randomToken()
	if err != nil {
		return store.AgentToken{}, "", err
	}
	raw := AgentTokenPrefix + secret
	tok, err := s.queries.CreateAgentToken(r.Context(), store.CreateAgentTokenParams{
		UserID:    user.ID,
		Name:      name,
		TokenHash: agentTokenHash(raw),
		ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(ttl), Valid: true},
	})
	if err != nil {
		return store.AgentToken{}, "", err
	}
	s.audit.Record(r, audit.Event{
		Kind: audit.KindAgentTokenCreated, UserID: user.ID, Subject: user.Email,
		Detail: map[string]any{"token": tok.ID.String(), "name": tok.Name, "expires_at": tok.ExpiresAt.Time},
	})
	return tok, raw, nil
}

// AuthenticateAgentToken resolves a personal access token to the account it
// acts for. An unknown, revoked or expired token is ErrInvalidAccessToken,
// the same answer a bad JWT gets. ClientID names the token ("pat:<id>"), so
// the audit trail says which one acted.
func (s *Service) AuthenticateAgentToken(ctx context.Context, raw string) (AgentToken, error) {
	row, err := s.queries.GetAgentTokenByHash(ctx, agentTokenHash(raw))
	if errors.Is(err, pgx.ErrNoRows) {
		return AgentToken{}, fmt.Errorf("%w: unknown or expired personal token", ErrInvalidAccessToken)
	}
	if err != nil {
		return AgentToken{}, err
	}
	t := row.AgentToken
	if !t.LastUsedAt.Valid || time.Since(t.LastUsedAt.Time) > agentTokenTouchAfter {
		// A failed touch is not a failed request: the token is valid now.
		_ = s.queries.TouchAgentToken(ctx, t.ID)
	}
	return AgentToken{User: row.User, ClientID: "pat:" + t.ID.String(), Expiry: t.ExpiresAt.Time}, nil
}

// ListAgentTokens is the person's live tokens, newest first.
func (s *Service) ListAgentTokens(ctx context.Context, userID pgtype.UUID) ([]store.AgentToken, error) {
	return s.queries.ListUserAgentTokens(ctx, userID)
}

// RevokeAgentToken deletes one of the person's own tokens; another person's
// id deletes nothing.
func (s *Service) RevokeAgentToken(r *http.Request, user store.User, id pgtype.UUID) (bool, error) {
	n, err := s.queries.DeleteUserAgentToken(r.Context(), store.DeleteUserAgentTokenParams{ID: id, UserID: user.ID})
	if err == nil && n > 0 {
		s.audit.Record(r, audit.Event{
			Kind: audit.KindAgentTokenRevoked, UserID: user.ID, Subject: user.Email,
			Detail: map[string]any{"token": id.String()},
		})
	}
	return n > 0, err
}

// PurgeExpiredAgentTokens drops tokens past their date. Expired tokens are
// already refused on lookup; this is housekeeping.
func (s *Service) PurgeExpiredAgentTokens(ctx context.Context) (int64, error) {
	return s.queries.DeleteExpiredAgentTokens(ctx)
}
