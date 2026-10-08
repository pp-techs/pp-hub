package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nhatbui/pp-hub/apps/server/internal/comments"
	"github.com/nhatbui/pp-hub/apps/server/internal/share"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

// Comments (M29) on block documents. Every route takes download on the
// document — whoever can open it can read and join its discussion — and the
// comments module applies the per-comment rules on top (author-only edit;
// delete by the author or an editor; resolve and reopen by the thread's opener
// or an editor). See internal/comments.

// maxCommentRequestBytes bounds a comment request: MaxBodyRunes of four-byte
// runes, the mentions, and the JSON around them.
const maxCommentRequestBytes = 128 << 10

// personDTO is a user as comments and notifications show them.
type personDTO struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	AvatarURL string `json:"avatar_url"`
}

func personOf(u store.User) personDTO {
	return personDTO{ID: uuidString(u.ID), Name: displayName(u), AvatarURL: u.AvatarUrl}
}

type commentDTO struct {
	ID        string    `json:"id"`
	Author    personDTO `json:"author"`
	Body      string    `json:"body"`
	CreatedAt string    `json:"created_at"`
	EditedAt  string    `json:"edited_at,omitempty"`
}

type threadDTO struct {
	ID         string       `json:"id"`
	Subject    string       `json:"subject,omitempty"`
	Resolved   bool         `json:"resolved"`
	ResolvedAt string       `json:"resolved_at,omitempty"`
	ResolvedBy *personDTO   `json:"resolved_by,omitempty"`
	CreatedAt  string       `json:"created_at"`
	CreatedBy  personDTO    `json:"created_by"`
	Comments   []commentDTO `json:"comments"`
}

func commentToDTO(c comments.Comment) commentDTO {
	d := commentDTO{
		ID:        uuidString(c.ID),
		Author:    personOf(c.Author),
		Body:      c.Body,
		CreatedAt: c.CreatedAt.Format(time.RFC3339),
	}
	if c.EditedAt.Valid {
		d.EditedAt = c.EditedAt.Time.Format(time.RFC3339)
	}
	return d
}

func threadToDTO(t comments.Thread) threadDTO {
	d := threadDTO{
		ID:        uuidString(t.ID),
		Subject:   t.Subject,
		Resolved:  t.ResolvedAt.Valid,
		CreatedAt: t.CreatedAt.Format(time.RFC3339),
		CreatedBy: personOf(t.CreatedBy),
		Comments:  make([]commentDTO, 0, len(t.Comments)),
	}
	if t.ResolvedAt.Valid {
		d.ResolvedAt = t.ResolvedAt.Time.Format(time.RFC3339)
	}
	if t.ResolvedBy != nil {
		p := personOf(*t.ResolvedBy)
		d.ResolvedBy = &p
	}
	for _, c := range t.Comments {
		d.Comments = append(d.Comments, commentToDTO(c))
	}
	return d
}

// commentInput is the body of a new thread or reply. Mentions are user ids.
type commentInput struct {
	Body     string   `json:"body"`
	Mentions []string `json:"mentions"`
	// Subject is what an extension node's thread is about ("card:<uuid>").
	Subject string `json:"subject"`
}

// readCommentInput decodes a new comment, answering 400 for a malformed one.
func readCommentInput(w http.ResponseWriter, r *http.Request) (comments.Input, bool) {
	if !limitBody(w, r, maxCommentRequestBytes) {
		return comments.Input{}, false
	}
	var body commentInput
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "invalid JSON body")
		return comments.Input{}, false
	}
	in := comments.Input{Body: body.Body, Subject: body.Subject, Mentions: make([]pgtype.UUID, 0, len(body.Mentions))}
	for _, m := range body.Mentions {
		var id pgtype.UUID
		if err := id.Scan(m); err != nil {
			writeProblem(w, http.StatusBadRequest, "Bad Request", "invalid mention: "+m)
			return comments.Input{}, false
		}
		in.Mentions = append(in.Mentions, id)
	}
	return in, true
}

func (s *Server) commentError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, comments.ErrNotFound):
		writeProblem(w, http.StatusNotFound, "Not Found", err.Error())
	case errors.Is(err, comments.ErrForbidden):
		writeProblem(w, http.StatusForbidden, "Forbidden", err.Error())
	case errors.Is(err, comments.ErrEmptyBody), errors.Is(err, comments.ErrBodyTooLong),
		errors.Is(err, comments.ErrTooManyMentions):
		writeProblem(w, http.StatusBadRequest, "Bad Request", err.Error())
	default:
		s.nodeError(w, r, err)
	}
}

// requireDocumentComments resolves the node in the URL for a comments route:
// view on a markdown document, or on an extension node (M46), whose threads
// are about its parts. Whoever can read a node can discuss it, and reading is
// view.
func (s *Server) requireDocumentComments(w http.ResponseWriter, r *http.Request) (*store.User, store.Node) {
	u, node, _ := s.requireNodeCaps(w, r, share.CapView)
	if u == nil {
		return nil, store.Node{}
	}
	if node.Kind == store.NodeKindExt {
		return u, node
	}
	if !s.isMarkdownFile(w, r, node) {
		return nil, store.Node{}
	}
	return u, node
}

// commentSubject checks the subject of a thread about to be opened. A document
// has none; an extension node's must name a part of it that its extension
// accepts, so a thread cannot be hung on a card that is not there.
func (s *Server) commentSubject(w http.ResponseWriter, r *http.Request, node store.Node, subject string) bool {
	if node.Kind != store.NodeKindExt {
		if subject != "" {
			writeProblem(w, http.StatusBadRequest, "Bad Request", "a document's threads have no subject")
			return false
		}
		return true
	}
	if subject == "" || s.ext == nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "subject is required")
		return false
	}
	ok, err := s.ext.ValidSubject(r.Context(), node, subject)
	if err != nil {
		Error(w, r, err)
		return false
	}
	if !ok {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "subject is not part of this node")
		return false
	}
	return true
}

// requireThread resolves the thread in the URL and the caller's capabilities
// on its document, which must include view.
func (s *Server) requireThread(w http.ResponseWriter, r *http.Request) (*store.User, store.Node, share.Caps, pgtype.UUID) {
	threadID, err := uuidParam(r, "threadID")
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "invalid thread id")
		return nil, store.Node{}, share.Caps{}, pgtype.UUID{}
	}
	nodeID, err := s.comments.ThreadNode(r.Context(), threadID)
	if err != nil {
		s.commentError(w, r, err)
		return nil, store.Node{}, share.Caps{}, pgtype.UUID{}
	}
	u, node, caps := s.requireNodeCapsByID(w, r, nodeID, share.CapView)
	return u, node, caps, threadID
}

// requireComment is requireThread for a comment in the URL.
func (s *Server) requireComment(w http.ResponseWriter, r *http.Request) (*store.User, share.Caps, pgtype.UUID) {
	commentID, err := uuidParam(r, "commentID")
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "invalid comment id")
		return nil, share.Caps{}, pgtype.UUID{}
	}
	nodeID, err := s.comments.CommentNode(r.Context(), commentID)
	if err != nil {
		s.commentError(w, r, err)
		return nil, share.Caps{}, pgtype.UUID{}
	}
	u, _, caps := s.requireNodeCapsByID(w, r, nodeID, share.CapView)
	return u, caps, commentID
}

// handleListComments is GET /nodes/{id}/comments.
func (s *Server) handleListComments(w http.ResponseWriter, r *http.Request) {
	u, node := s.requireDocumentComments(w, r)
	if u == nil {
		return
	}
	threads, err := s.comments.Threads(r.Context(), node.ID, r.URL.Query().Get("subject"))
	if err != nil {
		s.commentError(w, r, err)
		return
	}
	out := make([]threadDTO, 0, len(threads))
	for _, t := range threads {
		out = append(out, threadToDTO(t))
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{"threads": out})
}

// handleCreateCommentThread is POST /nodes/{id}/comment-threads: a new
// thread with its first comment. The thread id is the server's; the editor
// anchors the thread with it afterwards.
func (s *Server) handleCreateCommentThread(w http.ResponseWriter, r *http.Request) {
	u, node := s.requireDocumentComments(w, r)
	if u == nil {
		return
	}
	in, ok := readCommentInput(w, r)
	if !ok {
		return
	}
	if !s.commentSubject(w, r, node, in.Subject) {
		return
	}
	t, err := s.comments.CreateThread(r.Context(), *u, node, in)
	if err != nil {
		s.commentError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, threadToDTO(t))
}

// handleReplyComment is POST /comment-threads/{id}/comments. A reply to a
// resolved thread reopens it.
func (s *Server) handleReplyComment(w http.ResponseWriter, r *http.Request) {
	u, node, _, threadID := s.requireThread(w, r)
	if u == nil {
		return
	}
	in, ok := readCommentInput(w, r)
	if !ok {
		return
	}
	t, err := s.comments.Reply(r.Context(), *u, node, threadID, in)
	if err != nil {
		s.commentError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, threadToDTO(t))
}

// handleResolveThread is POST /comment-threads/{id}/resolve.
func (s *Server) handleResolveThread(w http.ResponseWriter, r *http.Request) {
	s.setThreadResolved(w, r, true)
}

// handleReopenThread is POST /comment-threads/{id}/reopen.
func (s *Server) handleReopenThread(w http.ResponseWriter, r *http.Request) {
	s.setThreadResolved(w, r, false)
}

func (s *Server) setThreadResolved(w http.ResponseWriter, r *http.Request, resolved bool) {
	u, _, caps, threadID := s.requireThread(w, r)
	if u == nil {
		return
	}
	t, err := s.comments.SetResolved(r.Context(), *u, caps, threadID, resolved)
	if err != nil {
		s.commentError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, threadToDTO(t))
}

// handleEditComment is PATCH /comments/{id}: its author only.
func (s *Server) handleEditComment(w http.ResponseWriter, r *http.Request) {
	u, _, commentID := s.requireComment(w, r)
	if u == nil {
		return
	}
	if !limitBody(w, r, maxCommentRequestBytes) {
		return
	}
	var body struct {
		Body string `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "invalid JSON body")
		return
	}
	c, err := s.comments.Edit(r.Context(), *u, commentID, body.Body)
	if err != nil {
		s.commentError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, commentToDTO(c))
}

// handleDeleteComment is DELETE /comments/{id}: its author, or an editor of
// the document. The thread goes with its last comment.
func (s *Server) handleDeleteComment(w http.ResponseWriter, r *http.Request) {
	u, caps, commentID := s.requireComment(w, r)
	if u == nil {
		return
	}
	if err := s.comments.Delete(r.Context(), *u, caps, commentID); err != nil {
		s.commentError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
