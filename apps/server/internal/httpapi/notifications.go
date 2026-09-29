package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nhatbui/pp-hub/apps/server/internal/auth"
	"github.com/nhatbui/pp-hub/apps/server/internal/notifications"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

// In-app notifications (M29): the list, the unread count, marking read, and a
// Server-Sent Events stream that pushes each new notification to its
// recipient's open tabs. See internal/notifications.

// The stream's timings. Variables so a test can shorten them.
var (
	// streamHeartbeat keeps proxies from timing an idle stream out (nginx
	// reads for an hour on this location) and lets the server notice a
	// client that went away.
	streamHeartbeat = 25 * time.Second
	// streamRecheck is how often a long stream asks again whether its
	// session is still live, as collab's recheck does for sockets: a
	// revoked, expired or suspended session's stream ends, and the
	// client's reconnect is refused with 401 or 403.
	streamRecheck = 60 * time.Second
)

// maxMarkReadBytes bounds POST /notifications/read.
const maxMarkReadBytes = 64 << 10

type notificationNodeDTO struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	SpaceID string `json:"space_id"`
}

type notificationDTO struct {
	ID        string              `json:"id"`
	Kind      string              `json:"kind"`
	Actor     personDTO           `json:"actor"`
	Node      notificationNodeDTO `json:"node"`
	ThreadID  string              `json:"thread_id,omitempty"`
	CommentID string              `json:"comment_id,omitempty"`
	Excerpt   string              `json:"excerpt"`
	CreatedAt string              `json:"created_at"`
	ReadAt    string              `json:"read_at,omitempty"`
}

func notificationToDTO(it notifications.Item) notificationDTO {
	d := notificationDTO{
		ID:        uuidString(it.ID),
		Kind:      string(it.Kind),
		Actor:     personOf(it.Actor),
		Node:      notificationNodeDTO{ID: uuidString(it.NodeID), Name: it.NodeName, SpaceID: uuidString(it.SpaceID)},
		ThreadID:  uuidString(it.ThreadID),
		CommentID: uuidString(it.CommentID),
		Excerpt:   it.Excerpt,
		CreatedAt: it.CreatedAt.Format(time.RFC3339),
	}
	if it.ReadAt.Valid {
		d.ReadAt = it.ReadAt.Time.Format(time.RFC3339)
	}
	return d
}

// handleListNotifications is GET /notifications?cursor=&limit=.
func (s *Server) handleListNotifications(w http.ResponseWriter, r *http.Request) {
	u := caller(r)
	limit := 0
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			writeProblem(w, http.StatusBadRequest, "Bad Request", "invalid limit")
			return
		}
		limit = n
	}
	items, next, err := s.notifications.List(r.Context(), u.ID, r.URL.Query().Get("cursor"), limit)
	if errors.Is(err, notifications.ErrBadCursor) {
		writeProblem(w, http.StatusBadRequest, "Bad Request", err.Error())
		return
	}
	if err != nil {
		Error(w, r, err)
		return
	}
	out := struct {
		Items      []notificationDTO `json:"items"`
		NextCursor string            `json:"next_cursor,omitempty"`
	}{Items: make([]notificationDTO, 0, len(items)), NextCursor: next}
	for _, it := range items {
		out.Items = append(out.Items, notificationToDTO(it))
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, out)
}

// handleUnreadNotifications is GET /notifications/unread-count.
func (s *Server) handleUnreadNotifications(w http.ResponseWriter, r *http.Request) {
	n, err := s.notifications.UnreadCount(r.Context(), caller(r).ID)
	if err != nil {
		Error(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]int64{"count": n})
}

// handleMarkNotificationsRead is POST /notifications/read {"ids"?, "all"?}.
// It answers the unread count left, which is what the badge shows next.
func (s *Server) handleMarkNotificationsRead(w http.ResponseWriter, r *http.Request) {
	u := caller(r)
	if !limitBody(w, r, maxMarkReadBytes) {
		return
	}
	var body struct {
		IDs []string `json:"ids"`
		All bool     `json:"all"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "invalid JSON body")
		return
	}
	if !body.All && len(body.IDs) == 0 {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "name the notifications to mark, or all")
		return
	}
	ids := make([]pgtype.UUID, 0, len(body.IDs))
	for _, raw := range body.IDs {
		var id pgtype.UUID
		if err := id.Scan(raw); err != nil {
			writeProblem(w, http.StatusBadRequest, "Bad Request", "invalid notification id")
			return
		}
		ids = append(ids, id)
	}
	if _, err := s.notifications.MarkRead(r.Context(), u.ID, ids, body.All); err != nil {
		Error(w, r, err)
		return
	}
	n, err := s.notifications.UnreadCount(r.Context(), u.ID)
	if err != nil {
		Error(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"count": n})
}

// handleNotificationStream is GET /notifications/stream: Server-Sent Events,
// one `notification` event per new notification (data: the list item's
// JSON), and a comment line every streamHeartbeat. The session middleware
// authenticated it once; streamRecheck asks again while it lives. Streams are
// this process's (notifications.Broker): one server instance.
func (s *Server) handleNotificationStream(w http.ResponseWriter, r *http.Request) {
	sess := callerSession(r)
	sub, err := s.notifications.Subscribe(sess.User.ID)
	if errors.Is(err, notifications.ErrTooManyStreams) {
		writeProblem(w, http.StatusTooManyRequests, "Too Many Requests", err.Error())
		return
	}
	if errors.Is(err, notifications.ErrShuttingDown) {
		writeProblem(w, http.StatusServiceUnavailable, "Service Unavailable", err.Error())
		return
	}
	if err != nil {
		Error(w, r, err)
		return
	}
	defer sub.Close()

	rc := http.NewResponseController(w)
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-store")
	// nginx's location for this path already turns buffering off; this
	// covers any other proxy that honours it.
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	// The first bytes tell the client the stream is open, and how long to
	// wait before reconnecting.
	if _, err := fmt.Fprint(w, "retry: 5000\n\n"); err != nil {
		return
	}
	if err := rc.Flush(); err != nil {
		return
	}

	heartbeat := time.NewTicker(streamHeartbeat)
	defer heartbeat.Stop()
	recheck := time.NewTicker(streamRecheck)
	defer recheck.Stop()
	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case it, ok := <-sub.C:
			if !ok {
				// Dropped for falling behind, or the server is shutting
				// down: the client reconnects and refetches the list.
				return
			}
			data, err := json.Marshal(notificationToDTO(it))
			if err != nil {
				slog.ErrorContext(ctx, "encode notification", "err", err)
				return
			}
			if _, err := fmt.Fprintf(w, "id: %s\nevent: notification\ndata: %s\n\n", uuidString(it.ID), data); err != nil {
				return
			}
		case <-heartbeat.C:
			if _, err := fmt.Fprint(w, ": heartbeat\n\n"); err != nil {
				return
			}
		case <-recheck.C:
			if !s.streamSessionActive(r, sess) {
				return
			}
			continue
		}
		if err := rc.Flush(); err != nil {
			return
		}
	}
}

// streamSessionActive reports whether a stream's session may keep it open. A
// lookup that fails keeps the stream: a database hiccup is not a revocation.
func (s *Server) streamSessionActive(r *http.Request, sess *auth.Session) bool {
	active, err := s.store.SessionActive(r.Context(), store.SessionActiveParams{
		ID:           sess.ID,
		CreatedAfter: pgtype.Timestamptz{Time: time.Now().Add(-auth.SessionAbsoluteTTL), Valid: true},
	})
	if err != nil {
		slog.WarnContext(r.Context(), "recheck notification stream", "err", err)
		return true
	}
	return active
}
