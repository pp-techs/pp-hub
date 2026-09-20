// Package audit records the events that change who can reach what: sign-ins
// and their failures, session revocations, admin and suspension changes, and
// grants given or taken away.
//
// Distinct from internal/activity, which is a per-space feed of work for
// colleagues to read. This is a per-instance record of access for whoever has
// to answer "who got in, from where, and when did that permission appear".
// The two differ in their schema (no space, a nullable subject, request
// context), in who reads them, and in what a missing row costs.
//
// Recording is best-effort in the same way activity is: the event describes
// something that already happened, and failing a sign-in because its audit
// line could not be written would trade the real outcome for the record of
// it. Failures are logged at error level rather than warn, because unlike an
// activity row, a gap here is a gap in an audit trail.
package audit

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

// Kind is the verb. Text rather than an enum so that adding one is a code
// change rather than a migration.
type Kind string

const (
	KindLogin       Kind = "login"
	KindLoginFailed Kind = "login_failed"
	KindLogout      Kind = "logout"
	// A session ended from the Sessions tab rather than by signing out here.
	KindSessionRevoked Kind = "session_revoked"
	// "Sign out everywhere else."
	KindSessionsRevoked Kind = "sessions_revoked"
	// An identity whose (issuer, subject) changed and was matched back to an
	// existing account by verified email. Rare and worth seeing.
	KindIdentityAdopted Kind = "identity_adopted"
	KindAdminGranted    Kind = "admin_granted"
	KindAdminRevoked    Kind = "admin_revoked"
	KindSuspended       Kind = "account_suspended"
	KindUnsuspended     Kind = "account_unsuspended"
	KindGrantCreated    Kind = "grant_created"
	KindGrantRevoked    Kind = "grant_revoked"
	KindShareCreated    Kind = "share_created"
	KindShareRevoked    Kind = "share_revoked"
	KindMemberAdded     Kind = "member_added"
	KindMemberRemoved   Kind = "member_removed"
)

// maxUserAgent bounds what the client controls before it reaches a TEXT
// column.
const maxUserAgent = 400

type Service struct {
	q *store.Queries
}

func New(q *store.Queries) *Service { return &Service{q: q} }

// Event is one thing that happened.
type Event struct {
	Kind Kind
	// UserID may be zero: a sign-in that failed before an account was
	// resolved has no user to name.
	UserID pgtype.UUID
	// Subject is who the caller claimed to be when UserID is zero, and a
	// durable label once the account is gone.
	Subject string
	Detail  map[string]any
}

// Record writes the event with the request's context attached.
func (s *Service) Record(r *http.Request, e Event) {
	detail := []byte("{}")
	if len(e.Detail) > 0 {
		if encoded, err := json.Marshal(e.Detail); err == nil {
			detail = encoded
		}
	}
	// A cancelled request must not lose its audit row — the caller
	// disconnecting is exactly when one matters — so the write does not
	// inherit the request's cancellation.
	ctx := context.WithoutCancel(r.Context())
	err := s.q.RecordSecurityEvent(ctx, store.RecordSecurityEventParams{
		Kind:      string(e.Kind),
		UserID:    e.UserID,
		Subject:   e.Subject,
		Ip:        ClientIP(r),
		UserAgent: truncate(r.UserAgent(), maxUserAgent),
		RequestID: middleware.GetReqID(r.Context()),
		Detail:    detail,
	})
	if err != nil {
		slog.Error("security event not recorded", "kind", e.Kind, "err", err)
	}
}

// Purge drops rows older than the cutoff.
func (s *Service) Purge(ctx context.Context, before pgtype.Timestamptz) (int64, error) {
	return s.q.DeleteSecurityEventsBefore(ctx, before)
}

// ClientIP is the caller's address: the one a trusted proxy reported through
// X-Forwarded-For, or the TCP peer when there is no trusted proxy in front.
//
// It lives here because it is request provenance, and because the two
// packages that record it — this one and internal/auth — both need it while
// neither may import the HTTP edge.
//
// It deliberately does not read the headers itself. chi's RealIP used to do
// that unconditionally, from any source, which made the caller's address the
// caller's choice: rate-limit buckets keyed on it were free to reset, and the
// address shown on a session row was whatever an attacker wanted the victim
// to see. middleware.ClientIPFromXFF only sets a value when a configured
// trusted proxy vouched for it, and sets none at all otherwise — so an empty
// result here means "believe the socket", not "believe the header".
func ClientIP(r *http.Request) string {
	if ip := middleware.GetClientIP(r.Context()); ip != "" {
		return ip
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}
