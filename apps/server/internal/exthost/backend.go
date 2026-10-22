package exthost

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// A packaged app's server logic runs in its author's own service, never here
// (M51). The host proxies the frame's backend calls to it with a token naming
// who is asking, about which node, with which capabilities; the backend may
// call back into the record store, broadcast and notify with that same token,
// and is re-authorized live on every call.

const (
	// TokenTTL is how long a backend token is good for.
	TokenTTL = 5 * time.Minute
	// BackendTimeout bounds one proxied call.
	BackendTimeout = 30 * time.Second
	// maxBackendRequest and maxBackendResponse bound what crosses the proxy.
	maxBackendRequest  = 1 << 20
	maxBackendResponse = 10 << 20
)

// TokenClaims is the payload of a backend token (contract §5).
type TokenClaims struct {
	Iss    string `json:"iss"`
	Aud    string `json:"aud"`
	Sub    string `json:"sub"`
	Iat    int64  `json:"iat"`
	Exp    int64  `json:"exp"`
	Jti    string `json:"jti"`
	Node   string `json:"node"`
	Space  string `json:"space"`
	Kind   string `json:"kind"`
	Caps   Caps   `json:"caps"`
	Name   string `json:"name"`
	Email  string `json:"email"`
	Locale string `json:"locale,omitempty"`
}

// Audience is the aud of an app's tokens.
func Audience(appID string) string { return "pp-app:" + appID }

var jwtHeader = base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))

// SignToken is an HS256 JWT over claims. The key is the secret's text as the
// administrator copied it, which is what a JWT library given a string uses.
func SignToken(secret string, c TokenClaims) (string, error) {
	payload, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	signing := jwtHeader + "." + base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(signing))
	return signing + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

var errBadToken = errors.New("invalid app token")

// VerifyToken checks a token's signature, algorithm, audience and expiry.
func VerifyToken(secret, token, appID string, now time.Time) (TokenClaims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 || secret == "" {
		return TokenClaims{}, errBadToken
	}
	head, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return TokenClaims{}, errBadToken
	}
	var h struct {
		Alg string `json:"alg"`
	}
	if json.Unmarshal(head, &h) != nil || h.Alg != "HS256" {
		return TokenClaims{}, errBadToken
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return TokenClaims{}, errBadToken
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(parts[0] + "." + parts[1]))
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return TokenClaims{}, errBadToken
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return TokenClaims{}, errBadToken
	}
	var c TokenClaims
	if json.Unmarshal(payload, &c) != nil {
		return TokenClaims{}, errBadToken
	}
	if c.Aud != Audience(appID) || now.Unix() >= c.Exp {
		return TokenClaims{}, errBadToken
	}
	return c, nil
}

// tokenFor mints the token for a request the session guard admitted.
func (r *Registry) tokenFor(req *http.Request, rq appReq) (string, error) {
	u := r.caller(req)
	jti := make([]byte, 12)
	if _, err := rand.Read(jti); err != nil {
		return "", err
	}
	now := time.Now()
	return SignToken(rq.e.secret, TokenClaims{
		Iss: r.cfg.ExternalURL, Aud: Audience(rq.e.m.ID), Sub: uuidString(u.ID),
		Iat: now.Unix(), Exp: now.Add(TokenTTL).Unix(), Jti: hex.EncodeToString(jti),
		Node: uuidString(rq.node.ID), Space: uuidString(rq.node.SpaceID),
		Kind: rq.node.ExtKind.String, Caps: rq.caps,
		Name: displayName(u.DisplayName, u.Name, u.Email), Email: u.Email,
		Locale: req.Header.Get("Accept-Language"),
	})
}

func (r *Registry) backendClient() *http.Client {
	if r.cfg.BackendClient != nil {
		return r.cfg.BackendClient
	}
	return &http.Client{
		Timeout: BackendTimeout,
		// A redirect would carry the token somewhere the administrator did
		// not name; the frame gets the 3xx instead.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// forwardRequestHeaders and forwardResponseHeaders are what crosses the
// proxy; cookies and credentials never do.
var (
	forwardRequestHeaders  = []string{"Content-Type", "Accept", "Accept-Language", "If-Match", "If-None-Match", "If-Modified-Since"}
	forwardResponseHeaders = []string{"Content-Type", "Cache-Control", "ETag", "Last-Modified", "Content-Language", "Content-Disposition", "Location"}
)

// backendTarget joins the backend's base URL and the path after /backend.
func backendTarget(base, rest, rawQuery string) (string, bool) {
	for _, seg := range strings.Split(rest, "/") {
		if seg == ".." || seg == "." {
			return "", false
		}
	}
	u, err := url.Parse(base)
	if err != nil {
		return "", false
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/" + strings.TrimLeft(rest, "/")
	u.RawQuery = rawQuery
	return u.String(), true
}

// handleBackendProxy is ANY /api/v1/apps/{id}/nodes/{node}/backend/<path>.
func (r *Registry) handleBackendProxy(w http.ResponseWriter, req *http.Request) {
	rq := appRequest(req)
	base := rq.e.backend()
	if base == "" {
		writeProblem(w, http.StatusNotFound, "this app has no backend")
		return
	}
	if rq.e.secret == "" {
		writeProblem(w, http.StatusServiceUnavailable, "this app's backend is not set up: an administrator has to generate its secret")
		return
	}
	target, ok := backendTarget(base, chi.URLParam(req, "*"), req.URL.RawQuery)
	if !ok {
		writeProblem(w, http.StatusBadRequest, "invalid backend path")
		return
	}
	token, err := r.tokenFor(req, rq)
	if err != nil {
		fail(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(req.Context(), BackendTimeout)
	defer cancel()
	var body io.Reader
	if req.Body != nil && req.Method != http.MethodGet && req.Method != http.MethodHead {
		body = http.MaxBytesReader(w, req.Body, maxBackendRequest)
	}
	out, err := http.NewRequestWithContext(ctx, req.Method, target, body)
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "invalid backend request")
		return
	}
	for _, h := range forwardRequestHeaders {
		if v := req.Header.Get(h); v != "" {
			out.Header.Set(h, v)
		}
	}
	out.Header.Set("Authorization", "Bearer "+token)
	out.Header.Set("X-PP-App", rq.e.m.ID)
	out.Header.Set("X-PP-Node", uuidString(rq.node.ID))
	out.Header.Set("X-PP-User", uuidString(rq.user))
	if id := req.Header.Get("X-Request-Id"); id != "" {
		out.Header.Set("X-Request-Id", id)
	}
	res, err := r.backendClient().Do(out)
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeProblem(w, http.StatusRequestEntityTooLarge, "a backend request carries at most 1 MiB")
			return
		}
		slog.WarnContext(req.Context(), "app backend unreachable", "app", rq.e.m.ID, "err", err)
		writeProblem(w, http.StatusBadGateway, "the app's backend did not answer")
		return
	}
	defer res.Body.Close()
	for _, h := range forwardResponseHeaders {
		if v := res.Header.Get(h); v != "" {
			w.Header().Set(h, v)
		}
	}
	w.WriteHeader(res.StatusCode)
	_, _ = io.Copy(w, io.LimitReader(res.Body, maxBackendResponse))
}

// MountBackendAPI adds the routes an app's backend calls back with a token,
// at /api/apps/{appID}/nodes/{nodeID}. They are outside /api/v1 because the
// caller is a server with a bearer token, not a browser with a session: the
// same-origin check and the cookie do not apply, as for /mcp.
func (r *Registry) MountBackendAPI(rt chi.Router) {
	rt.Route("/api/apps/{appID}/nodes/{nodeID}", func(rt chi.Router) {
		rt.Group(func(rt chi.Router) {
			rt.Use(r.tokenGuard(CapView))
			r.readRoutes(rt)
		})
		rt.Group(func(rt chi.Router) {
			rt.Use(r.tokenGuard(CapEdit))
			r.writeRoutes(rt)
		})
	})
}

// tokenGuard admits a backend's call: a valid token for this app and this
// node, whose subject still holds need on it, is not suspended, and the app
// still on.
func (r *Registry) tokenGuard(need Cap) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			appID := chi.URLParam(req, "appID")
			e := r.lookup(appID)
			raw, ok := strings.CutPrefix(req.Header.Get("Authorization"), "Bearer ")
			if e == nil || !ok {
				w.Header().Set("WWW-Authenticate", `Bearer realm="pp-app"`)
				writeProblem(w, http.StatusUnauthorized, errBadToken.Error())
				return
			}
			claims, err := VerifyToken(e.secret, strings.TrimSpace(raw), appID, time.Now())
			if err != nil || claims.Node != chi.URLParam(req, "nodeID") {
				w.Header().Set("WWW-Authenticate", `Bearer realm="pp-app", error="invalid_token"`)
				writeProblem(w, http.StatusUnauthorized, errBadToken.Error())
				return
			}
			nodeID, err1 := parseUUID(claims.Node)
			userID, err2 := parseUUID(claims.Sub)
			if err1 != nil || err2 != nil {
				writeProblem(w, http.StatusUnauthorized, errBadToken.Error())
				return
			}
			if !r.userActive(req.Context(), userID) {
				writeProblem(w, http.StatusForbidden, "this account is suspended")
				return
			}
			rq, no := r.authorizeApp(req.Context(), appID, nodeID, userID, need)
			if no != nil {
				writeProblem(w, no.status, no.detail)
				return
			}
			next.ServeHTTP(w, req.WithContext(context.WithValue(req.Context(), appReqKey{}, rq)))
		})
	}
}

// userActive reports whether an account exists and is not suspended.
func (r *Registry) userActive(ctx context.Context, id pgtype.UUID) bool {
	u, err := r.cfg.Store.GetUser(ctx, id)
	return err == nil && !u.SuspendedAt.Valid
}
