package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/nhatbui/pp-hub/apps/server/internal/agenttools"
	"github.com/nhatbui/pp-hub/apps/server/internal/auth"
)

// The agent dispatcher (M41): agenttools calls pp-hub's REST API, and this
// answers those calls in process, through the same route table the network
// serves (apiRoutes). A tool therefore runs the very handler a click in the
// web client runs — the capability checks, the audit row, the notification —
// and nothing about access is written a second time for agents.
//
// What the network's /api/v1 group adds in front of that table does not
// apply here, deliberately: the same-origin check defends a cookie, and the
// caller here carries none; the session is the one the MCP middleware
// resolved from the bearer token, placed where caller(r) finds it.

// newAgentRouter mounts the API table, plus the agent-only routes, for the
// dispatcher. Built once per Server.
func (s *Server) newAgentRouter() http.Handler {
	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) { s.apiRoutes(r, true) })
	return r
}

// agentAPI is agenttools.API for one MCP request: its caller, and the
// request whose provenance (address, user agent, request id, the agent
// marker) every inner call carries into the audit log.
type agentAPI struct {
	router http.Handler
	outer  *http.Request
	sess   *auth.Session
}

var _ agenttools.API = agentAPI{}

// Do serves one API call through the router and buffers the answer.
func (a agentAPI) Do(ctx context.Context, method, path string, body any) (res agenttools.Response, err error) {
	var reader io.Reader = http.NoBody
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return agenttools.Response{}, err
		}
		reader = bytes.NewReader(encoded)
	}
	// The outer request's context carries the request id, the trusted client
	// address and the agent marker; ctx, the tool's, only adds cancellation
	// for a call the client gave up on.
	inner, cancel := context.WithCancel(a.outer.Context())
	defer cancel()
	stop := context.AfterFunc(ctx, cancel)
	defer stop()

	req, err := innerRequest(inner, a.outer, a.sess, method, path, reader)
	if err != nil {
		return agenttools.Response{}, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	// A panic in a handler is answered as a 500 here: the tool runs on the
	// MCP SDK's goroutine, outside the router's recoverer.
	defer func() {
		if p := recover(); p != nil {
			slog.ErrorContext(req.Context(), "agent call panicked", "method", method, "path", path, "panic", p)
			res, err = agenttools.Response{}, fmt.Errorf("internal error")
		}
	}()
	rec := &bufferedResponse{header: http.Header{}}
	a.router.ServeHTTP(rec, req)
	if rec.status == 0 {
		rec.status = http.StatusOK
	}
	return agenttools.Response{Status: rec.status, Body: rec.body.Bytes()}, nil
}

// innerRequest is a call to /api/v1 that the agent router answers in
// process, as sess. It carries outer's provenance — address, user agent and,
// through ctx (outer's context or one derived from it), the request id and
// the agent marker — into the audit log.
func innerRequest(ctx context.Context, outer *http.Request, sess *auth.Session, method, path string, body io.Reader) (*http.Request, error) {
	inner := context.WithValue(ctx, sessionCtxKey, sess)
	// The outer request's routing state would make the router resume
	// matching where the outer route left off rather than route this path
	// afresh.
	inner = context.WithValue(inner, chi.RouteCtxKey, (*chi.Context)(nil))
	req, err := http.NewRequestWithContext(inner, method, "/api/v1"+path, body)
	if err != nil {
		return nil, err
	}
	req.RemoteAddr = outer.RemoteAddr
	req.Header.Set("User-Agent", outer.UserAgent())
	return req, nil
}

// bufferedResponse is the http.ResponseWriter a dispatched call writes to.
type bufferedResponse struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (b *bufferedResponse) Header() http.Header { return b.header }

func (b *bufferedResponse) WriteHeader(status int) {
	if b.status == 0 {
		b.status = status
	}
}

func (b *bufferedResponse) Write(p []byte) (int, error) {
	if b.status == 0 {
		b.status = http.StatusOK
	}
	return b.body.Write(p)
}
