// Package notifications is the in-app half of M29: a row per person told about
// a comment, a reply or a mention, the list and unread count they read it
// from, and the live stream that pushes a new row to their open tabs.
//
// A comment's notifications are written in the comment's own transaction (the
// comments module calls Write with its transaction's queries) and delivered to
// open streams once it commits. A mention written into a markdown document is
// found after the fact by the notify:mentions job on the version hook
// (mentions.go).
package notifications

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"log/slog"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nhatbui/pp-hub/apps/server/internal/share"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

// Kind is what a notification is about.
type Kind string

const (
	// KindComment: somebody opened a thread on a document you own.
	KindComment Kind = "comment"
	// KindReply: somebody answered in a thread you wrote in, or on a
	// document you own.
	KindReply Kind = "reply"
	// KindMention: somebody named you in a comment or in a document.
	KindMention Kind = "mention"
)

// ReadRetention is how long a notification is kept after it was read. The
// server's sweep purges older read rows; unread ones are kept.
const ReadRetention = 30 * 24 * time.Hour

// DefaultPage and MaxPage bound one page of the list.
const (
	DefaultPage = 20
	MaxPage     = 100
)

// excerptRunes bounds the text a notification quotes.
const excerptRunes = 140

// ErrBadCursor refuses a cursor this package did not mint.
var ErrBadCursor = errors.New("invalid cursor")

// Item is one notification as its recipient sees it.
type Item struct {
	ID        pgtype.UUID
	UserID    pgtype.UUID
	Kind      Kind
	Actor     store.User
	NodeID    pgtype.UUID
	NodeName  string
	SpaceID   pgtype.UUID
	ThreadID  pgtype.UUID
	CommentID pgtype.UUID
	Excerpt   string
	// Subject is the part of an extension node it is about ("card:<uuid>");
	// empty for a document.
	Subject   string
	CreatedAt time.Time
	ReadAt    pgtype.Timestamptz
}

// New is one notification to write.
type New struct {
	UserID    pgtype.UUID
	Kind      Kind
	ThreadID  pgtype.UUID
	CommentID pgtype.UUID
	Excerpt   string
	// Subject is set by an extension's notification, which has no thread.
	Subject string
}

// Access resolves what a user may do with nodes (share.Service).
type Access interface {
	EffectiveCapsForNode(ctx context.Context, node store.Node, userID pgtype.UUID) (share.Caps, error)
	AccessCaps(ctx context.Context, nodeIDs []pgtype.UUID, userID pgtype.UUID) (map[pgtype.UUID]share.Resolved, error)
}

// Blobs reads a version's bytes (blob.Store).
type Blobs interface {
	Get(ctx context.Context, hash string) (io.ReadCloser, error)
}

// Service writes, lists and streams notifications, and runs the document
// mention job.
type Service struct {
	q      *store.Queries
	blobs  Blobs
	access Access
	broker *Broker
	log    *slog.Logger
}

// NewService builds the service. Its broker is the process's: the HTTP
// streams and every writer must share one Service.
func NewService(q *store.Queries, blobs Blobs, access Access) *Service {
	return &Service{q: q, blobs: blobs, access: access, broker: NewBroker(),
		log: slog.Default().With("module", "notifications")}
}

// Subscribe opens a live stream of user's new notifications.
func (s *Service) Subscribe(user pgtype.UUID) (*Subscription, error) {
	return s.broker.Subscribe(user)
}

// Shutdown ends every open stream and refuses new ones (Broker.Shutdown).
func (s *Service) Shutdown() { s.broker.Shutdown() }

// Readers keeps the candidates who can open node — an active account holding
// view on it, which is what opening a document takes — in their first
// order, each once. A notification quotes the document's comments or text,
// so nobody who could not read them is told.
func (s *Service) Readers(ctx context.Context, node store.Node, candidates []pgtype.UUID) ([]pgtype.UUID, error) {
	if len(candidates) == 0 {
		return nil, nil
	}
	users, err := s.q.ListUsersByIDs(ctx, candidates)
	if err != nil {
		return nil, err
	}
	active := make(map[pgtype.UUID]bool, len(users))
	for _, u := range users {
		active[u.ID] = !u.SuspendedAt.Valid
	}
	var out []pgtype.UUID
	seen := make(map[pgtype.UUID]bool, len(candidates))
	for _, id := range candidates {
		if seen[id] || !active[id] {
			continue
		}
		seen[id] = true
		caps, err := s.access.EffectiveCapsForNode(ctx, node, id)
		if err != nil {
			return nil, err
		}
		if caps.View {
			out = append(out, id)
		}
	}
	return out, nil
}

// Write stores notes about node by actor through q, which is the caller's
// transaction, and returns their ids for Deliver once it has committed.
func (s *Service) Write(ctx context.Context, q *store.Queries, actor, node pgtype.UUID, notes []New) ([]pgtype.UUID, error) {
	ids := make([]pgtype.UUID, 0, len(notes))
	for _, n := range notes {
		id, err := q.CreateNotification(ctx, store.CreateNotificationParams{
			UserID:    n.UserID,
			Kind:      string(n.Kind),
			ActorID:   actor,
			NodeID:    node,
			ThreadID:  n.ThreadID,
			CommentID: n.CommentID,
			Excerpt:   n.Excerpt,
			Subject:   pgtype.Text{String: n.Subject, Valid: n.Subject != ""},
		})
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// Deliver pushes committed notifications to their recipients' open streams.
// Best-effort: the rows are stored whatever happens here, and a failure only
// means they show up on the next list instead of at once.
func (s *Service) Deliver(ctx context.Context, ids []pgtype.UUID) {
	if len(ids) == 0 {
		return
	}
	rows, err := s.q.GetNotificationsByIDs(context.WithoutCancel(ctx), ids)
	if err != nil {
		s.log.WarnContext(ctx, "deliver notifications", "err", err)
		return
	}
	for _, r := range rows {
		s.broker.Publish(Item{
			ID: r.ID, UserID: r.UserID, Kind: Kind(r.Kind), Actor: r.User,
			NodeID: r.NodeID, NodeName: r.NodeName, SpaceID: r.SpaceID,
			ThreadID: r.ThreadID, CommentID: r.CommentID, Excerpt: r.Excerpt, Subject: r.Subject,
			CreatedAt: r.CreatedAt.Time, ReadAt: r.ReadAt,
		})
	}
}

// List returns a page of user's notifications, newest first, and the cursor of
// the next page ("" on the last). Notifications about a document the user can
// no longer open, or one in the trash, are left out.
func (s *Service) List(ctx context.Context, user pgtype.UUID, cursor string, limit int) ([]Item, string, error) {
	if limit <= 0 {
		limit = DefaultPage
	}
	limit = min(limit, MaxPage)
	params := store.ListNotificationsParams{UserID: user, Lim: int32(limit)}
	if cursor != "" {
		at, id, err := decodeCursor(cursor)
		if err != nil {
			return nil, "", err
		}
		params.BeforeAt = pgtype.Timestamptz{Time: at, Valid: true}
		params.BeforeID = id
	}
	rows, err := s.q.ListNotifications(ctx, params)
	if err != nil {
		return nil, "", err
	}
	nodes := make([]pgtype.UUID, 0, len(rows))
	for _, r := range rows {
		nodes = append(nodes, r.NodeID)
	}
	readable, err := s.readable(ctx, user, nodes)
	if err != nil {
		return nil, "", err
	}
	items := make([]Item, 0, len(rows))
	for _, r := range rows {
		if !readable[r.NodeID] {
			continue
		}
		items = append(items, Item{
			ID: r.ID, UserID: r.UserID, Kind: Kind(r.Kind), Actor: r.User,
			NodeID: r.NodeID, NodeName: r.NodeName, SpaceID: r.SpaceID,
			ThreadID: r.ThreadID, CommentID: r.CommentID, Excerpt: r.Excerpt, Subject: r.Subject,
			CreatedAt: r.CreatedAt.Time, ReadAt: r.ReadAt,
		})
	}
	next := ""
	if len(rows) == limit {
		last := rows[len(rows)-1]
		next = encodeCursor(last.CreatedAt.Time, last.ID)
	}
	return items, next, nil
}

// UnreadCount counts user's unread notifications the list would show.
func (s *Service) UnreadCount(ctx context.Context, user pgtype.UUID) (int64, error) {
	rows, err := s.q.CountUnreadNotificationsByNode(ctx, user)
	if err != nil {
		return 0, err
	}
	nodes := make([]pgtype.UUID, 0, len(rows))
	for _, r := range rows {
		nodes = append(nodes, r.NodeID)
	}
	readable, err := s.readable(ctx, user, nodes)
	if err != nil {
		return 0, err
	}
	var n int64
	for _, r := range rows {
		if readable[r.NodeID] {
			n += r.Unread
		}
	}
	return n, nil
}

// MarkRead marks user's notifications read: the given ids, or every unread one
// when all is set. Ids that are not user's are ignored.
func (s *Service) MarkRead(ctx context.Context, user pgtype.UUID, ids []pgtype.UUID, all bool) (int64, error) {
	if all {
		return s.q.MarkAllNotificationsRead(ctx, user)
	}
	if len(ids) == 0 {
		return 0, nil
	}
	return s.q.MarkNotificationsRead(ctx, store.MarkNotificationsReadParams{UserID: user, Ids: ids})
}

// Purge deletes notifications read before cutoff.
func (s *Service) Purge(ctx context.Context, cutoff time.Time) (int64, error) {
	return s.q.PurgeReadNotifications(ctx, pgtype.Timestamptz{Time: cutoff, Valid: true})
}

// readable reports, per node, whether user may open it (view).
func (s *Service) readable(ctx context.Context, user pgtype.UUID, nodes []pgtype.UUID) (map[pgtype.UUID]bool, error) {
	out := make(map[pgtype.UUID]bool, len(nodes))
	if len(nodes) == 0 {
		return out, nil
	}
	res, err := s.access.AccessCaps(ctx, nodes, user)
	if err != nil {
		return nil, err
	}
	for id, r := range res {
		out[id] = share.EffectiveCaps(r).View
	}
	return out, nil
}

// Excerpt is the quote a notification carries: text with its whitespace
// collapsed, cut at a word-ish boundary to a short line.
func Excerpt(text string) string {
	text = strings.Join(strings.FieldsFunc(text, unicode.IsSpace), " ")
	if utf8.RuneCountInString(text) <= excerptRunes {
		return text
	}
	r := []rune(text)[:excerptRunes-1]
	return strings.TrimRightFunc(string(r), unicode.IsSpace) + "…"
}

// A cursor is the (created_at, id) of the last row of a page, opaque to the
// client.
func encodeCursor(at time.Time, id pgtype.UUID) string {
	raw := strconv.FormatInt(at.UnixMicro(), 10) + "_" + uuidString(id)
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func decodeCursor(c string) (time.Time, pgtype.UUID, error) {
	raw, err := base64.RawURLEncoding.DecodeString(c)
	if err != nil {
		return time.Time{}, pgtype.UUID{}, ErrBadCursor
	}
	micros, rest, ok := strings.Cut(string(raw), "_")
	if !ok {
		return time.Time{}, pgtype.UUID{}, ErrBadCursor
	}
	us, err := strconv.ParseInt(micros, 10, 64)
	if err != nil {
		return time.Time{}, pgtype.UUID{}, ErrBadCursor
	}
	var id pgtype.UUID
	if err := id.Scan(rest); err != nil {
		return time.Time{}, pgtype.UUID{}, ErrBadCursor
	}
	return time.UnixMicro(us), id, nil
}

func uuidString(id pgtype.UUID) string {
	v, _ := id.Value()
	s, _ := v.(string)
	return s
}

// isNoRows is pgx's "nothing matched".
func isNoRows(err error) bool { return errors.Is(err, pgx.ErrNoRows) }
