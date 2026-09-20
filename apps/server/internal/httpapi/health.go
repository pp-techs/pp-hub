package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"time"
)

// handleReadyz reports whether this process can actually serve.
//
// /healthz stays a constant 200 — it answers "is the process up", which is
// what a restart policy needs. Readiness is the other question, and the one
// both healthchecks were asking by accident: a container that cannot reach
// Postgres reported healthy, and compose kept routing to it.
//
// The body names no dependency. This handler is not proxied by nginx, but the
// detail belongs in the log either way, where the operator reading it is
// already authenticated by having a shell.
func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	if s.pool == nil || s.blobs == nil {
		writeProblem(w, http.StatusServiceUnavailable, "Not Ready", "dependencies not configured")
		return
	}
	if err := s.pool.Ping(ctx); err != nil {
		slog.ErrorContext(ctx, "readiness: database unreachable", "err", err)
		writeProblem(w, http.StatusServiceUnavailable, "Not Ready", "a dependency is unavailable")
		return
	}
	if err := s.blobs.Check(ctx); err != nil {
		slog.ErrorContext(ctx, "readiness: blob store unusable", "err", err)
		writeProblem(w, http.StatusServiceUnavailable, "Not Ready", "a dependency is unavailable")
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ready"))
}
