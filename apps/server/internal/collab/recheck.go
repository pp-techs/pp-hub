package collab

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nhatbui/pp-hub/apps/server/internal/auth"
	"github.com/nhatbui/pp-hub/apps/server/internal/fs"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

// strandedGrace is how long past the idle timeout an empty room may stay
// resident before Recheck closes it itself. ygo's sweeper evicts an idle room
// within one and a half idle timeouts; a room still here well after that is
// one it lost track of — touched by Apply after its last peer left (ygo then
// stops counting it idle), or loaded by LoadDoc and then refused by ygo.
const strandedGrace = 2 * time.Minute

// Recheck is the periodic pass over resident rooms (M21's "access is
// re-resolved"): each peer's session and capabilities are resolved again, and
// a socket whose access changed is closed — a revoked peer is refused on the
// reconnect its provider makes, a downgraded or upgraded one comes back with
// the new mode. A document trashed, purged or no longer markdown closes every
// socket. It also refreshes the rooms' file locks, drops epochs handed out
// and never joined, and closes stranded rooms.
func (h *Hub) Recheck(ctx context.Context) {
	now := time.Now()
	type resident struct {
		rm         *room
		conns      []*conn
		emptySince time.Time
		token      string
	}
	var rooms []resident
	h.mu.Lock()
	for _, rm := range h.byName {
		if !rm.loaded {
			if !h.pendingLive(rm, now) {
				h.forgetLocked(rm)
			}
			continue
		}
		res := resident{rm: rm, emptySince: rm.emptySince, token: rm.lockToken}
		for c := range rm.conns {
			res.conns = append(res.conns, c)
		}
		rooms = append(rooms, res)
	}
	h.mu.Unlock()

	for _, res := range rooms {
		rm := res.rm
		if res.token != "" {
			if n, err := h.store.AcquireLock(ctx, store.AcquireLockParams{NodeID: rm.nodeID, LockToken: res.token}); err != nil {
				h.log.WarnContext(ctx, "refresh room lock", "room", rm.name, "err", err)
			} else if n == 0 {
				h.log.WarnContext(ctx, "room lock taken by another holder", "room", rm.name)
			}
		}
		if len(res.conns) == 0 {
			if !res.emptySince.IsZero() && now.Sub(res.emptySince) > h.cfg.IdleTimeout+strandedGrace && !rm.failing.Load() {
				h.closed(ctx, rm, h.srv.CloseRoom(rm.name, false))
			}
			continue
		}
		node, live, err := h.liveDocument(ctx, rm.nodeID)
		if err != nil {
			h.log.WarnContext(ctx, "recheck room", "room", rm.name, "err", err)
			continue
		}
		for _, c := range res.conns {
			if !live || !h.stillAdmitted(ctx, node, c) {
				c.cancel()
			}
		}
	}
}

// liveDocument reports whether a room's document can still be edited here: a
// live file whose current version is markdown. An error is a failure to find
// out, not an answer.
func (h *Hub) liveDocument(ctx context.Context, nodeID pgtype.UUID) (store.Node, bool, error) {
	node, err := h.files.Get(ctx, nodeID)
	if errors.Is(err, fs.ErrNotFound) {
		return store.Node{}, false, nil
	}
	if err != nil {
		return store.Node{}, false, err
	}
	ver, err := h.store.GetCurrentVersion(ctx, nodeID)
	if errors.Is(err, pgx.ErrNoRows) {
		return store.Node{}, false, nil
	}
	if err != nil {
		return store.Node{}, false, err
	}
	return node, node.Kind == store.NodeKindFile && fs.IsMarkdown(ver.Mime), nil
}

// stillAdmitted reports whether authorize would admit c's peer the same way
// now. A lookup that fails keeps the socket: a database hiccup is not a
// revocation, and the next pass asks again.
func (h *Hub) stillAdmitted(ctx context.Context, node store.Node, c *conn) bool {
	active, err := h.store.SessionActive(ctx, store.SessionActiveParams{
		ID:           c.peer.Session,
		CreatedAfter: pgtype.Timestamptz{Time: time.Now().Add(-auth.SessionAbsoluteTTL), Valid: true},
	})
	if err != nil {
		h.log.WarnContext(ctx, "recheck session", "err", err)
		return true
	}
	if !active {
		return false
	}
	caps, err := h.access.EffectiveCapsForNode(ctx, node, c.peer.User)
	if err != nil {
		h.log.WarnContext(ctx, "recheck access", "node", node.ID, "err", err)
		return true
	}
	return caps.Download && c.readOnly == !caps.Edit
}
