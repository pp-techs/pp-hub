package collab

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/reearth/ygo/crdt"
	"github.com/reearth/ygo/provider/websocket"

	"github.com/nhatbui/pp-hub/apps/server/internal/blob"
	"github.com/nhatbui/pp-hub/apps/server/internal/fs"
	"github.com/nhatbui/pp-hub/apps/server/internal/mdoc"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

// room is one document's collaboration session, from the epoch Open hands out
// until ygo unloads it.
type room struct {
	nodeID pgtype.UUID
	epoch  string
	name   string

	// Lifecycle, guarded by Hub.mu. A room is pending (handed out, maybe
	// joined, not loaded), loading (ygo is in LoadDoc), then loaded until
	// onUnload forgets it.
	loading    bool
	loaded     bool
	expires    time.Time // pending only
	lossy      bool      // loaded only; Open answers pending rooms from a fresh parse
	lockToken  string
	conns      map[*conn]struct{}
	emptySince time.Time

	// mu serialises write-back and reseed, and guards doc, frontmatter and
	// replacing (set while Reseed swaps the file under the room).
	mu          sync.Mutex
	doc         *crdt.Doc
	frontmatter []byte
	replacing   bool
	// failing is set while write-back keeps failing, so the room must not be
	// closed out from under the edits it still holds in memory.
	failing atomic.Bool

	// wmu guards writer, the user behind the latest change to the document,
	// and deleted, every deletion the room has seen (see observe). Separate
	// from mu: the update observer runs on whatever goroutine committed,
	// which may hold mu.
	wmu     sync.Mutex
	writer  pgtype.UUID
	deleted *crdt.IDSet
}

// conn is one admitted socket.
type conn struct {
	peer     Peer
	cancel   context.CancelFunc
	readOnly bool
	room     *room // set by authorize, under Hub.mu
}

// authorize is ygo's Authorize: it runs before the upgrade, and a refusal is a
// plain 401. It requires download — sync hands the whole document to the
// browser, the line edit-session draws for OnlyOffice — and makes a reader
// read-only, which ygo enforces by dropping its writes and its awareness.
func (h *Hub) authorize(r *http.Request) (websocket.ConnectionConfig, bool) {
	ctx := r.Context()
	c, _ := ctx.Value(connKey{}).(*conn)
	if c == nil {
		return websocket.ConnectionConfig{}, false
	}
	name := r.PathValue("room")
	nodeID, ok := roomNode(name)
	if !ok || !h.current(name, time.Now()) {
		return websocket.ConnectionConfig{}, false
	}
	node, err := h.files.Get(ctx, nodeID)
	if err != nil || node.Kind != store.NodeKindFile {
		return websocket.ConnectionConfig{}, false
	}
	ver, err := h.store.GetCurrentVersion(ctx, nodeID)
	if err != nil || !fs.IsMarkdown(ver.Mime) {
		return websocket.ConnectionConfig{}, false
	}
	caps, err := h.access.EffectiveCapsForNode(ctx, node, c.peer.User)
	if err != nil {
		h.log.WarnContext(ctx, "resolve collab access", "node", nodeID, "err", err)
		return websocket.ConnectionConfig{}, false
	}
	if !caps.Download {
		return websocket.ConnectionConfig{}, false
	}
	c.readOnly = !caps.Edit

	// Admit under the lock, re-checking the epoch: the room may have
	// unloaded while access was being resolved.
	h.mu.Lock()
	rm := h.byName[name]
	if rm == nil || !(rm.loaded || h.pendingLive(rm, time.Now())) {
		h.mu.Unlock()
		return websocket.ConnectionConfig{}, false
	}
	c.room = rm
	rm.conns[c] = struct{}{}
	rm.emptySince = time.Time{}
	h.mu.Unlock()
	h.metrics.PeerJoined()
	return websocket.ConnectionConfig{ReadOnly: c.readOnly, Origin: c.peer.User}, true
}

// current reports whether name is the epoch its document's room holds now.
func (h *Hub) current(name string, now time.Time) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	rm := h.byName[name]
	return rm != nil && (rm.loaded || h.pendingLive(rm, now))
}

// persistence is the ygo adapter. It is the flush signal and the loader,
// never a store: see Hub.writeBack.
type persistence struct{ h *Hub }

// LoadDoc seeds a room from its document's current version. Only the pending
// epoch loads, once: a room ygo evicted and is asked to recreate — a stale
// client, a reseed racing the unload — is refused, so a name never has two
// generations.
func (p persistence) LoadDoc(name string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return p.h.load(ctx, name)
}

// StoreUpdate is the flush signal without a context; ygo prefers
// StoreUpdateContext.
func (p persistence) StoreUpdate(name string, update []byte) error {
	return p.StoreUpdateContext(context.Background(), name, update)
}

// StoreUpdateContext writes the room back when a coalesced batch is due. The
// two-byte empty updates a join and an unload produce carry nothing. Once the
// document has deletions, a join's update carries its whole delete set (see
// observe) and does get here, costing a write-back that adds no version for
// the join itself. ctx ends when the server shuts down; ygo retries a batch
// that failed for it with a fresh context on its way out.
func (p persistence) StoreUpdateContext(ctx context.Context, name string, update []byte) error {
	if len(update) <= 2 {
		return nil
	}
	h := p.h
	h.mu.Lock()
	rm := h.byName[name]
	h.mu.Unlock()
	if rm == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	return h.writeBack(ctx, rm)
}

func (h *Hub) load(ctx context.Context, name string) ([]byte, error) {
	h.mu.Lock()
	rm := h.byName[name]
	if rm == nil || rm.loaded || rm.loading {
		h.mu.Unlock()
		return nil, errRoomGone
	}
	rm.loading = true
	h.mu.Unlock()

	seed, token, parsed, err := h.seed(ctx, rm)
	h.mu.Lock()
	rm.loading = false
	if err != nil {
		// The client asks for a room again and gets a new epoch.
		h.forgetLocked(rm)
		h.mu.Unlock()
		h.log.WarnContext(ctx, "load room", "room", name, "err", err)
		return nil, err
	}
	rm.loaded = true
	rm.lossy = parsed.Lossy
	rm.lockToken = token
	h.mu.Unlock()
	h.metrics.RoomLoaded()
	return seed, nil
}

// seed takes the file's lock for the room and builds its initial state.
func (h *Hub) seed(ctx context.Context, rm *room) ([]byte, string, mdoc.Parsed, error) {
	// The lock first: a room that cannot hold the file must not start. While
	// it is held a REST replacement is refused (423), as for OnlyOffice.
	token := lockPrefix + rm.name
	n, err := h.store.AcquireLock(ctx, store.AcquireLockParams{NodeID: rm.nodeID, LockToken: token})
	if err != nil {
		return nil, "", mdoc.Parsed{}, err
	}
	if n == 0 {
		return nil, "", mdoc.Parsed{}, errLocked
	}
	parsed, err := h.parseCurrent(ctx, rm.nodeID)
	var seed []byte
	if err == nil {
		seed, err = mdoc.Seed(parsed.Doc)
	}
	if err != nil {
		_ = h.store.DeleteLock(ctx, store.DeleteLockParams{NodeID: rm.nodeID, LockToken: token})
		return nil, "", mdoc.Parsed{}, err
	}
	// Frontmatter lives outside the fragment — the editor would turn it into
	// a rule and a heading — and goes back in front of every write.
	rm.mu.Lock()
	rm.frontmatter = parsed.Frontmatter
	rm.mu.Unlock()
	return seed, token, parsed, nil
}

// onLoad keeps the room's live document and attributes its changes (observe).
func (h *Hub) onLoad(_ context.Context, name string, doc *crdt.Doc) error {
	h.mu.Lock()
	rm := h.byName[name]
	h.mu.Unlock()
	if rm == nil {
		return errRoomGone
	}
	rm.mu.Lock()
	rm.doc = doc
	rm.mu.Unlock()
	rm.wmu.Lock()
	rm.deleted = crdt.DeleteSetFromDoc(doc)
	rm.wmu.Unlock()
	doc.OnUpdate(rm.observe)
	return nil
}

// observe records the user behind a change to the document. ygo passes the
// peer as the origin of the updates it applies, and the patched
// ConnectionOrigin maps that back to the user authorize admitted; a server
// write (a reseed) maps to nobody.
//
// Only a transaction that changed the document counts, and the update's size
// does not tell: ygo hands its observers the transaction's new structs
// together with the document's whole delete set, so once anything has been
// deleted, the empty sync a peer answers the join handshake with arrives as a
// non-empty update — and would make whoever opened the document the author
// of the edits somebody else made before them. A change inserts structs or
// deletes something not deleted before.
func (rm *room) observe(update []byte, origin any) {
	ids, err := crdt.ContentIDsFromUpdateV1(update)
	if err != nil {
		return
	}
	rm.wmu.Lock()
	defer rm.wmu.Unlock()
	changed := !ids.Inserts.IsEmpty() || !crdt.ExcludeIDSet(ids.Deletes, rm.deleted).IsEmpty()
	// Merged rather than replaced: observers of concurrent transactions may
	// run out of commit order, and an older, smaller set must not undo a
	// newer one.
	rm.deleted = crdt.MergeIDSets(rm.deleted, ids.Deletes)
	if !changed {
		return
	}
	v, ok := websocket.ConnectionOrigin(origin)
	if !ok {
		return
	}
	if user, ok := v.(pgtype.UUID); ok {
		rm.writer = user
	}
}

// onUnload forgets a room ygo evicted and releases its lock.
func (h *Hub) onUnload(ctx context.Context, name string) {
	h.mu.Lock()
	rm := h.byName[name]
	if rm == nil {
		h.mu.Unlock()
		return
	}
	h.forgetLocked(rm)
	wasLoaded := rm.loaded
	h.mu.Unlock()
	h.releaseLock(ctx, rm)
	if wasLoaded {
		h.metrics.RoomUnloaded()
	}
}

func (h *Hub) releaseLock(ctx context.Context, rm *room) {
	h.mu.Lock()
	token := rm.lockToken
	rm.lockToken = ""
	h.mu.Unlock()
	if token == "" {
		return
	}
	if err := h.store.DeleteLock(ctx, store.DeleteLockParams{NodeID: rm.nodeID, LockToken: token}); err != nil {
		h.log.WarnContext(ctx, "release room lock", "room", rm.name, "err", err)
	}
}

// writeBack serializes the room and makes it the file's next version — only
// when the markdown differs from the current version's bytes, so a session
// that changed nothing, or changed it back, adds no history. The version goes
// through fs like any other: claimed blob, quota, retention, preview.
//
// It is authored by the user behind the latest change. A change with no user
// behind it is a reseed, whose content is the current version by
// construction; if its markdown still differs, that is the serializer
// normalizing a restored file, and no write happens for it — a version
// "by" the document's owner, or anybody else, would record an edit nobody
// made. The next real edit writes the normalized form with its author.
//
// A failure is returned so ygo keeps the batch and the room: the edits live
// on in memory, and the next flush — or the next eviction attempt — retries.
// A document that is gone (trashed, purged, no longer markdown) has nowhere
// to go, and its edits are dropped with a log line.
//
// While the file under the room is being replaced (Reseed) nothing is
// written: the room still holds the old document, and writing it would undo
// the replacement. The batch skipped is not lost — the reseed that ends the
// replacement is itself an update, whose flush serializes the whole room.
func (h *Hub) writeBack(ctx context.Context, rm *room) error {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	if rm.doc == nil || rm.replacing {
		return nil
	}
	return h.persistTimed(ctx, rm)
}

// persistTimed is persist, measured and logged. Caller holds rm.mu.
func (h *Hub) persistTimed(ctx context.Context, rm *room) error {
	start := time.Now()
	err := h.persist(ctx, rm)
	h.metrics.Flushed(time.Since(start), err != nil)
	rm.failing.Store(err != nil)
	if err != nil {
		h.log.ErrorContext(ctx, "write back room", "room", rm.name, "err", err)
	}
	return err
}

// persist is writeBack's body. Caller holds rm.mu.
func (h *Hub) persist(ctx context.Context, rm *room) error {
	frag := rm.doc.GetXmlFragment(mdoc.FragmentName)
	out := mdoc.Render(mdoc.Read(frag), rm.frontmatter)

	ver, err := h.store.GetCurrentVersion(ctx, rm.nodeID)
	if errors.Is(err, pgx.ErrNoRows) {
		h.log.WarnContext(ctx, "room's document is gone; edits dropped", "room", rm.name)
		return nil
	}
	if err != nil {
		return err
	}
	if !fs.IsMarkdown(ver.Mime) {
		h.log.WarnContext(ctx, "room's document is no longer markdown; edits dropped", "room", rm.name)
		return nil
	}
	if blob.Hash(out) == ver.BlobHash {
		return nil
	}
	rm.wmu.Lock()
	writer := rm.writer
	rm.wmu.Unlock()
	if !writer.Valid {
		return nil
	}
	node, err := h.files.Get(ctx, rm.nodeID)
	if err == nil {
		_, _, err = h.files.PutFile(ctx, node.SpaceID, node.ParentID, node.ID, node.Name, bytes.NewReader(out), writer)
	}
	if errors.Is(err, fs.ErrNotFound) || errors.Is(err, fs.ErrNotFile) {
		h.log.WarnContext(ctx, "room's document is gone; edits dropped", "room", rm.name)
		return nil
	}
	return err
}

// loadedRoom is the document's resident room, or nil.
func (h *Hub) loadedRoom(nodeID pgtype.UUID) *room {
	h.mu.Lock()
	defer h.mu.Unlock()
	if rm := h.byNode[nodeID]; rm != nil && rm.loaded {
		return rm
	}
	return nil
}

// Reseed runs change — anything that replaces the file under a room, such as
// restoring an older version — and makes a resident room hold the result.
// Without a room, it is just change: the next load parses the current
// version.
//
// Edits the room has not written yet go first, as a version of their own, so
// the replacement is one restore away from undoing. Then the room's blocks are
// all deleted and the parsed file inserted in one server transaction
// (Server.Apply): every peer sees the new document at once and keeps the
// epoch. A room with nobody in it is closed instead — ygo stops counting an
// Apply-touched room as idle, so it would never unload — and the next join
// loads the new version.
func (h *Hub) Reseed(ctx context.Context, nodeID pgtype.UUID, change func(context.Context) error) error {
	rm := h.loadedRoom(nodeID)
	if rm == nil {
		return change(ctx)
	}
	rm.mu.Lock()
	if rm.doc != nil {
		// A failure is logged, not returned: the edits it could not write
		// are the ones the caller asked to replace.
		_ = h.persistTimed(ctx, rm)
	}
	if err := change(ctx); err != nil {
		rm.mu.Unlock()
		return err
	}
	rm.replacing = true
	rm.wmu.Lock()
	rm.writer = pgtype.UUID{} // an edit after the reseed is its author's again
	rm.wmu.Unlock()
	rm.mu.Unlock()
	defer func() {
		rm.mu.Lock()
		rm.replacing = false
		rm.mu.Unlock()
	}()

	if h.peers(rm) == 0 {
		err := h.srv.CloseRoom(rm.name, false)
		if !errors.Is(err, websocket.ErrRoomHasPeers) {
			h.closed(ctx, rm, err)
			return nil
		}
		// Somebody joined meanwhile: reseed them.
	}
	parsed, err := h.parseCurrent(ctx, nodeID)
	if err != nil {
		// The replacement cannot be edited here (no longer markdown, too
		// large): nobody may keep editing the old one either.
		h.log.WarnContext(ctx, "reseed room: closing it", "room", rm.name, "err", err)
		h.closed(ctx, rm, h.srv.CloseRoom(rm.name, true))
		return nil
	}
	err = h.srv.Apply(ctx, rm.name, func(doc *crdt.Doc, transact func(func(*crdt.Transaction))) {
		frag := doc.GetXmlFragment(mdoc.FragmentName) // outside transact: it takes the doc lock
		transact(func(txn *crdt.Transaction) {
			if n := frag.Len(); n > 0 {
				frag.Delete(txn, 0, n)
			}
			mdoc.Fill(txn, frag, parsed.Doc)
		})
	})
	switch {
	case err == nil:
		rm.mu.Lock()
		rm.frontmatter = parsed.Frontmatter
		rm.mu.Unlock()
		h.mu.Lock()
		rm.lossy = parsed.Lossy
		h.mu.Unlock()
	case errors.Is(err, errRoomGone):
		// Unloaded meanwhile, and LoadDoc refused to recreate it: the next
		// load parses the new version.
		return nil
	default:
		// The file is replaced; only the room failed to follow. Close it so
		// every peer rejoins onto the new version rather than editing the
		// old one.
		h.log.ErrorContext(ctx, "reseed room: closing it", "room", rm.name, "err", err)
		h.closed(ctx, rm, h.srv.CloseRoom(rm.name, true))
		return nil
	}
	if h.peers(rm) == 0 {
		// The last peer left while Apply ran.
		h.closed(ctx, rm, h.srv.CloseRoom(rm.name, false))
	}
	return nil
}

func (h *Hub) peers(rm *room) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(rm.conns)
}

// closed settles a CloseRoom. ygo writes the room back, evicts it and fires
// onUnload itself; a room it no longer has — its load failed after LoadDoc,
// so no unload ever fired — is forgotten here.
func (h *Hub) closed(ctx context.Context, rm *room, err error) {
	switch {
	case err == nil, errors.Is(err, websocket.ErrRoomHasPeers), errors.Is(err, websocket.ErrServerShutdown):
	case errors.Is(err, websocket.ErrRoomNotFound):
		h.onUnload(ctx, rm.name)
	default:
		h.log.WarnContext(ctx, "close room", "room", rm.name, "err", err)
	}
}
