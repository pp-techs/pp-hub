// Package collab hosts the realtime rooms behind the block editor (M39): one
// reearth/ygo websocket.Server, speaking the Hocuspocus protocol, whose rooms
// are seeded from a markdown file's current version and written back to it as
// ordinary versions. See docs/architecture/block-documents.md.
//
// A room is named `<nodeId>.<epoch>`. The Y.Doc exists only in memory and is
// regenerated from markdown — with fresh client ids — every time a room loads,
// so a client still holding a previous generation must not be allowed to merge
// it into the new one: the epoch is minted when a room is first asked for
// (Open), and a connection naming any other epoch is refused.
//
// Everything here assumes one server instance (the rooms, their epochs and the
// file locks they hold are this process's); several would need ygo's cluster
// relay, which is out of scope as it is for the rate limiter.
package collab

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/reearth/ygo/provider/websocket"

	"github.com/nhatbui/pp-hub/apps/server/internal/fs"
	"github.com/nhatbui/pp-hub/apps/server/internal/mdoc"
	"github.com/nhatbui/pp-hub/apps/server/internal/obs"
	"github.com/nhatbui/pp-hub/apps/server/internal/share"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

// Service is what the rest of the server needs from collaboration: the HTTP
// edge hands out rooms, serves their sockets, and replaces the file under a
// room through it.
type Service interface {
	// Open returns the room for a markdown document the caller has already
	// been authorized to download, minting a new epoch when none is resident
	// or pending.
	Open(ctx context.Context, doc store.Node) (Session, error)
	// ServeSocket runs one collaboration socket for the authenticated peer.
	// The caller has already checked the request's Origin.
	ServeSocket(w http.ResponseWriter, r *http.Request, room string, peer Peer)
	// Reseed runs change, which replaces the document's file, and makes a
	// resident room hold the result, keeping its epoch and its peers.
	Reseed(ctx context.Context, nodeID pgtype.UUID, change func(context.Context) error) error
}

// Session is what a client needs to join a document's room.
type Session struct {
	Room  string
	Lossy bool
}

// Peer is who is behind a socket: the user it acts for and the session that
// authenticated it, both re-checked while the socket lives.
type Peer struct {
	User    pgtype.UUID
	Session pgtype.UUID
}

// Files is the file tree collab reads documents from and writes versions to
// (fs.Service). Its errors are fs's.
type Files interface {
	Get(ctx context.Context, nodeID pgtype.UUID) (store.Node, error)
	OpenFile(ctx context.Context, nodeID pgtype.UUID) (store.Node, store.NodeVersion, io.ReadCloser, error)
	PutFile(ctx context.Context, spaceID, parentID, fileID pgtype.UUID, name string, r io.Reader, creator pgtype.UUID) (store.Node, store.NodeVersion, error)
}

// Access resolves what a user may do with a node (share.Service).
type Access interface {
	EffectiveCapsForNode(ctx context.Context, node store.Node, userID pgtype.UUID) (share.Caps, error)
}

// Store is the handful of queries collab runs itself (store.Queries).
type Store interface {
	GetCurrentVersion(ctx context.Context, id pgtype.UUID) (store.NodeVersion, error)
	AcquireLock(ctx context.Context, arg store.AcquireLockParams) (int64, error)
	DeleteLock(ctx context.Context, arg store.DeleteLockParams) error
	DeleteLocksWithPrefix(ctx context.Context, prefix string) (int64, error)
	SessionActive(ctx context.Context, arg store.SessionActiveParams) (bool, error)
}

var (
	// ErrNotMarkdown refuses a room for a file whose current version is not
	// a block document.
	ErrNotMarkdown = errors.New("this file is not a markdown document")
	// ErrTooLarge refuses a room for a document too big to hold in memory
	// as a live Y.Doc.
	ErrTooLarge = errors.New("this document is too large to open in the editor")

	// errRoomGone refuses to load a room whose epoch is not the one pending:
	// a stale client, or a server-side write racing the room's unload.
	errRoomGone = errors.New("collab: room is not pending")
	// errLocked refuses to load a room over a lock somebody else holds.
	errLocked = errors.New("collab: file is locked by another editor")
)

// lockPrefix marks the file_locks tokens rooms hold, so the edge can tell a
// room's lock (restore is allowed through it) from an office editor's.
const lockPrefix = "collab:"

// IsLockToken reports whether a file_locks token is a collaboration room's.
func IsLockToken(token string) bool { return strings.HasPrefix(token, lockPrefix) }

// maxDocumentBytes bounds the markdown a room will load. A live Y.Doc costs a
// multiple of its markdown, and nobody writes a 16 MiB document by hand.
const maxDocumentBytes = 16 << 20

// Config is the room policy. The zero value of any field takes its default.
type Config struct {
	// Origin is this deployment's origin (scheme://host), the only one ygo
	// admits. The edge has refused every other one, and a missing one,
	// before ygo sees the request; ygo's own fallback — Origin host equal to
	// the Host header — would refuse a proxy that rewrites Host.
	Origin string
	// CoalesceWindow is how long a room must be quiet before its edits are
	// written back (30 s); CoalesceMaxWait caps how long edits under
	// continuous typing wait (5 min). Together they bound what a crash loses.
	CoalesceWindow  time.Duration
	CoalesceMaxWait time.Duration
	// IdleTimeout keeps a room warm after its last peer leaves (2 min), so a
	// reload rejoins the same epoch instead of rebuilding the document.
	IdleTimeout time.Duration
	// PendingTTL is how long an epoch handed out by Open waits for its first
	// connection (2 min).
	PendingTTL time.Duration
	// RecheckInterval is how often every peer's access is resolved again and
	// every room's lock refreshed (60 s).
	RecheckInterval time.Duration
	// PingInterval is how often each socket is pinged (15 s): the provider
	// reconnects after 30 s without a message.
	PingInterval time.Duration
}

func (c Config) withDefaults() Config {
	def := func(d *time.Duration, v time.Duration) {
		if *d <= 0 {
			*d = v
		}
	}
	def(&c.CoalesceWindow, 30*time.Second)
	def(&c.CoalesceMaxWait, 5*time.Minute)
	def(&c.IdleTimeout, 2*time.Minute)
	def(&c.PendingTTL, 2*time.Minute)
	def(&c.RecheckInterval, time.Minute)
	def(&c.PingInterval, 15*time.Second)
	return c
}

// Hub owns the websocket.Server and the rooms' state around it.
type Hub struct {
	files   Files
	access  Access
	store   Store
	metrics *obs.CollabMetrics
	log     *slog.Logger
	cfg     Config
	srv     *websocket.Server

	// mu guards the maps and each room's lifecycle fields (see room). It is
	// never held across I/O, and never taken before a room's own mutexes are
	// released — a room's write-back takes mu briefly, not the reverse.
	mu     sync.Mutex
	byNode map[pgtype.UUID]*room
	byName map[string]*room

	stop     chan struct{}
	stopOnce sync.Once
	loopDone chan struct{}
}

var _ Service = (*Hub)(nil)

// New builds the hub. Start it before serving and Shut it down after the HTTP
// server has stopped accepting connections.
func New(files Files, access Access, st Store, metrics *obs.CollabMetrics, cfg Config) *Hub {
	cfg = cfg.withDefaults()
	h := &Hub{
		files:    files,
		access:   access,
		store:    st,
		metrics:  metrics,
		log:      slog.Default().With("module", "collab"),
		cfg:      cfg,
		byNode:   make(map[pgtype.UUID]*room),
		byName:   make(map[string]*room),
		stop:     make(chan struct{}),
		loopDone: make(chan struct{}),
	}

	// The persistence adapter is only the flush signal: StoreUpdate's batch
	// is never stored — the room is serialized whole and compared with the
	// current version (see writeBack). Deltas replayed against a regenerated
	// seed lose content, which the spike reproduced.
	srv := websocket.NewServerWithPersistence(persistence{h})
	srv.HocuspocusFraming = true
	srv.PingInterval = cfg.PingInterval
	srv.Authorize = h.authorize
	// OnTokenAuth stays nil. When set it replaces Authorize's read-only
	// decision with whatever the client's token says — the spike made a
	// reader writable with somebody else's token.
	srv.OnLoadDocument = h.onLoad
	srv.OnUnloadDocument = h.onUnload
	srv.PersistCoalesceWindow = cfg.CoalesceWindow
	srv.PersistCoalesceMaxWait = cfg.CoalesceMaxWait
	srv.RoomIdleTimeout = cfg.IdleTimeout
	if cfg.Origin != "" {
		srv.AllowedOrigins = []string{cfg.Origin}
	}
	// Resource caps. A document is a person's writing, not a data set: these
	// sit well above any real one and well below what would hurt the process.
	srv.MaxRooms = 1024
	srv.MaxResidentRooms = 256
	srv.MaxPeersPerRoom = 64
	srv.MaxConnections = 4096
	srv.MaxMessageBytes = 32 << 20
	srv.MaxUpdateBytes = 32 << 20
	srv.MessageRateLimit = 100
	srv.MessageRateBurst = 400
	srv.MaxAwarenessBytesPerRoom = 8 << 20
	srv.MaxAwarenessClientsPerRoom = 1024
	srv.AwarenessExpiry = time.Minute
	srv.Logger = h.log
	h.srv = srv
	return h
}

// Start clears the locks a previous process's rooms left behind — with one
// instance, no room can be resident before this one serves — and starts the
// re-resolution loop, which runs until Shutdown or ctx ends.
func (h *Hub) Start(ctx context.Context) error {
	n, err := h.store.DeleteLocksWithPrefix(ctx, lockPrefix)
	if err != nil {
		return err
	}
	if n > 0 {
		h.log.InfoContext(ctx, "released stale room locks", "count", n)
	}
	go h.loop(ctx)
	return nil
}

func (h *Hub) loop(ctx context.Context) {
	defer close(h.loopDone)
	t := time.NewTicker(h.cfg.RecheckInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-h.stop:
			return
		case <-t.C:
			h.Recheck(ctx)
		}
	}
}

// Shutdown closes every socket and writes every room back, then releases the
// locks of rooms still resident (an idle room is not unloaded by it). Call it
// after the HTTP server has stopped accepting: a socket accepted during it
// could open a room nothing waits for.
func (h *Hub) Shutdown(ctx context.Context) error {
	h.stopOnce.Do(func() { close(h.stop) })
	select {
	case <-h.loopDone:
	case <-ctx.Done():
	}
	err := h.srv.Shutdown(ctx)

	h.mu.Lock()
	var held []*room
	for _, rm := range h.byName {
		if rm.lockToken != "" {
			held = append(held, rm)
		}
	}
	h.mu.Unlock()
	for _, rm := range held {
		h.releaseLock(context.WithoutCancel(ctx), rm)
	}
	return err
}

// Open returns the document's room. A resident room, or one handed out and
// not yet joined, keeps its epoch; otherwise a new one is minted. Lossy comes
// from parsing the current version, which a resident room already did.
func (h *Hub) Open(ctx context.Context, doc store.Node) (Session, error) {
	now := time.Now()
	h.mu.Lock()
	if rm := h.byNode[doc.ID]; rm != nil && rm.loaded {
		s := Session{Room: rm.name, Lossy: rm.lossy}
		h.mu.Unlock()
		return s, nil
	}
	h.mu.Unlock()

	parsed, err := h.parseCurrent(ctx, doc.ID)
	if err != nil {
		return Session{}, err
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	rm := h.byNode[doc.ID]
	switch {
	case rm != nil && rm.loaded:
		return Session{Room: rm.name, Lossy: rm.lossy}, nil
	case rm != nil && h.pendingLive(rm, now):
		rm.expires = now.Add(h.cfg.PendingTTL)
		return Session{Room: rm.name, Lossy: parsed.Lossy}, nil
	case rm != nil:
		h.forgetLocked(rm)
	}
	rm = &room{nodeID: doc.ID, epoch: newEpoch(), expires: now.Add(h.cfg.PendingTTL), conns: map[*conn]struct{}{}}
	rm.name = doc.ID.String() + "." + rm.epoch
	h.byNode[rm.nodeID] = rm
	h.byName[rm.name] = rm
	return Session{Room: rm.name, Lossy: parsed.Lossy}, nil
}

// pendingLive reports whether a room that has not loaded still holds its
// epoch: it is loading, a peer has been admitted to it, or it has not expired.
// Caller holds mu.
func (h *Hub) pendingLive(rm *room, now time.Time) bool {
	return rm.loading || len(rm.conns) > 0 || now.Before(rm.expires)
}

// forgetLocked drops a room from the maps. Caller holds mu.
func (h *Hub) forgetLocked(rm *room) {
	if h.byName[rm.name] == rm {
		delete(h.byName, rm.name)
	}
	if h.byNode[rm.nodeID] == rm {
		delete(h.byNode, rm.nodeID)
	}
}

// ServeSocket runs one socket through ygo. The request context is made
// cancellable so a single peer can be closed: ygo has no per-peer close, and
// cancelling the request closes its socket and leaves the room and every
// other peer alone. The client sees 1006 and asks the server again.
func (h *Hub) ServeSocket(w http.ResponseWriter, r *http.Request, room string, peer Peer) {
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	c := &conn{peer: peer, cancel: cancel}
	r = r.WithContext(context.WithValue(ctx, connKey{}, c))
	// ygo names the room from this path value; chi's own is {room} too, but
	// the handler must not depend on how the edge spelled its pattern.
	r.SetPathValue("room", room)
	defer h.release(c)
	h.srv.ServeHTTP(w, r)
}

// connKey carries the socket's *conn from ServeSocket to authorize.
type connKey struct{}

// release forgets a socket that authorize admitted, once ygo is done with it.
func (h *Hub) release(c *conn) {
	h.mu.Lock()
	rm := c.room
	if rm == nil {
		h.mu.Unlock()
		return
	}
	delete(rm.conns, c)
	if len(rm.conns) == 0 {
		rm.emptySince = time.Now()
	}
	h.mu.Unlock()
	h.metrics.PeerLeft()
}

// roomNode is the node a room name addresses.
func roomNode(name string) (pgtype.UUID, bool) {
	i := strings.LastIndexByte(name, '.')
	if i <= 0 {
		return pgtype.UUID{}, false
	}
	var id pgtype.UUID
	if err := id.Scan(name[:i]); err != nil {
		return pgtype.UUID{}, false
	}
	return id, true
}

func newEpoch() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b) // crypto/rand.Read never fails
	return hex.EncodeToString(b)
}

// parseCurrent reads and parses a document's current version.
func (h *Hub) parseCurrent(ctx context.Context, nodeID pgtype.UUID) (mdoc.Parsed, error) {
	_, ver, rc, err := h.files.OpenFile(ctx, nodeID)
	if err != nil {
		return mdoc.Parsed{}, err
	}
	defer rc.Close()
	if !fs.IsMarkdown(ver.Mime) {
		return mdoc.Parsed{}, ErrNotMarkdown
	}
	if ver.Size > maxDocumentBytes {
		return mdoc.Parsed{}, ErrTooLarge
	}
	src, err := io.ReadAll(io.LimitReader(rc, maxDocumentBytes+1))
	if err != nil {
		return mdoc.Parsed{}, err
	}
	if len(src) > maxDocumentBytes {
		return mdoc.Parsed{}, ErrTooLarge
	}
	return mdoc.Parse(src)
}
