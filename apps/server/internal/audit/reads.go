package audit

import (
	"maps"
	"net/http"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

// ReadDedupWindow is how long one (kind, actor, node) read stays a single
// row. A viewer that fetches a PDF in Range requests, or a person who
// downloads the same file twice in a row, is one read for the purposes of
// "who looked at this".
const ReadDedupWindow = time.Hour

// readDedupMax bounds the cache. Past it the cache forgets rather than grows:
// the failure mode is an extra row, never a missing one.
const readDedupMax = 100_000

type readCache struct {
	mu   sync.Mutex
	seen map[string]time.Time
	max  int
}

func newReadCache(max int) *readCache {
	return &readCache{seen: make(map[string]time.Time), max: max}
}

// first reports whether key has not been seen within ReadDedupWindow of now,
// and marks it seen if so.
func (c *readCache) first(key string, now time.Time) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if at, ok := c.seen[key]; ok && now.Sub(at) < ReadDedupWindow {
		return false
	}
	if len(c.seen) >= c.max {
		for k, at := range c.seen {
			if now.Sub(at) >= ReadDedupWindow {
				delete(c.seen, k)
			}
		}
		if len(c.seen) >= c.max {
			c.seen = make(map[string]time.Time)
		}
	}
	c.seen[key] = now
	return true
}

// Read is one intentional read of a node: by a signed-in user, or through a
// share link, in which case User is nil and ShareID/SharedBy say which link.
type Read struct {
	Kind     Kind
	User     *store.User
	ShareID  pgtype.UUID
	SharedBy pgtype.UUID
	Node     store.Node
	// Detail holds extras merged into the recorded detail, e.g. {"mode": "edit"}.
	Detail map[string]any
}

// RecordRead writes a read event unless the same actor already read the same
// node the same way within ReadDedupWindow. A link visitor is told apart by
// link and address, so visitors behind one NAT share a row per window.
func (s *Service) RecordRead(r *http.Request, rd Read) {
	if s == nil || s.q == nil {
		return
	}
	var actor string
	if rd.User != nil {
		actor = rd.User.ID.String()
		// An agent's read is its own row, not folded into the person's:
		// "read by them" and "read by their agent" are different answers.
		if client, ok := Agent(r.Context()); ok {
			actor += "|agent:" + client
		}
	} else {
		actor = "link:" + rd.ShareID.String() + "@" + ClientIP(r)
	}
	if !s.reads.first(string(rd.Kind)+"|"+actor+"|"+rd.Node.ID.String(), time.Now()) {
		return
	}
	detail := map[string]any{"name": rd.Node.Name, "space": rd.Node.SpaceID.String()}
	e := Event{Kind: rd.Kind, NodeID: rd.Node.ID, Detail: detail}
	if rd.User != nil {
		e.UserID = rd.User.ID
		e.Subject = rd.User.Email
	} else {
		e.Subject = LinkSubject
		detail["share"] = rd.ShareID.String()
		detail["shared_by"] = rd.SharedBy.String()
	}
	maps.Copy(detail, rd.Detail)
	s.Record(r, e)
}
