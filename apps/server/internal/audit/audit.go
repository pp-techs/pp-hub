// Package audit records the events that change who can reach what: sign-ins
// and their failures, session revocations, admin and suspension changes, and
// grants given or taken away — and, since M26, who read which file.
//
// Distinct from internal/activity, which is a per-space feed of work for
// colleagues to read. This is a per-instance record of access for whoever has
// to answer "who got in, from where, and when did that permission appear".
// The two differ in their schema (no space, a nullable subject, request
// context), in who reads them, and in what a missing row costs.
//
// Read events (downloads, folder zips, inline views, editor opens) are the
// high-volume kind, so they are deduplicated in memory: one row per (kind,
// actor, node) per ReadDedupWindow, per replica — N replicas can write up to
// N rows for the same window. They also have their own retention, separate
// from the rest of the log.
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
	"maps"
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
	// A personal access token for agents was minted or revoked (M41).
	KindAgentTokenCreated Kind = "agent_token_created"
	KindAgentTokenRevoked Kind = "agent_token_revoked"
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
	// An address the identity provider was asked to invite into the space's
	// organization. It has no user_id: the point of an invitation is that
	// there may be no account to point at yet.
	KindMemberInvited   Kind = "member_invited"
	KindSettingsUpdated Kind = "settings_updated"
	// Groups decide who a group grant reaches, so changing one is changing
	// access — and an admin adding somebody to a group is the one way space
	// administration turns into file access.
	KindGroupCreated       Kind = "group_created"
	KindGroupUpdated       Kind = "group_updated"
	KindGroupDeleted       Kind = "group_deleted"
	KindGroupMemberAdded   Kind = "group_member_added"
	KindGroupMemberRemoved Kind = "group_member_removed"

	// Reads. Only intentional ones: thumbnails, the editor's own fetch of
	// the bytes and Range continuations are not separate events.
	KindFileDownloaded   Kind = "file_downloaded"
	KindFolderDownloaded Kind = "folder_downloaded"
	KindFileViewed       Kind = "file_viewed"
	KindFileOpened       Kind = "file_opened"

	// Raised by internal/alert, not by a request.
	KindSecurityAlert       Kind = "security_alert"
	KindSecurityLogExported Kind = "security_log_exported"
)

// ReadKinds are the kinds retained under the read-audit retention rather than
// the general one.
var ReadKinds = []Kind{KindFileDownloaded, KindFolderDownloaded, KindFileViewed, KindFileOpened}

func readKindStrings() []string {
	out := make([]string, len(ReadKinds))
	for i, k := range ReadKinds {
		out[i] = string(k)
	}
	return out
}

// LinkSubject is the subject of a read made through a share link, where there
// is no account to name.
const LinkSubject = "share_link"

// maxUserAgent bounds what the client controls before it reaches a TEXT
// column.
const maxUserAgent = 400

type Service struct {
	q     *store.Queries
	reads *readCache
}

func New(q *store.Queries) *Service { return &Service{q: q, reads: newReadCache(readDedupMax)} }

// Event is one thing that happened.
type Event struct {
	Kind Kind
	// UserID may be zero: a sign-in that failed before an account was
	// resolved has no user to name.
	UserID pgtype.UUID
	// Subject is who the caller claimed to be when UserID is zero, and a
	// durable label once the account is gone.
	Subject string
	// NodeID is the file or folder the event concerns; zero for none.
	NodeID pgtype.UUID
	Detail map[string]any
}

// agentKey carries the agent a request acts through (WithAgent).
type agentKey struct{}

// WithAgent marks ctx as a request an AI agent made on its user's behalf
// (M41), clientID being the OAuth client that holds the token — for a CIMD
// client, the URL of its metadata document. Every event recorded under it
// says so, so "who did this" can tell a person from their agent.
func WithAgent(ctx context.Context, clientID string) context.Context {
	return context.WithValue(ctx, agentKey{}, clientID)
}

// Agent is the agent client ctx was marked with, and whether there is one.
func Agent(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(agentKey{}).(string)
	return id, ok
}

// Record writes the event with the request's context attached.
func (s *Service) Record(r *http.Request, e Event) {
	if s == nil || s.q == nil {
		return
	}
	// A cancelled request must not lose its audit row — the caller
	// disconnecting is exactly when one matters — so the write does not
	// inherit the request's cancellation.
	ctx := context.WithoutCancel(r.Context())
	detail := e.Detail
	if client, ok := Agent(ctx); ok {
		detail = maps.Clone(detail)
		if detail == nil {
			detail = map[string]any{}
		}
		detail["via"] = "mcp"
		detail["client_id"] = client
	}
	err := s.q.RecordSecurityEvent(ctx, store.RecordSecurityEventParams{
		Kind:      string(e.Kind),
		UserID:    e.UserID,
		Subject:   e.Subject,
		Ip:        ClientIP(r),
		UserAgent: truncate(r.UserAgent(), maxUserAgent),
		RequestID: middleware.GetReqID(r.Context()),
		Detail:    encodeDetail(detail),
		NodeID:    e.NodeID,
	})
	if err != nil {
		slog.ErrorContext(ctx, "security event not recorded", "kind", e.Kind, "err", err)
	}
}

// RecordSystem writes an event no request caused, attributed to ip — the
// address it is about, empty when there is none. Node and request context
// stay empty; the user is kept when the event names one.
func (s *Service) RecordSystem(ctx context.Context, e Event, ip string) {
	if s == nil || s.q == nil {
		return
	}
	ctx = context.WithoutCancel(ctx)
	err := s.q.RecordSystemSecurityEvent(ctx, store.RecordSystemSecurityEventParams{
		Kind:    string(e.Kind),
		UserID:  e.UserID,
		Subject: e.Subject,
		Ip:      ip,
		Detail:  encodeDetail(e.Detail),
	})
	if err != nil {
		slog.ErrorContext(ctx, "security event not recorded", "kind", e.Kind, "err", err)
	}
}

func encodeDetail(d map[string]any) []byte {
	if len(d) > 0 {
		if encoded, err := json.Marshal(d); err == nil {
			return encoded
		}
	}
	return []byte("{}")
}

// Purge drops rows older than their cutoff: before for the log in general,
// readsBefore for read events, which have their own retention.
func (s *Service) Purge(ctx context.Context, before, readsBefore pgtype.Timestamptz) (int64, error) {
	kinds := readKindStrings()
	n, err := s.q.DeleteSecurityEventsBefore(ctx, store.DeleteSecurityEventsBeforeParams{Before: before, ReadKinds: kinds})
	if err != nil {
		return 0, err
	}
	m, err := s.q.DeleteReadEventsBefore(ctx, store.DeleteReadEventsBeforeParams{Before: readsBefore, ReadKinds: kinds})
	return n + m, err
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
