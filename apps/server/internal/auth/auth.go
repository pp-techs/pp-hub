// Package auth implements OIDC login against Logto and server-side sessions.
//
// Two things here are deliberately configurable and easy to get wrong:
//
// Discovery runs against PP_OIDC_ISSUER — the issuer *this process* can reach
// — while PP_OIDC_ISSUER_EXTERNAL is what the browser is sent to and what must
// appear in the `iss` claim. When they differ, the two endpoints the browser
// visits (authorization and end-session) are rebased onto the external origin;
// token, userinfo and JWKS stay internal because only the server calls them.
// In the usual single-origin deployment the two strings are equal and none of
// this does anything.
//
// A session is a row in `sessions`, not a signed blob. The cookie carries an
// opaque 256-bit token and the database stores only its SHA-256, so a session
// can be revoked, listed, and survives a restart or a key rotation.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"golang.org/x/oauth2"

	"github.com/nhatbui/pp-hub/apps/server/internal/audit"
	"github.com/nhatbui/pp-hub/apps/server/internal/config"
	"github.com/nhatbui/pp-hub/apps/server/internal/orgsync"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
	"github.com/nhatbui/pp-hub/apps/server/internal/users"
)

const (
	sessionCookie = "pp_session"
	txCookie      = "pp_oidc_tx"

	// A session dies after this long unused. The deadline slides, but lazily
	// — see sessionRefreshAfter — so an active tab costs one UPDATE an hour
	// rather than one per request.
	sessionIdleTTL      = 30 * 24 * time.Hour
	sessionRefreshAfter = time.Hour

	// A guest's session is short, and slides the same way. Somebody who
	// proved an address once has not been through an identity provider, so
	// the proof is allowed to go stale sooner.
	guestIdleTTL = 7 * 24 * time.Hour

	// The login handshake is a round trip through the IdP, not a coffee break.
	txTTL = 10 * time.Minute
)

// SessionAbsoluteTTL is how long a session may live however active it is.
// Without an absolute cap, "idle timeout" is not a timeout. Exported for
// connections that outlive their request and re-check their session (collab).
const SessionAbsoluteTTL = 90 * 24 * time.Hour

const (
	// Bounds on what the client controls before it reaches a TEXT column.
	maxUserAgent = 400
)

// Session is a live login: the row's identity plus its owner, resolved
// together in one read.
type Session struct {
	ID   pgtype.UUID
	User store.User
}

type Service struct {
	cfg      *config.Config
	queries  *store.Queries
	audit    *audit.Service
	provider *oidc.Provider
	verifier *oidc.IDTokenVerifier
	oauth    oauth2.Config
	// endSessionURL is the IdP's RP-initiated logout endpoint, already rebased
	// onto the browser-facing origin. Empty when the provider does not
	// advertise one.
	endSessionURL string
	// secure marks cookies Secure. Derived from the scheme of the public URL
	// rather than from the request, because a request arriving over http on a
	// deployment that is meant to be https is exactly the case the flag has to
	// survive.
	secure bool
	// orgs accepts invitations and applies the organization claims to local
	// membership. Set by SetReconciler before the server starts serving.
	orgs Reconciler
	// onSignIn provisions personal spaces and adopts pending shares. Nil in
	// tests that do not exercise them.
	onSignIn SignInHook
	// profiles mirrors the IdP-owned profile fields — avatar, phone, gender
	// — onto the account at every sign-in.
	profiles *users.Service
}

// scopes are what the authorization request asks for: the three every
// provider serves, Logto's two organization scopes — which put
// `organizations` and `organization_roles` in the ID token — and `phone`
// (OIDC Core §5.4) only when PP_OIDC_PHONE_SCOPE=1, because Logto refuses it
// for a third-party application until its Permissions include it, and a
// refused scope fails the whole sign-in.
func scopes(cfg *config.Config) []string {
	s := []string{oidc.ScopeOpenID, "profile", "email",
		"urn:logto:scope:organizations", "urn:logto:scope:organization_roles"}
	if cfg.OIDCPhoneScope {
		s = append(s, "phone")
	}
	return s
}

// Reconciler applies the organization claims in an ID token to local
// membership, after accepting whatever invitations are waiting for the
// person signing in. Implemented by orgsync.Service; an interface here only
// so the dependency is stated where it is used.
type Reconciler interface {
	AcceptInvitations(ctx context.Context, user store.User, email string) (orgsync.Claims, error)
	ReconcileUser(ctx context.Context, user store.User, claims orgsync.Claims) error
}

func NewService(ctx context.Context, cfg *config.Config, queries *store.Queries, profiles *users.Service) (*Service, error) {
	discoveryCtx := ctx
	if cfg.OIDCIssuer != cfg.OIDCIssuerExt {
		// Fetch from the internal URL, but keep expecting the external issuer
		// in `iss` — which is what the IdP actually mints and what the browser
		// was sent to.
		discoveryCtx = oidc.InsecureIssuerURLContext(ctx, cfg.OIDCIssuerExt)
	}
	provider, err := oidc.NewProvider(discoveryCtx, cfg.OIDCIssuer)
	if err != nil {
		return nil, fmt.Errorf("oidc discovery %s: %w", cfg.OIDCIssuer, err)
	}

	endpoint := provider.Endpoint()
	endpoint.AuthURL = rebaseOrigin(endpoint.AuthURL, cfg.OIDCIssuer, cfg.OIDCIssuerExt)

	s := &Service{
		cfg:      cfg,
		queries:  queries,
		audit:    audit.New(queries),
		provider: provider,
		verifier: provider.Verifier(&oidc.Config{ClientID: cfg.OIDCClientID}),
		oauth: oauth2.Config{
			ClientID:     cfg.OIDCClientID,
			ClientSecret: cfg.OIDCClientSecret,
			Endpoint:     endpoint,
			RedirectURL:  cfg.ExternalURL + "/auth/callback",
			Scopes:       scopes(cfg),
		},
		secure:   strings.HasPrefix(cfg.ExternalURL, "https://"),
		profiles: profiles,
	}
	var meta struct {
		EndSession string `json:"end_session_endpoint"`
	}
	if err := provider.Claims(&meta); err != nil {
		return nil, fmt.Errorf("oidc discovery claims: %w", err)
	}
	s.endSessionURL = rebaseOrigin(meta.EndSession, cfg.OIDCIssuer, cfg.OIDCIssuerExt)
	if !s.secure {
		slog.WarnContext(ctx, "PP_EXTERNAL_URL is not https: session cookies will be sent without the Secure flag",
			"external_url", cfg.ExternalURL)
	}
	slog.InfoContext(ctx, "oidc ready", "discovery", cfg.OIDCIssuer, "issuer", cfg.OIDCIssuerExt, "secure_cookies", s.secure)
	return s, nil
}

// --- login handshake ---

// oidcTx is everything one in-flight login needs to remember, in one cookie.
//
// It is not signed. Every field is either random per transaction (state,
// nonce, verifier — 256 bits each, so unguessable) or re-validated on the way
// out (next, through safeNext again). A signature would protect against an
// attacker who can already write cookies to this origin, and such an attacker
// can equally well start a real handshake and copy the result: the defence
// against that is the state comparison below, which this cookie *is*.
type oidcTx struct {
	State    string `json:"s"`
	Nonce    string `json:"n"`
	Verifier string `json:"v"`
	Next     string `json:"r,omitempty"`
}

func (s *Service) HandleLogin(w http.ResponseWriter, r *http.Request) {
	state, err := randomToken()
	if err != nil {
		s.failLogin(w, r, "internal", "random state", err)
		return
	}
	nonce, err := randomToken()
	if err != nil {
		s.failLogin(w, r, "internal", "random nonce", err)
		return
	}
	tx := oidcTx{
		State:    state,
		Nonce:    nonce,
		Verifier: oauth2.GenerateVerifier(),
		// Where the browser was headed before it was bounced here. It cannot
		// ride the OAuth `state`, which is compared byte-for-byte.
		Next: safeNext(r.URL.Query().Get("next")),
	}
	blob, err := json.Marshal(tx)
	if err != nil {
		s.failLogin(w, r, "internal", "encode tx", err)
		return
	}
	http.SetCookie(w, s.cookie(txCookie, base64.RawURLEncoding.EncodeToString(blob), int(txTTL.Seconds())))

	// PKCE binds the code to this browser, so a code intercepted in the
	// redirect is useless without the verifier. The nonce binds the ID token
	// to this handshake, so a token replayed from another one is rejected.
	opts := []oauth2.AuthCodeOption{
		oidc.Nonce(tx.Nonce),
		oauth2.S256ChallengeOption(tx.Verifier),
	}
	// The invitation email links here with the invitee's address, so the
	// IdP opens on it rather than on a blank form. Standard OIDC; a provider
	// that ignores it loses nothing.
	if hint := loginHint(r.URL.Query().Get("login_hint")); hint != "" {
		opts = append(opts, oauth2.SetAuthURLParam("login_hint", hint))
	}
	http.Redirect(w, r, s.oauth.AuthCodeURL(tx.State, opts...), http.StatusFound)
}

// loginHint passes through an address-shaped hint and drops anything else.
// It is forwarded to the IdP verbatim, so it is bounded like every other
// client-controlled value that leaves this process.
func loginHint(raw string) string {
	hint := strings.TrimSpace(raw)
	if len(hint) > 254 || !strings.Contains(hint, "@") ||
		strings.ContainsFunc(hint, func(r rune) bool { return r <= ' ' || r == 0x7f }) {
		return ""
	}
	return hint
}

func (s *Service) HandleCallback(w http.ResponseWriter, r *http.Request) {
	tx, haveTx := s.readTx(r)
	// Unconditionally, before anything can return early. The transaction is
	// spent the moment the IdP redirects back; leaving the cookie in place on
	// a failure left a valid state value sitting in the browser for its full
	// ten minutes, which is a replay window for no benefit.
	s.clearCookie(w, txCookie)

	if e := r.URL.Query().Get("error"); e != "" {
		s.failLogin(w, r, e, "provider returned an error",
			errors.New(r.URL.Query().Get("error_description")))
		return
	}
	if !haveTx || tx.State == "" ||
		subtle.ConstantTimeCompare([]byte(r.URL.Query().Get("state")), []byte(tx.State)) != 1 {
		s.failLogin(w, r, "state", "state did not match the login transaction", nil)
		return
	}

	token, err := s.oauth.Exchange(r.Context(), r.URL.Query().Get("code"),
		oauth2.VerifierOption(tx.Verifier))
	if err != nil {
		s.failLogin(w, r, "exchange", "code exchange failed", err)
		return
	}
	rawID, ok := token.Extra("id_token").(string)
	if !ok {
		s.failLogin(w, r, "token", "provider returned no id_token", nil)
		return
	}
	idToken, err := s.verifier.Verify(r.Context(), rawID)
	if err != nil {
		s.failLogin(w, r, "token", "id_token failed verification", err)
		return
	}
	// go-oidc parses the nonce but leaves the comparison to the caller, since
	// only the caller knows what it sent.
	if subtle.ConstantTimeCompare([]byte(idToken.Nonce), []byte(tx.Nonce)) != 1 {
		s.failLogin(w, r, "token", "id_token nonce did not match the login transaction", nil)
		return
	}

	claims, err := s.claims(r.Context(), idToken, token)
	if err != nil {
		s.failLogin(w, r, "token", "id_token claims could not be read", err)
		return
	}
	user, err := s.resolveUser(r, idToken, claims)
	if err != nil {
		if errors.Is(err, errEmailTaken) {
			s.failLogin(w, r, "email-taken", "an account with this email already exists", err)
			return
		}
		s.failLogin(w, r, "internal", "resolve user", err)
		return
	}
	if user.SuspendedAt.Valid {
		s.failLogin(w, r, "suspended", "this account is suspended", nil)
		return
	}
	user = s.syncProfile(r, user, claims)
	s.promoteBootstrapAdmin(r, &user, claims)
	s.reconcileOrgs(r, user, claims)
	// After membership is current, so the hook reads the spaces this
	// sign-in just put the person in.
	if s.onSignIn != nil {
		verified := ""
		if bool(claims.EmailVerified) {
			verified = claims.Email
		}
		s.onSignIn(r, user, verified)
	}

	// Signing in again from a browser that already holds a session replaces
	// it rather than stacking on top. The old cookie is about to be
	// overwritten, so its row could never be presented again — it would just
	// sit in the Sessions list as a device nobody is using.
	s.revokeCookieSession(r)

	if err := s.issueSession(r.Context(), w, r, user.ID); err != nil {
		s.failLogin(w, r, "internal", "create session", err)
		return
	}
	s.audit.Record(r, audit.Event{
		Kind:    audit.KindLogin,
		UserID:  user.ID,
		Subject: user.Email,
	})

	// Back to wherever the browser was going, revalidated: the cookie is ours
	// and HttpOnly, but it is still input.
	dest := "/"
	if next := safeNext(tx.Next); next != "" {
		dest = next
	}
	http.Redirect(w, r, dest, http.StatusFound)
}

// SetReconciler wires the organization sync in after construction.
//
// A setter rather than a constructor argument because orgsync needs the
// space service, the space service needs the store, and this service needs a
// live OIDC discovery — an ordering that no single constructor call can
// satisfy without one of them taking the other's half-built self.
func (s *Service) SetReconciler(r Reconciler) { s.orgs = r }

// SignInHook runs at every successful interactive sign-in, after
// organization membership has been reconciled and before the session is
// issued. verifiedEmail is the address the identity provider vouched for, or
// empty when it did not. A hook must not fail the sign-in: it logs and moves
// on, like reconcileOrgs.
type SignInHook func(r *http.Request, user store.User, verifiedEmail string)

// SetSignInHook wires what personal spaces and pending shares need at
// sign-in (cmd/server). A setter for the same ordering reason as
// SetReconciler.
func (s *Service) SetSignInHook(h SignInHook) { s.onSignIn = h }

// reconcileOrgs accepts the invitations waiting for this person, then applies
// the organization claims before the session is issued, so the first page
// load already lists the right spaces — including one they were invited to a
// minute ago.
//
// A failure is logged and swallowed. The claims describe membership, not
// identity: refusing the sign-in would lock somebody out of spaces they are
// already in because a *different* space could not be updated, and the
// hourly sweep will converge it anyway.
func (s *Service) reconcileOrgs(r *http.Request, user store.User, claims idClaims) {
	orgClaims := orgsync.Claims{
		Organizations:     claims.Organizations,
		OrganizationRoles: claims.OrganizationRoles,
	}
	var accepted orgsync.Claims
	// Only a verified address may redeem an invitation made out to it.
	if bool(claims.EmailVerified) {
		var err error
		accepted, err = s.orgs.AcceptInvitations(r.Context(), user, claims.Email)
		if err != nil {
			slog.ErrorContext(r.Context(), "organization invitations at sign-in", "user", user.Email, "err", err)
		}
		// The ID token predates the acceptance; without the merge the new
		// membership would read as a departure.
		orgClaims = orgClaims.With(accepted)
	}
	// An absent `organizations` claim is not the same as an empty one. Empty
	// is Logto saying "no organizations", and leaving every linked space is
	// the right reading of it. Absent means the sign-in application was not
	// granted the organization scopes — a deployment mistake — and reading
	// that as "left everything" would evict every member at their next
	// sign-in.
	if claims.Organizations == nil && len(accepted.Organizations) == 0 {
		slog.WarnContext(r.Context(), "ID token carries no organizations claim; membership left to the sweep",
			"user", user.Email,
			"advice", "grant urn:logto:scope:organizations and urn:logto:scope:organization_roles to the sign-in application")
		return
	}
	if err := s.orgs.ReconcileUser(r.Context(), user, orgClaims); err != nil {
		slog.ErrorContext(r.Context(), "organization reconcile at sign-in", "user", user.Email, "err", err)
	}
}

// syncProfile mirrors the IdP-owned profile fields onto the account.
//
// Like reconcileOrgs, a failure is logged and swallowed: an avatar that cannot
// be fetched is not a reason to refuse a sign-in, and the next one retries.
func (s *Service) syncProfile(r *http.Request, user store.User, claims idClaims) store.User {
	synced, err := s.profiles.SyncIdPProfile(r.Context(), user, users.IdPProfile{
		Picture: claims.Picture,
		Phone:   claims.PhoneNumber,
		Gender:  claims.Gender,
	})
	if err != nil {
		slog.WarnContext(r.Context(), "idp profile sync", "user", user.ID.String(), "err", err)
	}
	return synced
}

// promoteBootstrapAdmin makes the operator named by PP_BOOTSTRAP_ADMIN_EMAIL
// an instance admin when they sign in.
//
// `users.is_admin` defaults to false and its only writer sits behind an
// admin-only endpoint, so a fresh install had no administrator and no way to
// appoint one except by hand in psql. This runs on every sign-in rather than
// once, so it also recovers an install whose last admin was demoted — and it
// is idempotent, so doing so costs nothing.
func (s *Service) promoteBootstrapAdmin(r *http.Request, user *store.User, claims idClaims) {
	want := s.cfg.BootstrapAdminEmail
	if want == "" || user.IsAdmin || !strings.EqualFold(user.Email, want) {
		return
	}
	// An unverified claim cannot buy instance admin. The address is matched
	// against a static configuration value, so without this the variable is
	// a standing authorization rule keyed on a field the IdP may let its
	// users edit — and anyone who could set their own address could take the
	// instance.
	if !bool(claims.EmailVerified) {
		slog.ErrorContext(r.Context(), "refusing admin promotion for an unverified email",
			"email", user.Email,
			"advice", "the identity provider must assert email_verified for this address")
		s.audit.Record(r, audit.Event{
			Kind:    audit.KindLoginFailed,
			UserID:  user.ID,
			Subject: user.Email,
			Detail:  map[string]any{"code": "bootstrap-admin-unverified"},
		})
		return
	}
	if err := s.queries.SetUserAdmin(r.Context(), store.SetUserAdminParams{
		ID: user.ID, IsAdmin: true,
	}); err != nil {
		slog.ErrorContext(r.Context(), "bootstrap admin promotion", "email", user.Email, "err", err)
		return
	}
	user.IsAdmin = true
	slog.WarnContext(r.Context(), "promoted to instance admin by PP_BOOTSTRAP_ADMIN_EMAIL",
		"email", user.Email,
		"advice", "unset the variable once a second admin exists, or it will keep re-promoting this account")
	s.audit.Record(r, audit.Event{
		Kind:    audit.KindAdminGranted,
		UserID:  user.ID,
		Subject: user.Email,
		Detail:  map[string]any{"via": "PP_BOOTSTRAP_ADMIN_EMAIL"},
	})
}

// failLogin logs the cause and bounces the browser back to the sign-in page
// with a machine-readable reason.
//
// The callback is a top-level navigation, not an API call, so the RFC 7807
// body the rest of the server returns would render as raw JSON in front of
// somebody who only tried to log in. The detail stays in the log; the browser
// gets a code the SPA can turn into a sentence.
func (s *Service) failLogin(w http.ResponseWriter, r *http.Request, code, msg string, cause error) {
	slog.ErrorContext(r.Context(), "oidc login failed", "code", code, "reason", msg, "err", cause)
	detail := map[string]any{"code": code, "reason": msg}
	if cause != nil {
		detail["err"] = cause.Error()
	}
	// Recorded even when no account was resolved — a run of these from one
	// address is the signal, and the rows with no user_id are exactly the
	// ones that carry it.
	s.audit.Record(r, audit.Event{Kind: audit.KindLoginFailed, Subject: code, Detail: detail})
	http.Redirect(w, r, "/login?error="+url.QueryEscape(code), http.StatusFound)
}

// claims reads the profile fields, falling back to the userinfo endpoint for
// providers that keep them out of the ID token.
func (s *Service) claims(ctx context.Context, idToken *oidc.IDToken, token *oauth2.Token) (idClaims, error) {
	var claims idClaims
	if err := idToken.Claims(&claims); err != nil {
		return claims, err
	}
	if claims.Email != "" {
		return claims, nil
	}
	info, err := s.provider.UserInfo(ctx, oauth2.StaticTokenSource(token))
	if err != nil {
		slog.WarnContext(ctx, "userinfo fetch", "err", err)
		return claims, nil
	}
	if err := info.Claims(&claims); err != nil {
		slog.WarnContext(ctx, "userinfo claims", "err", err)
	}
	return claims, nil
}

type idClaims struct {
	Email         string  `json:"email"`
	EmailVerified boolish `json:"email_verified"`
	Name          string  `json:"name"`
	// Profile claims the IdP owns and pp-hub only mirrors. Logto sends
	// `gender` only when it is set, and `phone_number` only for the `phone`
	// scope; absent decodes to "".
	Picture     string `json:"picture"`
	PhoneNumber string `json:"phone_number"`
	Gender      string `json:"gender"`
	// Organization claims, present only when the two urn:logto:scope:*
	// scopes were granted. Absent decodes to nil and empty to a non-nil
	// empty slice; reconcileOrgs tells the two apart.
	Organizations     []string `json:"organizations"`
	OrganizationRoles []string `json:"organization_roles"`
}

// boolish is a bool that also accepts "true"/"false".
//
// Some providers send email_verified as a string. encoding/json would fail the
// whole claims decode on that and take every other claim down with it, so the
// one field that is allowed to be sloppy parses both spellings.
type boolish bool

func (b *boolish) UnmarshalJSON(data []byte) error {
	var asBool bool
	if err := json.Unmarshal(data, &asBool); err == nil {
		*b = boolish(asBool)
		return nil
	}
	var asString string
	if err := json.Unmarshal(data, &asString); err != nil {
		return err
	}
	v, err := strconv.ParseBool(asString)
	if err != nil {
		return err
	}
	*b = boolish(v)
	return nil
}

var errEmailTaken = errors.New("email already belongs to another identity")

// resolveUser maps an OIDC identity onto a local account.
//
// The identity key is (issuer, subject), and the issuer is a URL an operator
// can change — putting the IdP behind a new hostname, or setting
// PP_OIDC_ISSUER_EXTERNAL for the first time. That used to make every
// returning user look brand new; the insert then collided with the unique
// index on (tenant_id, email_hash) and sign-in answered 500, for everyone, with
// no recovery short of editing Postgres by hand. So a miss on the identity
// falls back to the verified email and re-points the existing account.
//
// The fallback is gated on email_verified because an IdP that lets a user
// claim an arbitrary unverified address would otherwise let them claim an
// arbitrary account with it.
func (s *Service) resolveUser(r *http.Request, idToken *oidc.IDToken, claims idClaims) (store.User, error) {
	ctx := r.Context()
	user, err := s.queries.GetUserByIdentity(ctx, store.GetUserByIdentityParams{
		OidcIss: idToken.Issuer,
		OidcSub: idToken.Subject,
	})
	if err == nil {
		// The address is only refreshed when the IdP says it verified it.
		//
		// users.email is not decoration: it is what invitation acceptance
		// matches on, what a direct share is granted to, and what
		// PP_BOOTSTRAP_ADMIN_EMAIL is compared against. Rewriting it from an
		// unverified claim on every sign-in meant a user who could set their
		// own address at a self-service IdP could set it to the bootstrap
		// admin's and be promoted, or to a colleague's and collect their
		// pending invitations. The adoption path twenty lines below already
		// refused to trust an unverified claim; the asymmetry was the bug.
		email, verified := user.Email, user.EmailVerified
		if claims.Email != "" && bool(claims.EmailVerified) {
			email, verified = claims.Email, true
		} else if claims.Email != "" && claims.Email != user.Email {
			slog.WarnContext(ctx, "ignoring an unverified email claim",
				"user", user.ID.String(), "stored", user.Email, "claimed", claims.Email)
		}
		return s.queries.TouchUserLogin(ctx, store.TouchUserLoginParams{
			ID: user.ID, Email: email, Name: claims.Name, EmailVerified: verified,
		})
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return store.User{}, err
	}

	if claims.Email != "" && bool(claims.EmailVerified) {
		existing, err := s.queries.GetUserByEmail(ctx, claims.Email)
		if errors.Is(err, pgx.ErrNoRows) {
			// A guest is stored under the lower-cased address, and the
			// provider may spell it otherwise. Re-pointing the guest row,
			// rather than making a second account, keeps its grants and its
			// comments with the person they were for.
			existing, err = s.queries.GetGuestByEmailNorm(ctx, strings.ToLower(strings.TrimSpace(claims.Email)))
		}
		switch {
		case err == nil:
			slog.WarnContext(ctx, "oidc identity adopted by an existing account",
				"user", existing.ID.String(), "old_iss", existing.OidcIss, "new_iss", idToken.Issuer)
			s.audit.Record(r, audit.Event{
				Kind:    audit.KindIdentityAdopted,
				UserID:  existing.ID,
				Subject: claims.Email,
				Detail: map[string]any{
					"old_iss": existing.OidcIss,
					"new_iss": idToken.Issuer,
					"new_sub": idToken.Subject,
				},
			})
			return s.queries.AdoptUserIdentity(ctx, store.AdoptUserIdentityParams{
				ID:      existing.ID,
				OidcIss: idToken.Issuer,
				OidcSub: idToken.Subject,
				Email:   claims.Email,
				Name:    claims.Name,
			})
		case !errors.Is(err, pgx.ErrNoRows):
			return store.User{}, err
		}
	}

	created, err := s.queries.CreateUser(ctx, store.CreateUserParams{
		OidcSub: idToken.Subject,
		OidcIss: idToken.Issuer,
		Email:   claims.Email,
		Name:    claims.Name,
		// Recorded so sharing by email never resolves an address to an
		// account that merely claimed it.
		EmailVerified: claims.Email != "" && bool(claims.EmailVerified),
	})
	if err != nil {
		// The email is spoken for by an account this identity is not allowed
		// to adopt — an unverified address, or a second provider for the same
		// person. Say so instead of logging a constraint name.
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return store.User{}, fmt.Errorf("%w: %s", errEmailTaken, claims.Email)
		}
		return store.User{}, err
	}
	return created, nil
}

// --- sessions ---

func (s *Service) issueSession(ctx context.Context, w http.ResponseWriter, r *http.Request, userID pgtype.UUID) error {
	return s.issueSessionFor(ctx, w, r, userID, sessionIdleTTL)
}

// IssueGuestSession signs a guest in once their one-time code has been
// checked. The idle window is short and slides with use (guestIdleTTL).
func (s *Service) IssueGuestSession(ctx context.Context, w http.ResponseWriter, r *http.Request, userID pgtype.UUID) error {
	return s.issueSessionFor(ctx, w, r, userID, guestIdleTTL)
}

// idleTTLFor is how long a session of this account lives unused.
func idleTTLFor(u store.User) time.Duration {
	if u.Kind == "guest" {
		return guestIdleTTL
	}
	return sessionIdleTTL
}

func (s *Service) issueSessionFor(ctx context.Context, w http.ResponseWriter, r *http.Request, userID pgtype.UUID, ttl time.Duration) error {
	token, err := randomToken()
	if err != nil {
		return err
	}
	sum := sha256.Sum256([]byte(token))
	if _, err := s.queries.CreateSession(ctx, store.CreateSessionParams{
		UserID:    userID,
		TokenHash: sum[:],
		UserAgent: truncate(r.UserAgent(), maxUserAgent),
		Ip:        audit.ClientIP(r),
		ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(ttl), Valid: true},
	}); err != nil {
		return err
	}
	http.SetCookie(w, s.cookie(sessionCookie, token, int(ttl.Seconds())))
	return nil
}

// Authenticate resolves the session cookie to a live session, or nil.
//
// It takes the writer because a session that slides its deadline has to slide
// the browser's copy too; otherwise the cookie expires thirty days after the
// login that created it however active the person has been.
func (s *Service) Authenticate(w http.ResponseWriter, r *http.Request) *Session {
	c, err := r.Cookie(sessionCookie)
	if err != nil || c.Value == "" {
		return nil
	}
	sum := sha256.Sum256([]byte(c.Value))
	row, err := s.queries.GetSessionByHash(r.Context(), sum[:])
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			slog.ErrorContext(r.Context(), "session lookup", "err", err)
		}
		return nil
	}

	now := time.Now()
	if now.Sub(row.Session.CreatedAt.Time) > SessionAbsoluteTTL {
		if err := s.queries.DeleteSessionByHash(r.Context(), sum[:]); err != nil {
			slog.ErrorContext(r.Context(), "delete aged session", "err", err)
		}
		s.clearCookie(w, sessionCookie)
		return nil
	}
	if now.Sub(row.Session.LastUsedAt.Time) > sessionRefreshAfter {
		ttl := idleTTLFor(row.User)
		expires := now.Add(ttl)
		if err := s.queries.TouchSession(r.Context(), store.TouchSessionParams{
			ID:        row.Session.ID,
			ExpiresAt: pgtype.Timestamptz{Time: expires, Valid: true},
		}); err != nil {
			// A failed refresh is not a failed request: the session is valid
			// right now and the next request will try again.
			slog.ErrorContext(r.Context(), "touch session", "err", err)
		} else {
			http.SetCookie(w, s.cookie(sessionCookie, c.Value, int(ttl.Seconds())))
		}
	}
	return &Session{ID: row.Session.ID, User: row.User}
}

// HandleLogout revokes the current session and tells the caller where the
// IdP's own sign-out lives.
//
// Clearing the cookie alone left the IdP session standing, so "sign out"
// followed by "sign in" was a silent SSO re-entry — on a shared machine, as
// the previous person.
func (s *Service) HandleLogout(w http.ResponseWriter, r *http.Request) {
	endSession := s.EndSessionURL()
	if sess := s.Authenticate(w, r); sess != nil {
		s.audit.Record(r, audit.Event{
			Kind:    audit.KindLogout,
			UserID:  sess.User.ID,
			Subject: sess.User.Email,
		})
		// A guest never signed in at the provider, so there is no provider
		// session to end.
		if sess.User.Kind == "guest" {
			endSession = ""
		}
	}
	s.revokeCookieSession(r)
	s.clearCookie(w, sessionCookie)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"end_session_url": endSession})
}

// revokeCookieSession drops whatever session the request's cookie names, if
// any. Nothing to revoke is the ordinary case, not an error.
func (s *Service) revokeCookieSession(r *http.Request) {
	c, err := r.Cookie(sessionCookie)
	if err != nil || c.Value == "" {
		return
	}
	sum := sha256.Sum256([]byte(c.Value))
	if err := s.queries.DeleteSessionByHash(r.Context(), sum[:]); err != nil {
		slog.ErrorContext(r.Context(), "delete session", "err", err)
	}
}

// EndSessionURL is the provider's RP-initiated logout endpoint, or "" when the
// provider does not advertise one.
func (s *Service) EndSessionURL() string {
	if s.endSessionURL == "" {
		return ""
	}
	u, err := url.Parse(s.endSessionURL)
	if err != nil {
		return ""
	}
	q := u.Query()
	q.Set("post_logout_redirect_uri", s.cfg.ExternalURL)
	q.Set("client_id", s.cfg.OIDCClientID)
	u.RawQuery = q.Encode()
	return u.String()
}

// ListSessions returns the caller's live sessions, most recently used first.
func (s *Service) ListSessions(ctx context.Context, userID pgtype.UUID) ([]store.Session, error) {
	return s.queries.ListUserSessions(ctx, userID)
}

// RevokeSession drops one of the caller's own sessions. The user id is part of
// the predicate, so guessing another person's session id achieves nothing.
func (s *Service) RevokeSession(r *http.Request, user store.User, sessionID pgtype.UUID) (bool, error) {
	n, err := s.queries.DeleteUserSession(r.Context(), store.DeleteUserSessionParams{ID: sessionID, UserID: user.ID})
	if err == nil && n > 0 {
		s.audit.Record(r, audit.Event{
			Kind:    audit.KindSessionRevoked,
			UserID:  user.ID,
			Subject: user.Email,
			Detail:  map[string]any{"session": sessionID.String()},
		})
	}
	return n > 0, err
}

// RevokeOtherSessions signs the caller out everywhere except here.
func (s *Service) RevokeOtherSessions(r *http.Request, user store.User, keep pgtype.UUID) (int64, error) {
	n, err := s.queries.DeleteOtherUserSessions(r.Context(), store.DeleteOtherUserSessionsParams{
		UserID: user.ID, ID: keep,
	})
	if err == nil && n > 0 {
		s.audit.Record(r, audit.Event{
			Kind:    audit.KindSessionsRevoked,
			UserID:  user.ID,
			Subject: user.Email,
			Detail:  map[string]any{"revoked": n},
		})
	}
	return n, err
}

// PurgeExpiredSessions drops rows past their idle deadline. Expired sessions
// are already refused on lookup; this is housekeeping, not enforcement.
func (s *Service) PurgeExpiredSessions(ctx context.Context) (int64, error) {
	return s.queries.DeleteExpiredSessions(ctx)
}

// --- cookies and small helpers ---

func (s *Service) cookie(name, value string, maxAge int) *http.Cookie {
	return &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   s.secure,
		// Lax, not Strict: the OIDC callback is a cross-site top-level
		// navigation, and Strict would withhold the transaction cookie at
		// exactly the moment it is needed. It also keeps a link from an email
		// landing on a signed-in page rather than the login screen.
		SameSite: http.SameSiteLaxMode,
	}
}

func (s *Service) clearCookie(w http.ResponseWriter, name string) {
	http.SetCookie(w, s.cookie(name, "", -1))
}

// ClearSessionCookie drops the browser's copy of a session that has just been
// revoked out from under it.
func (s *Service) ClearSessionCookie(w http.ResponseWriter) {
	s.clearCookie(w, sessionCookie)
}

func (s *Service) readTx(r *http.Request) (oidcTx, bool) {
	var tx oidcTx
	c, err := r.Cookie(txCookie)
	if err != nil || c.Value == "" {
		return tx, false
	}
	blob, err := base64.RawURLEncoding.DecodeString(c.Value)
	if err != nil {
		return tx, false
	}
	if err := json.Unmarshal(blob, &tx); err != nil {
		return tx, false
	}
	return tx, true
}

// safeNext keeps only a same-origin absolute path, so the sign-in endpoint
// cannot be turned into an open redirect.
func safeNext(raw string) string {
	if raw == "" || !strings.HasPrefix(raw, "/") {
		return ""
	}
	// "//evil.example" is a protocol-relative URL, not a path — and so is
	// "/\evil.example", because browsers normalise a backslash to a forward
	// slash in the authority position even though net/url does not. A leading
	// "/" alone is therefore not enough.
	if strings.HasPrefix(raw, "//") || strings.HasPrefix(raw, `/\`) {
		return ""
	}
	if strings.ContainsAny(raw, "\r\n\x00") {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "" || u.Host != "" {
		return ""
	}
	return raw
}

// rebaseOrigin moves raw from one origin to another, leaving its path and
// query alone. Used to hand the browser an authorization URL it can actually
// reach when discovery ran against an internal hostname.
func rebaseOrigin(raw, from, to string) string {
	if raw == "" || from == to {
		return raw
	}
	fromU, err := url.Parse(from)
	if err != nil {
		return raw
	}
	toU, err := url.Parse(to)
	if err != nil {
		return raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != fromU.Scheme || u.Host != fromU.Host {
		return raw
	}
	u.Scheme, u.Host = toU.Scheme, toU.Host
	return u.String()
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}

func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
