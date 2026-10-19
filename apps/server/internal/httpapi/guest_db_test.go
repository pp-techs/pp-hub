package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nhatbui/pp-hub/apps/server/internal/collab"
	"github.com/nhatbui/pp-hub/apps/server/internal/comments"
	"github.com/nhatbui/pp-hub/apps/server/internal/guest"
	"github.com/nhatbui/pp-hub/apps/server/internal/notifications"
	"github.com/nhatbui/pp-hub/apps/server/internal/personal"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
	"github.com/nhatbui/pp-hub/apps/server/internal/sysconfig"
	"github.com/nhatbui/pp-hub/apps/server/internal/users"
)

// Guest access (docs/architecture/guest-access.md) through the production
// router and real session cookies, against a real Postgres:
//
//	PP_TEST_DATABASE_URL=postgres://pphub:pphub@localhost:5433/pphub go test ./internal/httpapi/ -run Guest

// guestSwitches stands in for the two instance settings guests need.
type guestSwitches struct {
	mu       sync.Mutex
	guest    bool
	external bool
}

func (g *guestSwitches) GuestAccess() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.guest
}

func (g *guestSwitches) ExternalSharingEnabled() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.external
}

func (g *guestSwitches) set(guest, external bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.guest, g.external = guest, external
}

// codeMailer records the last code sent to each address.
type codeMailer struct {
	mu    sync.Mutex
	codes map[string]string
	sent  map[string]int
}

func (m *codeMailer) GuestCode(_ context.Context, to, code string, _ time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.codes == nil {
		m.codes, m.sent = map[string]string{}, map[string]int{}
	}
	m.codes[to] = code
	m.sent[to]++
	return nil
}

func (m *codeMailer) last(to string) (string, int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.codes[to], m.sent[to]
}

type guestFixture struct {
	pagesFixture
	sw     *guestSwitches
	mailer *codeMailer
	doc    store.Node
}

func newGuestFixture(t *testing.T, label string) guestFixture {
	t.Helper()
	sw := &guestSwitches{guest: true, external: true}
	mailer := &codeMailer{}
	rig := newCollabRigWith(t, collab.Config{}, func(srv *Server) {
		srv.guest = guest.New(srv.store, sw, mailer)
		srv.share.SetExternalSharing(sw.ExternalSharingEnabled)
		srv.personal = personal.New(srv.store, srv.spaces, srv.share, sysconfig.NewService(srv.store, nil), srv.audit)
		notes := notifications.NewService(srv.store, srv.blobs, srv.share)
		srv.notifications = notes
		srv.comments = comments.NewService(srv.store, notes)
		srv.users = users.NewService(srv.store, srv.blobs)
	})
	ctx := context.Background()
	f := guestFixture{
		pagesFixture: pagesFixture{rig: rig, owner: feedUser(t, rig.q, label+"-owner")},
		sw:           sw,
		mailer:       mailer,
	}
	sp, err := rig.srv.spaces.Create(ctx, fmt.Sprintf("guest-%s-%d", label, os.Getpid()), f.owner.ID)
	if err != nil {
		t.Fatalf("create space: %v", err)
	}
	t.Cleanup(func() { _ = rig.q.DeleteSpace(ctx, sp.ID) })
	f.space = sp
	setSpaceExternal(t, rig.q, sp, true)
	f.member = feedUser(t, rig.q, label+"-member")
	if err := rig.srv.spaces.AddMember(ctx, sp.ID, f.member.ID, false); err != nil {
		t.Fatalf("add member: %v", err)
	}
	rig.login(t, f.owner)
	rig.login(t, f.member)
	f.doc = f.page(t, "Shared page", nil)
	return f
}

func (f guestFixture) address(label string) string {
	return fmt.Sprintf("%s-%d@guest.test", label, os.Getpid())
}

// shareTo is POST /nodes/{id}/shares by email as the owner.
func (f guestFixture) shareTo(t *testing.T, node store.Node, email, level string) rigResponse {
	t.Helper()
	return f.rig.do(t, f.owner, http.MethodPost, "/api/v1/nodes/"+uuidString(node.ID)+"/shares", map[string]any{
		"kind": "direct", "email": email, "level": level, "can_download": true,
	})
}

func (f guestFixture) askCode(t *testing.T, email string) rigResponse {
	t.Helper()
	return f.rig.do(t, store.User{}, http.MethodPost, "/auth/guest/code", map[string]string{"email": email})
}

func (f guestFixture) verify(t *testing.T, email, code string) rigResponse {
	t.Helper()
	return f.rig.do(t, store.User{}, http.MethodPost, "/auth/guest/verify", map[string]string{"email": email, "code": code})
}

// signIn runs the whole proof for an address that already has an invitation
// and returns the guest, whose cookie the rig now holds.
func (f guestFixture) signIn(t *testing.T, email string) store.User {
	t.Helper()
	if res := f.askCode(t, email); res.code != http.StatusNoContent {
		t.Fatalf("ask code: %d %s", res.code, res.body)
	}
	code, _ := f.mailer.last(email)
	if code == "" {
		t.Fatalf("no code was mailed to %s", email)
	}
	res := f.verify(t, email, code)
	if res.code != http.StatusNoContent {
		t.Fatalf("verify: %d %s", res.code, res.body)
	}
	g, err := f.rig.q.GetGuestByEmailNorm(context.Background(), email)
	if err != nil {
		t.Fatalf("guest row: %v", err)
	}
	var token string
	for _, c := range (&http.Response{Header: res.header}).Cookies() {
		if c.Name == "pp_session" {
			token = c.Value
		}
	}
	if token == "" {
		t.Fatal("verify set no session cookie")
	}
	f.rig.mu.Lock()
	f.rig.cookies[g.ID] = token
	f.rig.mu.Unlock()
	return g
}

func TestGuestOpensWhatWasSharedAndNothingElse(t *testing.T) {
	f := newGuestFixture(t, "e2e")
	email := f.address("e2e")
	if res := f.shareTo(t, f.doc, email, "view"); res.code != http.StatusAccepted {
		t.Fatalf("invite: %d %s", res.code, res.body)
	}

	// A wrong code opens nothing and says nothing about why.
	if res := f.askCode(t, email); res.code != http.StatusNoContent {
		t.Fatalf("ask code: %d", res.code)
	}
	if res := f.verify(t, email, "000000"); res.code != http.StatusUnprocessableEntity {
		t.Fatalf("wrong code: want 422, got %d", res.code)
	}
	g := f.signIn(t, email)
	if g.Kind != "guest" || !g.EmailVerified || g.OidcIss != "" {
		t.Fatalf("guest row: %+v", g)
	}

	node := "/api/v1/nodes/" + uuidString(f.doc.ID)
	for _, path := range []string{node + "/", node + "/comments", "/api/v1/users/me", "/api/v1/shared?scope=external", "/api/v1/notifications/unread-count"} {
		if res := f.rig.do(t, g, http.MethodGet, path, nil); res.code != http.StatusOK {
			t.Errorf("GET %s as guest: want 200, got %d %s", path, res.code, res.body)
		}
	}
	me := decodeJSON[userProfileDTO](t, f.rig.do(t, g, http.MethodGet, "/api/v1/users/me", nil))
	if me.Kind != "guest" || me.PersonalSpaceID != "" {
		t.Errorf("profile: kind %q, personal space %q", me.Kind, me.PersonalSpaceID)
	}

	// Taking part in the discussion is the point.
	res := f.rig.do(t, g, http.MethodPost, node+"/comment-threads", map[string]any{"body": "looks right to me"})
	if res.code != http.StatusCreated {
		t.Fatalf("comment as guest: %d %s", res.code, res.body)
	}

	// A guest belongs to no space: the list is empty, not refused.
	if res := f.rig.do(t, g, http.MethodGet, "/api/v1/spaces/", nil); res.code != http.StatusOK || strings.TrimSpace(string(res.body)) != "[]" {
		t.Errorf("spaces as guest: want 200 [], got %d %s", res.code, res.body)
	}

	// Everything else is closed, whatever the node says.
	spaceID := uuidString(f.space.ID)
	for _, c := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/spaces/" + spaceID + "/children"},
		{http.MethodGet, "/api/v1/spaces/" + spaceID + "/members"},
		{http.MethodGet, "/api/v1/search?q=shared"},
		{http.MethodGet, "/api/v1/recent"},
		{http.MethodGet, "/api/v1/users"},
		{http.MethodGet, "/api/v1/admin/stats"},
		{http.MethodPost, node + "/shares"},
		{http.MethodDelete, node + "/"},
		{http.MethodPatch, node + "/rename"},
		{http.MethodPost, "/api/v1/uploads/"},
		{http.MethodPost, "/api/v1/users/me/agent-tokens"},
		{http.MethodGet, "/api/v1/users/me/sessions"},
	} {
		if res := f.rig.do(t, g, c.method, c.path, map[string]any{}); res.code != http.StatusForbidden {
			t.Errorf("%s %s as guest: want 403, got %d %s", c.method, c.path, res.code, res.body)
		}
	}

	// The grant is the whole of the reach: a sibling nobody shared is not
	// theirs, though the route is open.
	other := f.page(t, "Not shared", nil)
	if res := f.rig.do(t, g, http.MethodGet, "/api/v1/nodes/"+uuidString(other.ID)+"/", nil); res.code == http.StatusOK {
		t.Errorf("a page nobody shared opened for the guest: %s", res.body)
	}

	// The guest is not an account: not in the member list, not in the console.
	members := f.rig.do(t, f.owner, http.MethodGet, "/api/v1/spaces/"+spaceID+"/members", nil)
	if strings.Contains(string(members.body), email) {
		t.Errorf("guest listed among the members: %s", members.body)
	}
	rows, err := f.rig.q.ListUsers(context.Background(), store.ListUsersParams{TenantID: "default", Limit: 100000})
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range rows {
		if u.ID == g.ID {
			t.Error("the admin user list includes a guest")
		}
	}
}

func TestGuestCodeIsSentOnlyForAReason(t *testing.T) {
	f := newGuestFixture(t, "reason")

	ask := func(email string) {
		t.Helper()
		// The same answer every time, or the form is an oracle.
		if res := f.askCode(t, email); res.code != http.StatusNoContent {
			t.Fatalf("ask %s: want 204, got %d %s", email, res.code, res.body)
		}
	}
	mails := func(email string) int { _, n := f.mailer.last(email); return n }

	nobody := f.address("nobody")
	ask(nobody)
	if mails(nobody) != 0 {
		t.Error("a code went to an address nothing was shared with")
	}

	// A real account's address is for signing in, not for a guest.
	ask(f.member.Email)
	if mails(f.member.Email) != 0 {
		t.Error("a code went to a real account's address")
	}

	// An edit invitation is not a guest's to open.
	editor := f.address("editor")
	if res := f.shareTo(t, f.doc, editor, "edit"); res.code != http.StatusAccepted {
		t.Fatalf("edit invite: %d %s", res.code, res.body)
	}
	ask(editor)
	if mails(editor) != 0 {
		t.Error("a code went to an address holding only an edit invitation")
	}

	viewer := f.address("viewer")
	if res := f.shareTo(t, f.doc, viewer, "view"); res.code != http.StatusAccepted {
		t.Fatalf("view invite: %d %s", res.code, res.body)
	}
	f.sw.set(false, true)
	ask(viewer)
	f.sw.set(true, false)
	ask(viewer)
	if mails(viewer) != 0 {
		t.Error("a code was sent while guests, or external sharing, were off")
	}
	f.sw.set(true, true)
	for range guest.SendsPerHour + 2 {
		ask(viewer)
	}
	if got := mails(viewer); got != guest.SendsPerHour {
		t.Errorf("codes mailed to one address in an hour: want %d, got %d", guest.SendsPerHour, got)
	}
}

func TestGuestCodeIsSingleUseAndBurnsOnGuessing(t *testing.T) {
	f := newGuestFixture(t, "burn")
	email := f.address("burn")
	if res := f.shareTo(t, f.doc, email, "view"); res.code != http.StatusAccepted {
		t.Fatalf("invite: %d", res.code)
	}

	f.askCode(t, email)
	code, _ := f.mailer.last(email)
	wrong := "000000"
	if code == wrong {
		wrong = "000001"
	}
	for range guest.MaxAttempts {
		if res := f.verify(t, email, wrong); res.code != http.StatusUnprocessableEntity {
			t.Fatalf("wrong code: %d", res.code)
		}
	}
	// The right code is useless once the guesses ran out.
	if res := f.verify(t, email, code); res.code != http.StatusUnprocessableEntity {
		t.Errorf("a burnt code still worked: %d", res.code)
	}

	// A fresh code works once.
	f.askCode(t, email)
	code, _ = f.mailer.last(email)
	if res := f.verify(t, email, code); res.code != http.StatusNoContent {
		t.Fatalf("fresh code: %d", res.code)
	}
	if res := f.verify(t, email, code); res.code != http.StatusUnprocessableEntity {
		t.Errorf("a code worked twice: %d", res.code)
	}

	// An expired code is as good as none.
	f.askCode(t, email)
	code, _ = f.mailer.last(email)
	if _, err := f.rig.q.DB().Exec(context.Background(),
		"UPDATE guest_otps SET expires_at = now() - interval '1 minute' WHERE email_norm = $1", email); err != nil {
		t.Fatal(err)
	}
	if res := f.verify(t, email, code); res.code != http.StatusUnprocessableEntity {
		t.Errorf("an expired code worked: %d", res.code)
	}
}

func TestGuestHoldsNothingWiderThanView(t *testing.T) {
	f := newGuestFixture(t, "wide")
	email := f.address("wide")
	if res := f.shareTo(t, f.doc, email, "view"); res.code != http.StatusAccepted {
		t.Fatalf("invite: %d", res.code)
	}
	g := f.signIn(t, email)

	// Now a guest, the address cannot be made an editor.
	if res := f.shareTo(t, f.doc, email, "edit"); res.code != http.StatusUnprocessableEntity {
		t.Errorf("edit share to a guest: want 422, got %d %s", res.code, res.body)
	}
	// A view share to another page is an ordinary external grant.
	second := f.page(t, "Second", nil)
	if res := f.shareTo(t, second, email, "view"); res.code != http.StatusCreated && res.code != http.StatusOK {
		t.Fatalf("second share: %d %s", res.code, res.body)
	}
	if res := f.rig.do(t, g, http.MethodGet, "/api/v1/nodes/"+uuidString(second.ID)+"/", nil); res.code != http.StatusOK {
		t.Errorf("guest cannot open the second page: %d", res.code)
	}
	list := f.rig.do(t, f.owner, http.MethodGet, "/api/v1/nodes/"+uuidString(f.doc.ID)+"/shares", nil)
	if !strings.Contains(string(list.body), `"grantee_guest":true`) {
		t.Errorf("the share list does not mark the guest: %s", list.body)
	}

	// An edit invitation waits for the address's own sign-in: guest
	// verification takes view invitations only.
	later := f.address("later")
	if res := f.shareTo(t, f.doc, later, "edit"); res.code != http.StatusAccepted {
		t.Fatalf("edit invite: %d", res.code)
	}
	if res := f.shareTo(t, second, later, "view"); res.code != http.StatusAccepted {
		t.Fatalf("view invite: %d", res.code)
	}
	lg := f.signIn(t, later)
	var left int
	if err := f.rig.q.DB().QueryRow(context.Background(),
		"SELECT count(*) FROM pending_shares WHERE email_norm = $1 AND level = 'edit'", later).Scan(&left); err != nil || left != 1 {
		t.Errorf("the edit invitation must stay pending: %d %v", left, err)
	}
	if res := f.rig.do(t, lg, http.MethodGet, "/api/v1/nodes/"+uuidString(f.doc.ID)+"/", nil); res.code == http.StatusOK {
		t.Errorf("a guest opened a page they were only invited to edit")
	}
}

func TestGuestSessionStopsWhenGuestAccessIsOff(t *testing.T) {
	f := newGuestFixture(t, "off")
	email := f.address("off")
	f.shareTo(t, f.doc, email, "view")
	g := f.signIn(t, email)
	path := "/api/v1/nodes/" + uuidString(f.doc.ID) + "/"
	if res := f.rig.do(t, g, http.MethodGet, path, nil); res.code != http.StatusOK {
		t.Fatalf("before: %d", res.code)
	}
	f.sw.set(false, true)
	if res := f.rig.do(t, g, http.MethodGet, path, nil); res.code != http.StatusForbidden {
		t.Errorf("guests off: want 403, got %d", res.code)
	}
	f.sw.set(true, false)
	if res := f.rig.do(t, g, http.MethodGet, path, nil); res.code != http.StatusForbidden {
		t.Errorf("external sharing off: want 403, got %d", res.code)
	}
	f.sw.set(true, true)
	if res := f.rig.do(t, g, http.MethodGet, path, nil); res.code != http.StatusOK {
		t.Errorf("switched back on: want 200, got %d", res.code)
	}
}

// A later sign-in through the identity provider with the same address keeps
// the row, so what the guest was shared and wrote stays theirs.
func TestGuestBecomesAMemberAtFirstSignIn(t *testing.T) {
	f := newGuestFixture(t, "promote")
	email := f.address("promote")
	f.shareTo(t, f.doc, email, "view")
	g := f.signIn(t, email)
	res := f.rig.do(t, g, http.MethodPost, "/api/v1/nodes/"+uuidString(f.doc.ID)+"/comment-threads", map[string]any{"body": "first"})
	if res.code != http.StatusCreated {
		t.Fatalf("comment: %d %s", res.code, res.body)
	}

	// The provider spells the address as it likes.
	found, err := f.rig.q.GetGuestByEmailNorm(context.Background(), strings.ToLower(strings.ToUpper(email)))
	if err != nil || found.ID != g.ID {
		t.Fatalf("lookup by normalized address: %v", err)
	}
	m, err := f.rig.q.AdoptUserIdentity(context.Background(), store.AdoptUserIdentityParams{
		ID: g.ID, OidcIss: "test", OidcSub: "promoted-" + uuidString(g.ID), Email: email, Name: "Promoted",
	})
	if err != nil {
		t.Fatalf("adopt: %v", err)
	}
	if m.ID != g.ID || m.Kind != "member" {
		t.Fatalf("adopted row: id kept %v, kind %q", m.ID == g.ID, m.Kind)
	}
	// Its session is now an ordinary one: the guest allowlist no longer applies.
	if res := f.rig.do(t, m, http.MethodGet, "/api/v1/spaces", nil); res.code == http.StatusForbidden {
		t.Errorf("a promoted account is still confined: %d", res.code)
	}
	var comments int
	if err := f.rig.q.DB().QueryRow(context.Background(),
		"SELECT count(*) FROM comments WHERE author_id = $1", g.ID).Scan(&comments); err != nil || comments != 1 {
		t.Errorf("comments kept: %d %v", comments, err)
	}
}

// A guest gets no personal space, whatever the instance gives members.
func TestGuestIsNeverGivenAPersonalSpace(t *testing.T) {
	f := newGuestFixture(t, "personal")
	email := f.address("personal")
	f.shareTo(t, f.doc, email, "view")
	g := f.signIn(t, email)
	policy := everyonePolicy{}
	svc := personal.New(f.rig.q, f.rig.srv.spaces, f.rig.srv.share, policy, f.rig.srv.audit)
	if err := svc.Provision(context.Background(), g); err != nil {
		t.Fatal(err)
	}
	if _, err := f.rig.q.GetPersonalSpace(context.Background(), g.ID); err == nil {
		t.Error("a guest was given a personal space")
	}
}

type everyonePolicy struct{}

func (everyonePolicy) PersonalSpaces() string           { return sysconfig.PersonalSpacesEveryone }
func (everyonePolicy) DefaultPersonalQuotaBytes() int64 { return 1 << 20 }

// The wiring half of the allowlist: with a live guest session, every other
// route the router mounts under /api/v1 answers 403 before its handler runs.
func TestEveryRouteRefusesGuestsUnlessListed(t *testing.T) {
	f := newGuestFixture(t, "walk")
	email := f.address("walk")
	f.shareTo(t, f.doc, email, "view")
	g := f.signIn(t, email)

	allowed := map[string]bool{}
	for _, spec := range guestRoutes {
		allowed[spec] = true
	}
	var refused, open int
	err := chi.Walk(f.rig.srv.Router().(chi.Routes), func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if !strings.HasPrefix(route, "/api/v1/") {
			return nil
		}
		rel := strings.TrimPrefix(route, "/api/v1")
		if allowed[method+" "+rel] {
			open++
			return nil
		}
		path := route
		for _, p := range []string{"nodeID", "spaceID", "userID", "shareID", "pendingID", "groupID", "versionID", "threadID", "commentID", "uploadID", "extID", "hash", "room"} {
			path = strings.ReplaceAll(path, "{"+p+"}", uuidString(pgtype.UUID{Bytes: [16]byte{1}, Valid: true}))
		}
		res := f.rig.do(t, g, method, path, map[string]any{})
		if res.code != http.StatusForbidden {
			t.Errorf("%s %s as guest: want 403, got %d", method, route, res.code)
		}
		refused++
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if refused < 50 || open != len(guestRoutes) {
		t.Errorf("walked %d refused and %d open routes; the list has %d", refused, open, len(guestRoutes))
	}
}

// The list names real routes (a rename must not leave a dead entry that
// reads as protection), and matches the way chi does.
func TestGuestRoutesAreRoutes(t *testing.T) {
	registered := map[string]bool{}
	err := chi.Walk(routerWithExtensions(t), func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		registered[method+" "+strings.TrimPrefix(route, "/api/v1")] = true
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, spec := range guestRoutes {
		if !registered[spec] {
			t.Errorf("guestRoutes names %q, which is not a route", spec)
		}
	}
}

func TestGuestMayCall(t *testing.T) {
	for _, c := range []struct {
		method, path string
		want         bool
	}{
		{"GET", "/api/v1/nodes/abc", true},
		{"GET", "/api/v1/nodes/abc/", true},
		{"GET", "/api/v1/nodes/abc/children", true},
		{"POST", "/api/v1/nodes/abc/comment-threads", true},
		{"GET", "/api/v1/nodes/abc/shares", false},
		{"POST", "/api/v1/nodes/abc/shares", false},
		{"DELETE", "/api/v1/nodes/abc", false},
		{"GET", "/api/v1/nodes/abc/children/extra", false},
		{"GET", "/api/v1/nodes//children", false},
		{"GET", "/api/v1/nodes/a/b/children", false},
		{"GET", "/api/v1/nodes", false},
		{"GET", "/api/v1/spaces", true},
		{"POST", "/api/v1/spaces", false},
		{"GET", "/api/v1/spaces/abc/children", false},
		{"GET", "/api/v1/users/me/sessions", false},
		{"GET", "/api/v1/users/me", true},
		{"PUT", "/api/v1/users/me", false},
	} {
		req, _ := http.NewRequest(c.method, "http://x"+c.path, nil)
		if got := guestMayCall(req); got != c.want {
			t.Errorf("%s %s: want %v, got %v", c.method, c.path, c.want, got)
		}
	}
}
