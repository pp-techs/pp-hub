package httpapi

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
)

// maxWebhookBody bounds what an unauthenticated caller can make this process
// read. A membership delta is a few hundred bytes; a megabyte is already two
// orders of magnitude of headroom.
const maxWebhookBody = 1 << 20

// handleLogtoWebhook accepts an Organization.Membership.Updated delivery.
//
// It is mounted outside /api/v1 on purpose. Everything under that prefix
// carries the session and same-origin middlewares, and TestEveryAPIRouteIsAuthenticated
// exists to keep it that way; adding an unauthenticated route inside the
// group would mean loosening the test that protects the whole API surface.
// Authentication here is the signature instead.
//
// The handler does no work beyond verifying and enqueuing. Logto treats a
// slow endpoint as a failed delivery, and the work this describes is a round
// trip back to Logto — so it becomes a job and the response is immediate.
//
// The delivery's own addedUserIds / removedUserIds are deliberately ignored.
// Deliveries carry no ordering guarantee, so applying them as deltas would
// let a stale "removed" land after a fresh "added". The job re-reads the
// organization and converges instead.
func (s *Server) handleLogtoWebhook(w http.ResponseWriter, r *http.Request) {
	if s.webhookKey == "" {
		// An unsigned endpoint that mutates membership is worse than no
		// endpoint: the hourly reconcile still converges without it.
		writeProblem(w, http.StatusNotFound, "Not Found", "webhook endpoint is not enabled")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxWebhookBody))
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Error", "could not read body")
		return
	}
	// Over the raw bytes, before any parse: Logto signs what it sent, and a
	// re-encoded body is a different string.
	mac := hmac.New(sha256.New, []byte(s.webhookKey))
	mac.Write(body)
	want := hex.EncodeToString(mac.Sum(nil))
	got := r.Header.Get("logto-signature-sha-256")
	if !hmac.Equal([]byte(want), []byte(got)) {
		slog.Warn("rejected webhook delivery with a bad signature", "ip", r.RemoteAddr)
		writeProblem(w, http.StatusUnauthorized, "Unauthorized", "signature did not match")
		return
	}

	var payload struct {
		Event          string `json:"event"`
		OrganizationID string `json:"organizationId"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		writeProblem(w, http.StatusBadRequest, "Error", "body is not JSON")
		return
	}
	if payload.OrganizationID == "" {
		// Some other event is subscribed to this endpoint. Acknowledge it —
		// a 4xx would make Logto retry something that will never apply.
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := s.orgs.Enqueue(r.Context(), payload.OrganizationID); err != nil {
		// Failing the delivery is right here: Logto retries, and the
		// alternative is losing the change until the hourly sweep.
		Error(w, r, err)
		return
	}
	slog.Info("organization reconcile queued", "event", payload.Event, "org", payload.OrganizationID)
	w.WriteHeader(http.StatusNoContent)
}
