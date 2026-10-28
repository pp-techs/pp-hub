package httpapi

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	gws "github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/reearth/ygo/crdt"
	"github.com/reearth/ygo/encoding"
	ygsync "github.com/reearth/ygo/sync"

	"github.com/nhatbui/pp-hub/apps/server/internal/audit"
	"github.com/nhatbui/pp-hub/apps/server/internal/auth"
	"github.com/nhatbui/pp-hub/apps/server/internal/collab"
	"github.com/nhatbui/pp-hub/apps/server/internal/config"
	"github.com/nhatbui/pp-hub/apps/server/internal/fs"
	"github.com/nhatbui/pp-hub/apps/server/internal/mdoc"
	"github.com/nhatbui/pp-hub/apps/server/internal/obs"
	"github.com/nhatbui/pp-hub/apps/server/internal/share"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

// A block document's room is a websocket session whose admission, kicks,
// write-back and reseed all resolve against real grants, sessions, locks and
// versions — so these run the production router, a real collab hub and a real
// websocket client against a real Postgres, or skip loudly like the rest of
// this package's database tests:
//
//	PP_TEST_DATABASE_URL=postgres://pphub:pphub@localhost:5433/pphub go test ./internal/httpapi/

// collabRig is srv.Router() served over HTTP, authenticating real session
// cookies, with a collab hub whose timings suit a test.
type collabRig struct {
	srv *Server
	q   *store.Queries
	hub *collab.Hub
	reg *prometheus.Registry
	// url is the test server's base URL, and this deployment's origin.
	url string

	mu      sync.Mutex
	cookies map[pgtype.UUID]string
	stop    sync.Once
}

func newCollabRig(t *testing.T, cfg collab.Config) *collabRig {
	t.Helper()
	return newCollabRigWith(t, cfg, nil)
}

// newCollabRigWith is newCollabRig with a chance to change the server before
// its router is built — routes are mounted when Router runs.
func newCollabRigWith(t *testing.T, cfg collab.Config, setup func(*Server)) *collabRig {
	t.Helper()
	srv, q, _ := uploadEnv(t)
	srv.audit = audit.New(q)
	ctx := context.Background()

	// auth.Service reads the IdP's discovery document when it is built and
	// nothing else from it to authenticate a session cookie.
	var idp *httptest.Server
	idp = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                                idp.URL,
			"authorization_endpoint":                idp.URL + "/authorize",
			"token_endpoint":                        idp.URL + "/token",
			"jwks_uri":                              idp.URL + "/jwks",
			"end_session_endpoint":                  idp.URL + "/logout",
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	}))
	t.Cleanup(idp.Close)

	ts := httptest.NewUnstartedServer(nil)
	base := "http://" + ts.Listener.Addr().String()
	authSvc, err := auth.NewService(ctx, &config.Config{
		OIDCIssuer: idp.URL, OIDCIssuerExt: idp.URL, OIDCClientID: "pphub-test", ExternalURL: base,
	}, q, nil)
	if err != nil {
		t.Fatalf("auth service: %v", err)
	}
	srv.auth = authSvc
	srv.externalURL = base

	reg := prometheus.NewRegistry()
	cfg.Origin = base
	if cfg.IdleTimeout == 0 {
		cfg.IdleTimeout = 200 * time.Millisecond
	}
	if cfg.CoalesceWindow == 0 {
		cfg.CoalesceWindow = 50 * time.Millisecond
		cfg.CoalesceMaxWait = 200 * time.Millisecond
	}
	// Recheck runs when a test calls it, not on a timer.
	cfg.RecheckInterval = time.Hour
	hub := collab.New(srv.fs, srv.share, q, obs.NewCollabMetrics(reg), cfg)
	if err := hub.Start(ctx); err != nil {
		t.Fatalf("start hub: %v", err)
	}
	srv.collab = hub

	if setup != nil {
		setup(srv)
	}
	ts.Config.Handler = srv.Router()
	ts.Start()
	rig := &collabRig{srv: srv, q: q, hub: hub, reg: reg, url: base, cookies: map[pgtype.UUID]string{}}
	// Cleanups run last-registered first: the hub closes its sockets and
	// writes its rooms back before the server stops and the pool closes.
	t.Cleanup(ts.Close)
	t.Cleanup(func() { rig.shutdown(t) })
	return rig
}

// shutdown stops the hub the way cmd/server does on SIGTERM.
func (rig *collabRig) shutdown(t *testing.T) {
	t.Helper()
	rig.stop.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := rig.hub.Shutdown(ctx); err != nil {
			t.Errorf("hub shutdown: %v", err)
		}
	})
}

// login gives u a live session and remembers its cookie.
func (rig *collabRig) login(t *testing.T, u store.User) store.Session {
	t.Helper()
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		t.Fatal(err)
	}
	token := hex.EncodeToString(raw)
	sum := sha256.Sum256([]byte(token))
	sess, err := rig.q.CreateSession(context.Background(), store.CreateSessionParams{
		UserID: u.ID, TokenHash: sum[:], UserAgent: "collab-test", Ip: "127.0.0.1",
		ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true},
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	rig.mu.Lock()
	rig.cookies[u.ID] = token
	rig.mu.Unlock()
	return sess
}

func (rig *collabRig) cookie(u store.User) string {
	rig.mu.Lock()
	defer rig.mu.Unlock()
	return "pp_session=" + rig.cookies[u.ID]
}

type rigResponse struct {
	code   int
	header http.Header
	body   []byte
}

// do sends one same-origin JSON request as u.
func (rig *collabRig) do(t *testing.T, u store.User, method, path string, body any) rigResponse {
	t.Helper()
	if body == nil {
		return rig.request(t, u, method, path, nil, nil)
	}
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	return rig.request(t, u, method, path, map[string]string{"Content-Type": "application/json"}, bytes.NewReader(b))
}

// request sends one same-origin request as u with the given headers.
func (rig *collabRig) request(t *testing.T, u store.User, method, path string, hdr map[string]string, body io.Reader) rigResponse {
	t.Helper()
	req, err := http.NewRequest(method, rig.url+path, body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", rig.url)
	req.Header.Set("Cookie", rig.cookie(u))
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("%s %s: read body: %v", method, path, err)
	}
	return rigResponse{code: resp.StatusCode, header: resp.Header, body: b}
}

// session is GET /nodes/{id}/collab as u.
func (rig *collabRig) session(t *testing.T, u store.User, doc store.Node) (collabSessionDTO, int) {
	t.Helper()
	res := rig.do(t, u, http.MethodGet, "/api/v1/nodes/"+uuidString(doc.ID)+"/collab", nil)
	var dto collabSessionDTO
	if res.code == http.StatusOK {
		if err := json.Unmarshal(res.body, &dto); err != nil {
			t.Fatalf("decode collab session: %v; body=%s", err, res.body)
		}
	}
	return dto, res.code
}

// mustSession is session, failing unless it is answered 200.
func (rig *collabRig) mustSession(t *testing.T, u store.User, doc store.Node) collabSessionDTO {
	t.Helper()
	dto, code := rig.session(t, u, doc)
	if code != http.StatusOK {
		t.Fatalf("GET collab as %s: %d", u.Name, code)
	}
	return dto
}

// dial opens a room's socket with u's cookie (none when u is nil) and the
// given Origin (none when empty). A refused upgrade returns the status; an
// admitted one returns 101 and a peer that has finished the sync handshake.
func (rig *collabRig) dial(t *testing.T, u *store.User, room, origin string) (*wsPeer, int) {
	t.Helper()
	hdr := http.Header{}
	if origin != "" {
		hdr.Set("Origin", origin)
	}
	if u != nil {
		hdr.Set("Cookie", rig.cookie(*u))
	}
	conn, resp, err := gws.DefaultDialer.Dial("ws"+strings.TrimPrefix(rig.url, "http")+"/api/v1/collab/"+room, hdr)
	if err != nil {
		if resp == nil {
			t.Fatalf("dial %s: %v", room, err)
		}
		_ = resp.Body.Close()
		return nil, resp.StatusCode
	}
	p := &wsPeer{conn: conn, room: room, doc: crdt.New(), done: make(chan struct{})}
	go p.readLoop()
	t.Cleanup(p.close)
	waitUntil(t, "the sync handshake", func() bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		return p.synced && len(p.acks) >= p.writes || p.closed
	})
	if p.isClosed() {
		t.Fatalf("socket to %s closed during the handshake", room)
	}
	return p, http.StatusSwitchingProtocols
}

// mustDial is dial as u with this deployment's Origin, failing unless admitted.
func (rig *collabRig) mustDial(t *testing.T, u store.User, room string) *wsPeer {
	t.Helper()
	p, code := rig.dial(t, &u, room, rig.url)
	if p == nil {
		t.Fatalf("dial %s as %s: %d", room, u.Name, code)
	}
	return p
}

// metric reads one of the hub's series: a histogram's sample count, or a
// gauge's or counter's value.
func (rig *collabRig) metric(t *testing.T, name string) float64 {
	t.Helper()
	fams, err := rig.reg.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	for _, f := range fams {
		if f.GetName() != name {
			continue
		}
		for _, m := range f.GetMetric() {
			switch {
			case m.GetHistogram() != nil:
				return float64(m.GetHistogram().GetSampleCount())
			case m.GetGauge() != nil:
				return m.GetGauge().GetValue()
			case m.GetCounter() != nil:
				return m.GetCounter().GetValue()
			}
		}
	}
	t.Fatalf("no metric %s", name)
	return 0
}

// flushes counts the room write-backs so far, whether or not a version came
// of them.
func (rig *collabRig) flushes(t *testing.T) float64 {
	t.Helper()
	return rig.metric(t, "pphub_collab_flush_duration_seconds")
}

// wsPeer is a Hocuspocus-framed Yjs client: it answers the server's sync
// step 1, applies what the room sends, and records the SyncStatus the server
// acknowledges each of its writes with.
type wsPeer struct {
	conn *gws.Conn
	room string
	doc  *crdt.Doc
	done chan struct{}

	// wmu serializes writes on the socket and orders writes with acks: the
	// server acknowledges every SyncStep2 and Update it receives, in order.
	wmu sync.Mutex

	mu     sync.Mutex
	writes int
	acks   []bool
	synced bool
	closed bool
}

func (p *wsPeer) readLoop() {
	defer close(p.done)
	for {
		_, data, err := p.conn.ReadMessage()
		if err != nil {
			p.mu.Lock()
			p.closed = true
			p.mu.Unlock()
			return
		}
		if len(data) == 1 {
			continue // the connection-level Ping
		}
		dec := encoding.NewDecoder(data)
		if _, err := dec.ReadVarString(); err != nil {
			continue
		}
		typ, err := dec.ReadVarUint()
		if err != nil {
			continue
		}
		switch typ {
		case 0: // Sync
			payload := dec.RemainingBytes()
			sub, _, err := ygsync.ReadSyncMessage(payload)
			if err != nil {
				continue
			}
			reply, err := ygsync.ApplySyncMessage(p.doc, payload, "server")
			if err != nil {
				continue
			}
			if reply != nil {
				p.send(reply)
			}
			if sub == ygsync.MsgSyncStep2 {
				p.mu.Lock()
				p.synced = true
				p.mu.Unlock()
			}
		case 8: // SyncStatus
			flag, err := dec.ReadVarUint()
			if err != nil {
				continue
			}
			p.mu.Lock()
			p.acks = append(p.acks, flag == 1)
			p.mu.Unlock()
		}
	}
}

// send writes one sync message and returns the index of the acknowledgement
// that answers it.
func (p *wsPeer) send(syncMsg []byte) int {
	enc := encoding.NewEncoder()
	enc.WriteVarString(p.room)
	enc.WriteVarUint(0)
	enc.WriteRaw(syncMsg)
	p.wmu.Lock()
	defer p.wmu.Unlock()
	p.mu.Lock()
	idx := p.writes
	p.writes++
	p.mu.Unlock()
	_ = p.conn.WriteMessage(gws.BinaryMessage, enc.Bytes())
	return idx
}

// edit replaces the whole document with md's blocks, the way a client
// rewriting everything would, and reports whether the server applied it.
func (p *wsPeer) edit(t *testing.T, md string) bool {
	t.Helper()
	parsed, err := mdoc.Parse([]byte(md))
	if err != nil {
		t.Fatalf("parse %q: %v", md, err)
	}
	frag := p.doc.GetXmlFragment(mdoc.FragmentName)
	return p.change(t, func(txn *crdt.Transaction) {
		if n := frag.Len(); n > 0 {
			frag.Delete(txn, 0, n)
		}
		mdoc.Fill(txn, frag, parsed.Doc)
	})
}

// setMeta sets the page's icon and cover in the room's meta map, as the page
// header does, an empty value deleting the key.
func (p *wsPeer) setMeta(t *testing.T, icon, cover string) bool {
	t.Helper()
	meta := p.doc.GetMap("meta")
	return p.change(t, func(txn *crdt.Transaction) {
		for k, v := range map[string]string{"icon": icon, "cover": cover} {
			if v == "" {
				meta.Delete(txn, k)
			} else {
				meta.Set(txn, k, v)
			}
		}
	})
}

// meta is the icon and cover the peer's copy of the meta map holds.
func (p *wsPeer) meta() (icon, cover any) {
	e := p.doc.GetMap("meta").Entries()
	return e["icon"], e["cover"]
}

// change sends what fn does to the peer's document and reports whether the
// server applied it.
func (p *wsPeer) change(t *testing.T, fn func(*crdt.Transaction)) bool {
	t.Helper()
	sv := p.doc.StateVector()
	p.doc.Transact(fn)
	idx := p.send(ygsync.EncodeUpdate(crdt.EncodeStateAsUpdateV1(p.doc, sv)))
	var applied bool
	waitUntil(t, "the server's acknowledgement", func() bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		if len(p.acks) > idx {
			applied = p.acks[idx]
			return true
		}
		return p.closed
	})
	if p.isClosed() && !applied {
		t.Fatalf("socket to %s closed before the change was acknowledged", p.room)
	}
	return applied
}

// markdown is what the peer's copy of the document serializes to.
func (p *wsPeer) markdown() string {
	return string(mdoc.Render(mdoc.Read(p.doc.GetXmlFragment(mdoc.FragmentName)), nil))
}

func (p *wsPeer) isClosed() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.closed
}

func (p *wsPeer) close() {
	_ = p.conn.Close()
	<-p.done
}

func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// canonical is md as the block editor writes it back.
func canonical(t *testing.T, md string) string {
	t.Helper()
	parsed, err := mdoc.Parse([]byte(md))
	if err != nil {
		t.Fatalf("parse %q: %v", md, err)
	}
	return string(mdoc.Render(parsed.Doc, parsed.Frontmatter))
}

// collabFixture is a markdown document owned by one person, with one person
// per access level around it, each signed in.
type collabFixture struct {
	space    store.Space
	owner    store.User
	editor   store.User // edit
	reader   store.User // view + download
	viewer   store.User // view only
	stranger store.User // space member without a grant
	doc      store.Node
}

func (rig *collabRig) fixture(t *testing.T, label, content string) collabFixture {
	t.Helper()
	ctx := context.Background()
	owner := feedUser(t, rig.q, label+"-owner")
	sp, err := rig.srv.spaces.Create(ctx, fmt.Sprintf("collab-%s-%d", label, os.Getpid()), owner.ID)
	if err != nil {
		t.Fatalf("create space: %v", err)
	}
	t.Cleanup(func() { _ = rig.q.DeleteSpace(ctx, sp.ID) })
	f := collabFixture{space: sp, owner: owner}
	for _, m := range []struct {
		u    *store.User
		role string
	}{{&f.editor, "editor"}, {&f.reader, "reader"}, {&f.viewer, "viewer"}, {&f.stranger, "stranger"}} {
		*m.u = feedUser(t, rig.q, label+"-"+m.role)
		if err := rig.srv.spaces.AddMember(ctx, sp.ID, m.u.ID, false); err != nil {
			t.Fatalf("add member: %v", err)
		}
	}
	f.doc = rig.putMarkdown(t, sp, owner, label+".md", content)
	grantDirect(t, rig.srv, f.doc, f.editor, owner, share.GrantCaps{Level: share.LevelEdit})
	grantDirect(t, rig.srv, f.doc, f.reader, owner, share.GrantCaps{Level: share.LevelView, CanDownload: true})
	grantDirect(t, rig.srv, f.doc, f.viewer, owner, share.GrantCaps{Level: share.LevelView})
	for _, u := range []store.User{f.owner, f.editor, f.reader, f.viewer, f.stranger} {
		rig.login(t, u)
	}
	return f
}

// putMarkdown stores a new top-level file in sp.
func (rig *collabRig) putMarkdown(t *testing.T, sp store.Space, by store.User, name, content string) store.Node {
	t.Helper()
	node, _, err := rig.srv.fs.PutFile(context.Background(), sp.ID, pgtype.UUID{}, pgtype.UUID{}, name, strings.NewReader(content), by.ID)
	if err != nil {
		t.Fatalf("put %s: %v", name, err)
	}
	return node
}

// putVersion stores content as doc's next version.
func (rig *collabRig) putVersion(t *testing.T, doc store.Node, by store.User, content string) store.NodeVersion {
	t.Helper()
	_, ver, err := rig.srv.fs.PutFile(context.Background(), doc.SpaceID, doc.ParentID, doc.ID, doc.Name, strings.NewReader(content), by.ID)
	if err != nil {
		t.Fatalf("put version: %v", err)
	}
	return ver
}

// versions lists doc's versions, newest first.
func (rig *collabRig) versions(t *testing.T, doc store.Node) []store.NodeVersion {
	t.Helper()
	vs, err := rig.q.ListVersions(context.Background(), doc.ID)
	if err != nil {
		t.Fatalf("list versions: %v", err)
	}
	return vs
}

// content reads one version's bytes.
func (rig *collabRig) content(t *testing.T, v store.NodeVersion) string {
	t.Helper()
	rc, err := rig.srv.blobs.Get(context.Background(), v.BlobHash)
	if err != nil {
		t.Fatalf("open version %d: %v", v.VersionNo, err)
	}
	defer rc.Close()
	b, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read version %d: %v", v.VersionNo, err)
	}
	return string(b)
}

// Who may join a room, and how: download admits (sync hands over the whole
// document), edit makes the socket writable, and everything else — no
// download, no grant, no session, another origin, a room that is not the
// document's current one — is refused before the upgrade.
func TestCollabAdmitsByCapability(t *testing.T) {
	rig := newCollabRig(t, collab.Config{})
	f := rig.fixture(t, "matrix", "# Plan\n\nFirst draft.\n")

	room := rig.mustSession(t, f.owner, f.doc).Room
	if !strings.HasPrefix(room, uuidString(f.doc.ID)+".") {
		t.Fatalf("room %q is not <nodeId>.<epoch>", room)
	}

	cases := []struct {
		name     string
		u        store.User
		session  int  // GET /collab
		canWrite bool // its can_write
		socket   int  // the upgrade
		writable bool // whether the room applies the socket's edits
	}{
		{"owner", f.owner, http.StatusOK, true, http.StatusSwitchingProtocols, true},
		{"editor", f.editor, http.StatusOK, true, http.StatusSwitchingProtocols, true},
		{"reader", f.reader, http.StatusOK, false, http.StatusSwitchingProtocols, false},
		{"view without download", f.viewer, http.StatusForbidden, false, http.StatusUnauthorized, false},
		{"member without a grant", f.stranger, http.StatusForbidden, false, http.StatusUnauthorized, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dto, code := rig.session(t, tc.u, f.doc)
			if code != tc.session {
				t.Fatalf("GET collab: %d, want %d", code, tc.session)
			}
			if code == http.StatusOK && (dto.Room != room || dto.CanWrite != tc.canWrite || dto.Lossy) {
				t.Fatalf("GET collab: %+v, want room %s can_write %v, not lossy", dto, room, tc.canWrite)
			}
			// Everyone dials the room the owner was handed: the socket must
			// decide on its own, whatever the caller learned elsewhere.
			u := tc.u
			p, code := rig.dial(t, &u, room, rig.url)
			if code != tc.socket {
				t.Fatalf("upgrade: %d, want %d", code, tc.socket)
			}
			if p == nil {
				return
			}
			if applied := p.edit(t, "# Plan\n\nEdited by "+tc.name+".\n"); applied != tc.writable {
				t.Fatalf("edit applied: %v, want %v", applied, tc.writable)
			}
			p.close()
		})
	}

	t.Run("stale epoch", func(t *testing.T) {
		if _, code := rig.dial(t, &f.owner, uuidString(f.doc.ID)+".0123456789abcdef", rig.url); code != http.StatusUnauthorized {
			t.Fatalf("upgrade: %d, want 401", code)
		}
	})
	t.Run("no session", func(t *testing.T) {
		if _, code := rig.dial(t, nil, room, rig.url); code != http.StatusUnauthorized {
			t.Fatalf("upgrade: %d, want 401", code)
		}
	})
	t.Run("foreign origin", func(t *testing.T) {
		if _, code := rig.dial(t, &f.owner, room, "https://evil.example"); code != http.StatusForbidden {
			t.Fatalf("upgrade: %d, want 403", code)
		}
	})
	t.Run("no origin", func(t *testing.T) {
		if _, code := rig.dial(t, &f.owner, room, ""); code != http.StatusForbidden {
			t.Fatalf("upgrade: %d, want 403", code)
		}
	})
	t.Run("not markdown", func(t *testing.T) {
		txt := rig.putMarkdown(t, f.space, f.owner, "notes.txt", "plain text\n")
		if _, code := rig.session(t, f.owner, txt); code != http.StatusBadRequest {
			t.Fatalf("GET collab on a .txt: %d, want 400", code)
		}
		if _, code := rig.dial(t, &f.owner, uuidString(txt.ID)+".0123456789abcdef", rig.url); code != http.StatusUnauthorized {
			t.Fatalf("upgrade to a .txt: %d, want 401", code)
		}
	})
	t.Run("lossy", func(t *testing.T) {
		html := rig.putMarkdown(t, f.space, f.owner, "raw.md", "<div class=\"box\">raw html</div>\n")
		if dto := rig.mustSession(t, f.owner, html); !dto.Lossy {
			t.Fatalf("GET collab on raw HTML: %+v, want lossy", dto)
		}
	})
}

// A reader receives the document but cannot change it: its edits are refused
// with SyncStatus 0, never reach the room, and never become a version.
func TestCollabDropsReadOnlyEdits(t *testing.T) {
	rig := newCollabRig(t, collab.Config{})
	original := canonical(t, "# Minutes\n\nNothing decided yet.\n")
	f := rig.fixture(t, "readonly", original)
	room := rig.mustSession(t, f.owner, f.doc).Room

	owner := rig.mustDial(t, f.owner, room)
	reader := rig.mustDial(t, f.reader, room)
	if got := reader.markdown(); got != original {
		t.Fatalf("reader sees %q, want %q", got, original)
	}
	if reader.edit(t, "# Minutes\n\nThe reader decided everything.\n") {
		t.Fatal("the room applied a reader's edit")
	}

	// A peer joining now gets the room's whole state in its handshake.
	late := rig.mustDial(t, f.editor, room)
	if got := late.markdown(); got != original {
		t.Fatalf("the room holds %q after a reader's edit, want %q", got, original)
	}
	if got := owner.markdown(); got != original {
		t.Fatalf("the reader's edit reached the owner: %q", got)
	}
	rig.shutdown(t)
	if n := len(rig.versions(t, f.doc)); n != 1 {
		t.Fatalf("%d versions after a reader's edit, want 1", n)
	}
}

// Access is re-resolved while a socket lives. A revoked grant, a downgrade
// and a signed-out session each close that one socket; everyone else stays,
// and each comes back the way GET /collab and the upgrade now decide.
func TestCollabRecheckClosesOnlyTheAffectedPeers(t *testing.T) {
	rig := newCollabRig(t, collab.Config{})
	ctx := context.Background()
	f := rig.fixture(t, "recheck", "# Draft\n")
	revoked := feedUser(t, rig.q, "recheck-revoked")
	signedOut := feedUser(t, rig.q, "recheck-signedout")
	for _, u := range []store.User{revoked, signedOut} {
		if err := rig.srv.spaces.AddMember(ctx, f.space.ID, u.ID, false); err != nil {
			t.Fatalf("add member: %v", err)
		}
	}
	revokedShare := grantDirect(t, rig.srv, f.doc, revoked, f.owner, share.GrantCaps{Level: share.LevelEdit})
	grantDirect(t, rig.srv, f.doc, signedOut, f.owner, share.GrantCaps{Level: share.LevelEdit})
	rig.login(t, revoked)
	signedOutSession := rig.login(t, signedOut)
	room := rig.mustSession(t, f.owner, f.doc).Room

	owner := rig.mustDial(t, f.owner, room)
	editor := rig.mustDial(t, f.editor, room) // downgraded below
	revokedPeer := rig.mustDial(t, revoked, room)
	signedOutPeer := rig.mustDial(t, signedOut, room)

	if _, err := rig.q.DeleteShare(ctx, store.DeleteShareParams{ID: revokedShare.ID, NodeID: f.doc.ID}); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	grantDirect(t, rig.srv, f.doc, f.editor, f.owner, share.GrantCaps{Level: share.LevelView, CanDownload: true})
	if _, err := rig.q.DeleteUserSession(ctx, store.DeleteUserSessionParams{ID: signedOutSession.ID, UserID: signedOut.ID}); err != nil {
		t.Fatalf("sign out: %v", err)
	}
	rig.hub.Recheck(ctx)

	for name, p := range map[string]*wsPeer{"revoked": revokedPeer, "downgraded": editor, "signed out": signedOutPeer} {
		waitUntil(t, "the "+name+" peer's socket to close", p.isClosed)
	}
	if owner.isClosed() {
		t.Fatal("the owner's socket was closed too")
	}
	if !owner.edit(t, "# Draft\n\nStill here.\n") {
		t.Fatal("the owner's edit was refused after the recheck")
	}

	if _, code := rig.session(t, revoked, f.doc); code != http.StatusForbidden {
		t.Fatalf("revoked GET collab: %d, want 403", code)
	}
	if _, code := rig.dial(t, &revoked, room, rig.url); code != http.StatusUnauthorized {
		t.Fatalf("revoked upgrade: %d, want 401", code)
	}
	if _, code := rig.dial(t, &signedOut, room, rig.url); code != http.StatusUnauthorized {
		t.Fatalf("signed-out upgrade: %d, want 401", code)
	}
	dto := rig.mustSession(t, f.editor, f.doc)
	if dto.Room != room || dto.CanWrite {
		t.Fatalf("downgraded GET collab: %+v, want room %s read-only", dto, room)
	}
	back := rig.mustDial(t, f.editor, room)
	if back.edit(t, "# Draft\n\nThe downgraded editor again.\n") {
		t.Fatal("a downgraded editor rejoined writable")
	}
}

// A room writes a version only when the markdown changed, authored by the
// user whose edit it holds — not by the document's owner.
func TestCollabWritesBackChangedMarkdownAsTheWriter(t *testing.T) {
	rig := newCollabRig(t, collab.Config{})
	ctx := context.Background()
	f := rig.fixture(t, "writeback", "")

	res := rig.do(t, f.owner, http.MethodPost, "/api/v1/spaces/"+uuidString(f.space.ID)+"/documents",
		map[string]string{"kind": "markdown", "name": "Notes"})
	if res.code != http.StatusCreated {
		t.Fatalf("create markdown document: %d %s", res.code, res.body)
	}
	var created nodeDTO
	if err := json.Unmarshal(res.body, &created); err != nil {
		t.Fatal(err)
	}
	if created.Name != "Notes.md" || !fs.IsMarkdown(created.Mime) {
		t.Fatalf("created %q (%s), want Notes.md as text/markdown", created.Name, created.Mime)
	}
	var id pgtype.UUID
	if err := id.Scan(created.ID); err != nil {
		t.Fatal(err)
	}
	doc, err := rig.q.GetNode(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	grantDirect(t, rig.srv, doc, f.editor, f.owner, share.GrantCaps{Level: share.LevelEdit})

	editor := rig.mustDial(t, f.editor, rig.mustSession(t, f.editor, doc).Room)

	// A change that serializes to the same markdown: the blocks are all
	// replaced by equal ones.
	before := rig.flushes(t)
	if !editor.edit(t, "") {
		t.Fatal("the editor's edit was refused")
	}
	waitUntil(t, "the room to be written back", func() bool { return rig.flushes(t) > before })
	if vs := rig.versions(t, doc); len(vs) != 1 {
		t.Fatalf("%d versions after an edit that changed no markdown, want 1", len(vs))
	}

	if !editor.edit(t, "# Notes\n\nWritten by the editor.\n") {
		t.Fatal("the editor's edit was refused")
	}
	waitUntil(t, "a new version", func() bool { return len(rig.versions(t, doc)) == 2 })
	v := rig.versions(t, doc)[0]
	if v.CreatedBy != f.editor.ID {
		t.Fatalf("version authored by %s, want the editor %s (the owner is %s)",
			uuidString(v.CreatedBy), uuidString(f.editor.ID), uuidString(f.owner.ID))
	}
	if got, want := rig.content(t, v), editor.markdown(); got != want || want == "" {
		t.Fatalf("version holds %q, want the room's %q", got, want)
	}
	if !fs.IsMarkdown(v.Mime) {
		t.Fatalf("version MIME %q, want text/markdown", v.Mime)
	}
}

// Edits still in memory are written when the server stops — as their
// author's, even when somebody who changed nothing joined after them.
func TestCollabShutdownWritesPendingEditsAsTheirAuthors(t *testing.T) {
	rig := newCollabRig(t, collab.Config{CoalesceWindow: time.Hour, CoalesceMaxWait: time.Hour})
	f := rig.fixture(t, "shutdown", canonical(t, "# Log\n"))
	room := rig.mustSession(t, f.editor, f.doc).Room
	editor := rig.mustDial(t, f.editor, room)
	// Replacing every block deletes the old ones: the room now has a
	// delete set, which every later handshake carries.
	if !editor.edit(t, "# Log\n\nLast words before the restart.\n") {
		t.Fatal("the editor's edit was refused")
	}
	if n := len(rig.versions(t, f.doc)); n != 1 {
		t.Fatalf("%d versions before the flush is due, want 1", n)
	}
	want := editor.markdown()
	if late := rig.mustDial(t, f.owner, room); late.markdown() != want {
		t.Fatalf("the late peer sees %q, want %q", late.markdown(), want)
	}
	rig.shutdown(t)
	vs := rig.versions(t, f.doc)
	if len(vs) != 2 || vs[0].CreatedBy != f.editor.ID || rig.content(t, vs[0]) != want {
		t.Fatalf("after shutdown: %d versions, newest by %s; want 2, the editor's %q", len(vs), uuidString(vs[0].CreatedBy), want)
	}
}

// A resident room holds the file's lock, so a REST replacement is refused as
// it is under OnlyOffice. Once the room unloads, the lock is gone, its epoch
// is refused, and the next session gets a new one.
func TestCollabRoomLocksTheFileWhileResident(t *testing.T) {
	rig := newCollabRig(t, collab.Config{})
	ctx := context.Background()
	f := rig.fixture(t, "lock", "# Locked\n")
	room := rig.mustSession(t, f.owner, f.doc).Room
	replace := func() int {
		return createUpload(t, rig.srv, f.owner, 5,
			tusMeta("filename", "lock.md", "node_id", uuidString(f.doc.ID))).Code
	}

	owner := rig.mustDial(t, f.owner, room)
	if lock, err := rig.srv.wopi.CurrentLock(ctx, f.doc.ID); err != nil || !collab.IsLockToken(lock) {
		t.Fatalf("lock while resident: %q, %v; want a room's", lock, err)
	}
	if code := replace(); code != http.StatusLocked {
		t.Fatalf("replacement upload while the room is resident: %d, want 423", code)
	}

	owner.close()
	waitUntil(t, "the room to unload", func() bool { return rig.metric(t, "pphub_collab_rooms") == 0 })
	if lock, err := rig.srv.wopi.CurrentLock(ctx, f.doc.ID); err != nil || lock != "" {
		t.Fatalf("lock after unload: %q, %v; want none", lock, err)
	}
	if _, code := rig.dial(t, &f.owner, room, rig.url); code != http.StatusUnauthorized {
		t.Fatalf("upgrade to the unloaded epoch: %d, want 401", code)
	}
	if next := rig.mustSession(t, f.owner, f.doc).Room; next == room {
		t.Fatalf("GET collab after unload handed out the old room %s", room)
	}
	if code := replace(); code != http.StatusCreated {
		t.Fatalf("replacement upload after unload: %d, want 201", code)
	}
}

// Restoring a version under an open room is allowed despite the room's lock:
// the room first writes what it has not saved, then every peer sees the
// restored document live, in the same epoch.
func TestCollabRestoreReseedsTheOpenRoom(t *testing.T) {
	rig := newCollabRig(t, collab.Config{CoalesceWindow: time.Hour, CoalesceMaxWait: time.Hour})
	ctx := context.Background()
	a := canonical(t, "# Proposal\n\nThe first version.\n")
	b := canonical(t, "# Proposal\n\nThe second version.\n")
	f := rig.fixture(t, "restore", a)
	first := rig.versions(t, f.doc)[0]
	rig.putVersion(t, f.doc, f.owner, b)

	room := rig.mustSession(t, f.owner, f.doc).Room
	editor := rig.mustDial(t, f.editor, room)
	if got := editor.markdown(); got != b {
		t.Fatalf("room seeded with %q, want the current version %q", got, b)
	}
	if !editor.edit(t, "# Proposal\n\nAn unsaved third version.\n") {
		t.Fatal("the editor's edit was refused")
	}
	unsaved := editor.markdown()

	res := rig.do(t, f.owner, http.MethodPost,
		"/api/v1/nodes/"+uuidString(f.doc.ID)+"/versions/"+uuidString(first.ID)+"/restore", nil)
	if res.code != http.StatusOK {
		t.Fatalf("restore under an open room: %d %s", res.code, res.body)
	}

	vs := rig.versions(t, f.doc)
	if len(vs) != 3 || vs[0].CreatedBy != f.editor.ID || rig.content(t, vs[0]) != unsaved {
		t.Fatalf("restore did not save the room's edits first: %d versions, newest by %s", len(vs), uuidString(vs[0].CreatedBy))
	}
	cur, err := rig.q.GetCurrentVersion(ctx, f.doc.ID)
	if err != nil || cur.ID != first.ID {
		t.Fatalf("current version %v (%v), want the restored first one", cur.VersionNo, err)
	}
	waitUntil(t, "the peer to see the restored document", func() bool { return editor.markdown() == a })
	if editor.isClosed() {
		t.Fatal("the reseed closed the peer's socket")
	}
	if got := rig.mustSession(t, f.owner, f.doc).Room; got != room {
		t.Fatalf("restore changed the room %s → %s", room, got)
	}
	if late := rig.mustDial(t, f.owner, room); late.markdown() != a {
		t.Fatalf("a peer joining after the restore sees %q, want %q", late.markdown(), a)
	}

	// The reseed is nobody's edit: writing the room back adds nothing.
	rig.shutdown(t)
	if n := len(rig.versions(t, f.doc)); n != 3 {
		t.Fatalf("%d versions after the reseeded room was written back, want 3", n)
	}
}

// Room locks belong to the process that holds the rooms: a restart releases
// the ones its predecessor left, and only those.
func TestCollabStartReleasesStaleRoomLocks(t *testing.T) {
	rig := newCollabRig(t, collab.Config{})
	ctx := context.Background()
	f := rig.fixture(t, "stalelock", "# Stale\n")
	office := rig.putMarkdown(t, f.space, f.owner, "office.docx", "not really a docx")

	if n, err := rig.q.AcquireLock(ctx, store.AcquireLockParams{NodeID: f.doc.ID, LockToken: "collab:" + uuidString(f.doc.ID) + ".dead"}); err != nil || n != 1 {
		t.Fatalf("plant room lock: %d, %v", n, err)
	}
	if err := rig.srv.wopi.Lock(ctx, office.ID, "office-token"); err != nil {
		t.Fatalf("plant office lock: %v", err)
	}

	next := collab.New(rig.srv.fs, rig.srv.share, rig.q, nil, collab.Config{Origin: rig.url})
	if err := next.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() { _ = next.Shutdown(context.Background()) })

	if lock, err := rig.srv.wopi.CurrentLock(ctx, f.doc.ID); err != nil || lock != "" {
		t.Fatalf("room lock after restart: %q, %v; want none", lock, err)
	}
	if lock, err := rig.srv.wopi.CurrentLock(ctx, office.ID); err != nil || lock != "office-token" {
		t.Fatalf("office lock after restart: %q, %v; want it kept", lock, err)
	}
	// The released document opens again: a planted lock under another token
	// would have refused the room its own.
	room := rig.mustSession(t, f.owner, f.doc).Room
	rig.mustDial(t, f.owner, room)
	if lock, err := rig.srv.wopi.CurrentLock(ctx, f.doc.ID); err != nil || !collab.IsLockToken(lock) {
		t.Fatalf("lock once the room loads: %q, %v; want the room's", lock, err)
	}
}

// The page's icon and cover are the room's meta map, seeded from the
// frontmatter: opening the room writes nothing, a change to meta alone is its
// author's edit, and write-back merges meta into the frontmatter, keeping
// every other key.
func TestCollabMetaIsFrontmatter(t *testing.T) {
	rig := newCollabRig(t, collab.Config{CoalesceWindow: time.Hour, CoalesceMaxWait: time.Hour})
	ctx := context.Background()
	f := rig.fixture(t, "meta", canonical(t, "---\ntitle: x\nicon: 🐱\n---\n# Cat\n\nPurrs.\n"))
	editor := rig.mustDial(t, f.editor, rig.mustSession(t, f.editor, f.doc).Room)
	waitUntil(t, "the seeded meta", func() bool {
		icon, cover := editor.meta()
		return icon == "🐱" && cover == nil
	})

	if err := rig.hub.Flush(ctx, f.doc.ID); err != nil {
		t.Fatalf("flush: %v", err)
	}
	if n := len(rig.versions(t, f.doc)); n != 1 {
		t.Fatalf("%d versions after opening the room, want 1", n)
	}

	cover := "attachment:" + strings.Repeat("ab", 32)
	if !editor.setMeta(t, "🐶", cover) {
		t.Fatal("the editor's meta change was refused")
	}
	if err := rig.hub.Flush(ctx, f.doc.ID); err != nil {
		t.Fatalf("flush: %v", err)
	}
	vs := rig.versions(t, f.doc)
	if len(vs) != 2 || vs[0].CreatedBy != f.editor.ID {
		t.Fatalf("after a meta change: %d versions, newest by %s; want 2, the editor's", len(vs), uuidString(vs[0].CreatedBy))
	}
	got := rig.content(t, vs[0])
	parsed, err := mdoc.Parse([]byte(got))
	if err != nil {
		t.Fatal(err)
	}
	if icon, c := mdoc.FrontmatterMeta(parsed.Frontmatter); icon != "🐶" || c != cover {
		t.Fatalf("stored meta %q, %q; want 🐶, %q in\n%s", icon, c, cover, got)
	}
	if !strings.Contains(string(parsed.Frontmatter), "title: x") || !strings.Contains(got, "Purrs.") {
		t.Fatalf("write-back lost the other frontmatter or the body:\n%s", got)
	}

	// The written frontmatter is the room's: removing the cover later
	// removes only it.
	if !editor.setMeta(t, "🐶", "") {
		t.Fatal("the editor's meta change was refused")
	}
	if err := rig.hub.Flush(ctx, f.doc.ID); err != nil {
		t.Fatalf("flush: %v", err)
	}
	vs = rig.versions(t, f.doc)
	parsed, err = mdoc.Parse([]byte(rig.content(t, vs[0])))
	if err != nil {
		t.Fatal(err)
	}
	if icon, c := mdoc.FrontmatterMeta(parsed.Frontmatter); len(vs) != 3 || icon != "🐶" || c != "" ||
		!strings.Contains(string(parsed.Frontmatter), "title: x") {
		t.Fatalf("after removing the cover: %d versions, frontmatter %q", len(vs), parsed.Frontmatter)
	}
}

// Write-back titles every page a document links to by its node's current
// name; a page that is not a live file in the space is Untitled.
func TestCollabWriteBackRendersLiveTitles(t *testing.T) {
	rig := newCollabRig(t, collab.Config{CoalesceWindow: time.Hour, CoalesceMaxWait: time.Hour})
	ctx := context.Background()
	f := rig.fixture(t, "titles", canonical(t, "# Index\n"))
	target := rig.putMarkdown(t, f.space, f.owner, "Target.md", "# Target\n")
	id, unknown := uuidString(target.ID), "00000000-0000-4000-8000-0000000000aa"
	editor := rig.mustDial(t, f.editor, rig.mustSession(t, f.editor, f.doc).Room)

	if err := rig.srv.fs.Rename(ctx, target.ID, "New Title.md"); err != nil {
		t.Fatalf("rename: %v", err)
	}
	md := "# Index\n\n<!-- slash:subPage {\"pageId\":\"" + id + "\"} -->\n[Target](/edit/" + id + ")\n\n" +
		"See <!-- slash:pageLink {\"pageId\":\"" + unknown + "\"} -->[Gone](/edit/" + unknown + ")<!-- /slash:pageLink -->.\n"
	if !editor.edit(t, md) {
		t.Fatal("the editor's edit was refused")
	}
	if err := rig.hub.Flush(ctx, f.doc.ID); err != nil {
		t.Fatalf("flush: %v", err)
	}
	vs := rig.versions(t, f.doc)
	if len(vs) != 2 {
		t.Fatalf("%d versions after the edit, want 2", len(vs))
	}
	got := rig.content(t, vs[0])
	if !strings.Contains(got, "[New Title](/edit/"+id+")") || !strings.Contains(got, "[Untitled](/edit/"+unknown+")") {
		t.Fatalf("write-back does not carry the live titles:\n%s", got)
	}

	// A rename alone is nobody's edit: another peer opening the document
	// and a flush leave the file as it is, though it would render the new
	// title; the title lands with the next real edit.
	if err := rig.srv.fs.Rename(ctx, target.ID, "Renamed Again.md"); err != nil {
		t.Fatalf("rename: %v", err)
	}
	rig.mustDial(t, f.owner, rig.mustSession(t, f.owner, f.doc).Room)
	if err := rig.hub.Flush(ctx, f.doc.ID); err != nil {
		t.Fatalf("flush: %v", err)
	}
	if n := len(rig.versions(t, f.doc)); n != 2 {
		t.Fatalf("%d versions after a rename and a peer joining, want 2", n)
	}
}

// A reseed resets meta to the replacement's frontmatter, as nobody's edit.
func TestCollabRestoreReseedsMeta(t *testing.T) {
	rig := newCollabRig(t, collab.Config{CoalesceWindow: time.Hour, CoalesceMaxWait: time.Hour})
	ctx := context.Background()
	cover := "attachment:" + strings.Repeat("cd", 32)
	f := rig.fixture(t, "metarestore", canonical(t, "---\nicon: 🐱\n---\n# Pet\n"))
	first := rig.versions(t, f.doc)[0]
	rig.putVersion(t, f.doc, f.owner, canonical(t, "---\nicon: 🐶\ncover: "+cover+"\n---\n# Pet\n"))

	editor := rig.mustDial(t, f.editor, rig.mustSession(t, f.editor, f.doc).Room)
	waitUntil(t, "the seeded meta", func() bool {
		icon, c := editor.meta()
		return icon == "🐶" && c == cover
	})

	res := rig.do(t, f.owner, http.MethodPost,
		"/api/v1/nodes/"+uuidString(f.doc.ID)+"/versions/"+uuidString(first.ID)+"/restore", nil)
	if res.code != http.StatusOK {
		t.Fatalf("restore under an open room: %d %s", res.code, res.body)
	}
	waitUntil(t, "the peer to see the restored meta", func() bool {
		icon, c := editor.meta()
		return icon == "🐱" && c == nil
	})
	if err := rig.hub.Flush(ctx, f.doc.ID); err != nil {
		t.Fatalf("flush: %v", err)
	}
	rig.shutdown(t)
	if n := len(rig.versions(t, f.doc)); n != 2 {
		t.Fatalf("%d versions after the reseeded room was written back, want 2", n)
	}
}

type fakeDropper struct {
	dropped []string
	err     error
}

func (d *fakeDropper) Drop(ctx context.Context, key string) error {
	if d.err != nil {
		return d.err
	}
	d.dropped = append(d.dropped, key)
	return nil
}

func TestRestoreOfficeDocumentDropsSessionAndClearsLock(t *testing.T) {
	rig := newCollabRig(t, collab.Config{})
	ctx := context.Background()
	f := rig.fixture(t, "restore-office", "# Draft\n")
	office := rig.putMarkdown(t, f.space, f.owner, "report.docx", "v1 content")
	v1 := rig.versions(t, office)[0]

	rig.putVersion(t, office, f.owner, "v2 content")

	officeLock := "wopi_restore_test_123"
	if err := rig.srv.wopi.Lock(ctx, office.ID, officeLock); err != nil {
		t.Fatalf("lock office: %v", err)
	}

	dropper := &fakeDropper{}
	rig.srv.officeDropper = dropper

	// 1. Invalid version ID must return 404 and not drop the session or clear the lock.
	bogusVersion := "00000000-0000-0000-0000-000000000001"
	badRes := rig.do(t, f.owner, http.MethodPost,
		"/api/v1/nodes/"+uuidString(office.ID)+"/versions/"+bogusVersion+"/restore", nil)
	if badRes.code != http.StatusNotFound {
		t.Errorf("restore with bogus version returned %d, want 404", badRes.code)
	}
	if len(dropper.dropped) != 0 {
		t.Errorf("office dropper called on invalid version: %v", dropper.dropped)
	}
	if lock, _ := rig.srv.wopi.CurrentLock(ctx, office.ID); lock != officeLock {
		t.Errorf("lock cleared on invalid version: %q, want %q", lock, officeLock)
	}

	// 2. Failing dropper must return 409 and not clear the lock.
	dropper.err = errors.New("command service timeout")
	failRes := rig.do(t, f.owner, http.MethodPost,
		"/api/v1/nodes/"+uuidString(office.ID)+"/versions/"+uuidString(v1.ID)+"/restore", nil)
	if failRes.code != http.StatusConflict {
		t.Errorf("restore with failing dropper returned %d, want 409", failRes.code)
	}
	if lock, _ := rig.srv.wopi.CurrentLock(ctx, office.ID); lock != officeLock {
		t.Errorf("lock cleared when dropper failed: %q, want %q", lock, officeLock)
	}
	dropper.err = nil

	// 3. Successful drop and restore.
	res := rig.do(t, f.owner, http.MethodPost,
		"/api/v1/nodes/"+uuidString(office.ID)+"/versions/"+uuidString(v1.ID)+"/restore", nil)
	if res.code != http.StatusOK {
		t.Fatalf("restore under office lock: %d %s", res.code, res.body)
	}

	if len(dropper.dropped) != 1 || dropper.dropped[0] != officeLock {
		t.Errorf("office dropper saw %v, want [%s]", dropper.dropped, officeLock)
	}

	lock, err := rig.srv.wopi.CurrentLock(ctx, office.ID)
	if err != nil || lock != "" {
		t.Errorf("lock after restore: %q (err %v), want empty", lock, err)
	}

	cur, err := rig.q.GetCurrentVersion(ctx, office.ID)
	if err != nil || cur.ID != v1.ID {
		t.Errorf("current version after restore = %v (%v), want %v", cur.ID, err, v1.ID)
	}
}
