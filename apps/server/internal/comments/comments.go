// Package comments is M29's threads on block documents. A thread belongs to a
// document; the markdown anchors it with the thread id in a comment marker
// around the annotated text (internal/mdoc), so it survives room reloads and
// version restores, and its rows cascade when the document is purged.
//
// Who may do what is decided here from the caller's capabilities on the
// document, which the edge resolves (httpapi): reading and writing take
// download — whoever can open the document can take part in its discussion —
// editing a comment is its author's alone, deleting one is its author's or an
// editor's, and resolving or reopening a thread is its opener's or an editor's.
package comments

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nhatbui/pp-hub/apps/server/internal/notifications"
	"github.com/nhatbui/pp-hub/apps/server/internal/share"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

const (
	// MaxBodyRunes bounds one comment.
	MaxBodyRunes = 10000
	// MaxMentions bounds the people one comment may name.
	MaxMentions = 50
)

var (
	ErrNotFound  = errors.New("comment not found")
	ErrForbidden = errors.New("not allowed on this comment")
	// ErrEmptyBody, ErrBodyTooLong and ErrTooManyMentions refuse input.
	ErrEmptyBody       = errors.New("comment body is empty")
	ErrBodyTooLong     = errors.New("comment body is too long")
	ErrTooManyMentions = errors.New("too many mentions")
)

// Notifier writes and delivers the notifications a comment causes
// (notifications.Service).
type Notifier interface {
	Readers(ctx context.Context, node store.Node, candidates []pgtype.UUID) ([]pgtype.UUID, error)
	Write(ctx context.Context, q *store.Queries, actor, node pgtype.UUID, notes []notifications.New) ([]pgtype.UUID, error)
	Deliver(ctx context.Context, ids []pgtype.UUID)
}

// Thread is one discussion on a document, oldest comment first.
type Thread struct {
	ID     pgtype.UUID
	NodeID pgtype.UUID
	// Subject is the part of an extension node the thread is about; empty on a document.
	Subject    string
	CreatedAt  time.Time
	CreatedBy  store.User
	ResolvedAt pgtype.Timestamptz
	// ResolvedBy is set exactly when ResolvedAt is.
	ResolvedBy *store.User
	Comments   []Comment
}

// Comment is one message in a thread.
type Comment struct {
	ID        pgtype.UUID
	ThreadID  pgtype.UUID
	Author    store.User
	Body      string
	CreatedAt time.Time
	EditedAt  pgtype.Timestamptz
}

// Input is a new comment: its text and the people it names.
type Input struct {
	Body     string
	Mentions []pgtype.UUID
	// Subject names the part of an extension node a new thread is about
	// ("card:<uuid>"); empty for a document, whose text anchors its threads.
	Subject string
}

type Service struct {
	q      *store.Queries
	notify Notifier
}

func NewService(q *store.Queries, notify Notifier) *Service {
	return &Service{q: q, notify: notify}
}

// ThreadNode returns the document a thread belongs to.
func (s *Service) ThreadNode(ctx context.Context, threadID pgtype.UUID) (pgtype.UUID, error) {
	t, err := s.q.GetCommentThread(ctx, threadID)
	if errors.Is(err, pgx.ErrNoRows) {
		return pgtype.UUID{}, ErrNotFound
	}
	return t.NodeID, err
}

// CommentNode returns the document a comment belongs to.
func (s *Service) CommentNode(ctx context.Context, commentID pgtype.UUID) (pgtype.UUID, error) {
	c, err := s.q.GetCommentWithNode(ctx, commentID)
	if errors.Is(err, pgx.ErrNoRows) {
		return pgtype.UUID{}, ErrNotFound
	}
	return c.NodeID, err
}

// Threads lists a node's threads, oldest first. A subject limits them to the
// threads about that part of an extension node ("" lists every thread).
func (s *Service) Threads(ctx context.Context, nodeID pgtype.UUID, subject string) ([]Thread, error) {
	sub := pgtype.Text{String: subject, Valid: subject != ""}
	threads, err := s.q.ListCommentThreads(ctx, store.ListCommentThreadsParams{NodeID: nodeID, Subject: sub})
	if err != nil {
		return nil, err
	}
	rows, err := s.q.ListNodeComments(ctx, store.ListNodeCommentsParams{NodeID: nodeID, Subject: sub})
	if err != nil {
		return nil, err
	}
	byThread := make(map[pgtype.UUID][]Comment, len(threads))
	for _, r := range rows {
		byThread[r.Comment.ThreadID] = append(byThread[r.Comment.ThreadID], toComment(r.Comment, r.User))
	}
	return s.assemble(ctx, threads, byThread)
}

// CreateThread opens a thread on node with its first comment, and tells the
// document's owner and the people it names.
func (s *Service) CreateThread(ctx context.Context, actor store.User, node store.Node, in Input) (Thread, error) {
	body, err := validate(in)
	if err != nil {
		return Thread{}, err
	}
	plan, err := s.recipients(ctx, actor.ID, node, notifications.KindComment, nil, in.Mentions)
	if err != nil {
		return Thread{}, err
	}
	var thread store.CommentThread
	var ids []pgtype.UUID
	err = s.q.InTx(ctx, func(q *store.Queries) error {
		var err error
		thread, err = q.CreateCommentThread(ctx, store.CreateCommentThreadParams{
			NodeID: node.ID, CreatedBy: actor.ID, Subject: pgtype.Text{String: in.Subject, Valid: in.Subject != ""},
		})
		if err != nil {
			return err
		}
		c, err := q.CreateComment(ctx, store.CreateCommentParams{ThreadID: thread.ID, AuthorID: actor.ID, Body: body})
		if err != nil {
			return err
		}
		ids, err = s.notify.Write(ctx, q, actor.ID, node.ID, plan.notes(thread.ID, c.ID, body))
		return err
	})
	if err != nil {
		return Thread{}, err
	}
	s.notify.Deliver(ctx, ids)
	return s.thread(ctx, thread.ID)
}

// Reply adds a comment to a thread on node, reopening it if it was resolved,
// and tells the thread's participants, the document's owner and the people it
// names.
func (s *Service) Reply(ctx context.Context, actor store.User, node store.Node, threadID pgtype.UUID, in Input) (Thread, error) {
	body, err := validate(in)
	if err != nil {
		return Thread{}, err
	}
	participants, err := s.q.ListThreadParticipants(ctx, threadID)
	if err != nil {
		return Thread{}, err
	}
	plan, err := s.recipients(ctx, actor.ID, node, notifications.KindReply, participants, in.Mentions)
	if err != nil {
		return Thread{}, err
	}
	var ids []pgtype.UUID
	err = s.q.InTx(ctx, func(q *store.Queries) error {
		t, err := s.lock(ctx, q, threadID)
		if err != nil {
			return err
		}
		if t.NodeID != node.ID {
			return ErrNotFound
		}
		if t.ResolvedAt.Valid {
			if _, err := q.ReopenCommentThread(ctx, threadID); err != nil {
				return err
			}
		}
		c, err := q.CreateComment(ctx, store.CreateCommentParams{ThreadID: threadID, AuthorID: actor.ID, Body: body})
		if err != nil {
			return err
		}
		ids, err = s.notify.Write(ctx, q, actor.ID, node.ID, plan.notes(threadID, c.ID, body))
		return err
	})
	if err != nil {
		return Thread{}, err
	}
	s.notify.Deliver(ctx, ids)
	return s.thread(ctx, threadID)
}

// Edit replaces the body of actor's own comment.
func (s *Service) Edit(ctx context.Context, actor store.User, commentID pgtype.UUID, body string) (Comment, error) {
	body, err := validate(Input{Body: body})
	if err != nil {
		return Comment{}, err
	}
	var out store.Comment
	err = s.q.InTx(ctx, func(q *store.Queries) error {
		cur, err := q.GetCommentWithNode(ctx, commentID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if cur.Comment.AuthorID != actor.ID {
			return ErrForbidden
		}
		if _, err := s.lock(ctx, q, cur.Comment.ThreadID); err != nil {
			return err
		}
		out, err = q.UpdateCommentBody(ctx, store.UpdateCommentBodyParams{ID: commentID, Body: body})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return err
	})
	if err != nil {
		return Comment{}, err
	}
	return toComment(out, actor), nil
}

// Delete removes a comment — its author's right, or an editor's of the
// document (caps are actor's on it) — and the thread with its last comment.
func (s *Service) Delete(ctx context.Context, actor store.User, caps share.Caps, commentID pgtype.UUID) error {
	return s.q.InTx(ctx, func(q *store.Queries) error {
		cur, err := q.GetCommentWithNode(ctx, commentID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if cur.Comment.AuthorID != actor.ID && !caps.Edit {
			return ErrForbidden
		}
		if _, err := s.lock(ctx, q, cur.Comment.ThreadID); err != nil {
			return err
		}
		n, err := q.DeleteComment(ctx, commentID)
		if err != nil {
			return err
		}
		if n == 0 {
			return ErrNotFound
		}
		_, err = q.DeleteEmptyCommentThread(ctx, cur.Comment.ThreadID)
		return err
	})
}

// SetResolved resolves or reopens a thread — its opener's right, or an
// editor's of the document (caps are actor's on it). Both are idempotent:
// resolving a resolved thread keeps who resolved it and when.
func (s *Service) SetResolved(ctx context.Context, actor store.User, caps share.Caps, threadID pgtype.UUID, resolved bool) (Thread, error) {
	err := s.q.InTx(ctx, func(q *store.Queries) error {
		t, err := s.lock(ctx, q, threadID)
		if err != nil {
			return err
		}
		if t.CreatedBy != actor.ID && !caps.Edit {
			return ErrForbidden
		}
		if resolved {
			_, err = q.ResolveCommentThread(ctx, store.ResolveCommentThreadParams{ID: threadID, UserID: actor.ID})
		} else {
			_, err = q.ReopenCommentThread(ctx, threadID)
		}
		return err
	})
	if err != nil {
		return Thread{}, err
	}
	return s.thread(ctx, threadID)
}

func (s *Service) lock(ctx context.Context, q *store.Queries, threadID pgtype.UUID) (store.CommentThread, error) {
	t, err := q.LockCommentThread(ctx, threadID)
	if errors.Is(err, pgx.ErrNoRows) {
		return t, ErrNotFound
	}
	return t, err
}

// thread loads one thread with its comments.
func (s *Service) thread(ctx context.Context, threadID pgtype.UUID) (Thread, error) {
	t, err := s.q.GetCommentThread(ctx, threadID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Thread{}, ErrNotFound
	}
	if err != nil {
		return Thread{}, err
	}
	rows, err := s.q.ListThreadComments(ctx, threadID)
	if err != nil {
		return Thread{}, err
	}
	comments := make([]Comment, 0, len(rows))
	for _, r := range rows {
		comments = append(comments, toComment(r.Comment, r.User))
	}
	out, err := s.assemble(ctx, []store.CommentThread{t}, map[pgtype.UUID][]Comment{t.ID: comments})
	if err != nil {
		return Thread{}, err
	}
	return out[0], nil
}

// assemble joins threads to their comments and to the people who opened and
// resolved them.
func (s *Service) assemble(ctx context.Context, threads []store.CommentThread, comments map[pgtype.UUID][]Comment) ([]Thread, error) {
	ids := make([]pgtype.UUID, 0, 2*len(threads))
	for _, t := range threads {
		ids = append(ids, t.CreatedBy)
		if t.ResolvedBy.Valid {
			ids = append(ids, t.ResolvedBy)
		}
	}
	users := map[pgtype.UUID]store.User{}
	if len(ids) > 0 {
		rows, err := s.q.ListUsersByIDs(ctx, ids)
		if err != nil {
			return nil, err
		}
		for _, u := range rows {
			users[u.ID] = u
		}
	}
	out := make([]Thread, 0, len(threads))
	for _, t := range threads {
		th := Thread{
			ID:         t.ID,
			NodeID:     t.NodeID,
			Subject:    t.Subject.String,
			CreatedAt:  t.CreatedAt.Time,
			CreatedBy:  users[t.CreatedBy],
			ResolvedAt: t.ResolvedAt,
			Comments:   comments[t.ID],
		}
		if t.ResolvedBy.Valid {
			u := users[t.ResolvedBy]
			th.ResolvedBy = &u
		}
		out = append(out, th)
	}
	return out, nil
}

// plan is who a new comment notifies, and as what.
type plan struct {
	kind      notifications.Kind
	mentioned []pgtype.UUID
	others    []pgtype.UUID
}

func (p plan) notes(threadID, commentID pgtype.UUID, body string) []notifications.New {
	excerpt := notifications.Excerpt(body)
	out := make([]notifications.New, 0, len(p.mentioned)+len(p.others))
	for _, id := range p.mentioned {
		out = append(out, notifications.New{UserID: id, Kind: notifications.KindMention, ThreadID: threadID, CommentID: commentID, Excerpt: excerpt})
	}
	for _, id := range p.others {
		out = append(out, notifications.New{UserID: id, Kind: p.kind, ThreadID: threadID, CommentID: commentID, Excerpt: excerpt})
	}
	return out
}

// recipients decides who hears about a comment: the people it names as a
// mention, then the thread's participants and the document's owner as kind —
// each person once, a mention winning, never the actor, and nobody who cannot
// open the document.
func (s *Service) recipients(ctx context.Context, actor pgtype.UUID, node store.Node, kind notifications.Kind, participants, mentions []pgtype.UUID) (plan, error) {
	candidates := make([]pgtype.UUID, 0, len(mentions)+len(participants)+1)
	candidates = append(candidates, mentions...)
	candidates = append(candidates, participants...)
	candidates = append(candidates, node.CreatedBy)
	readers, err := s.notify.Readers(ctx, node, candidates)
	if err != nil {
		return plan{}, err
	}
	allowed := make(map[pgtype.UUID]bool, len(readers))
	for _, id := range readers {
		allowed[id] = id != actor
	}
	p := plan{kind: kind}
	for _, id := range mentions {
		if allowed[id] {
			p.mentioned = append(p.mentioned, id)
			allowed[id] = false
		}
	}
	for _, id := range append(slices.Clip(participants), node.CreatedBy) {
		if allowed[id] {
			p.others = append(p.others, id)
			allowed[id] = false
		}
	}
	return p, nil
}

func validate(in Input) (string, error) {
	// Postgres text cannot hold NUL, and nobody means to write one.
	body := strings.TrimSpace(strings.ReplaceAll(in.Body, "\x00", ""))
	if body == "" {
		return "", ErrEmptyBody
	}
	if utf8.RuneCountInString(body) > MaxBodyRunes {
		return "", ErrBodyTooLong
	}
	if len(in.Mentions) > MaxMentions {
		return "", ErrTooManyMentions
	}
	return body, nil
}

func toComment(c store.Comment, author store.User) Comment {
	return Comment{
		ID:        c.ID,
		ThreadID:  c.ThreadID,
		Author:    author,
		Body:      c.Body,
		CreatedAt: c.CreatedAt.Time,
		EditedAt:  c.EditedAt,
	}
}
