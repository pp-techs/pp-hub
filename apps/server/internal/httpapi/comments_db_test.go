package httpapi

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nhatbui/pp-hub/apps/server/internal/collab"
	"github.com/nhatbui/pp-hub/apps/server/internal/comments"
	"github.com/nhatbui/pp-hub/apps/server/internal/mdoc"
	"github.com/nhatbui/pp-hub/apps/server/internal/notifications"
	"github.com/nhatbui/pp-hub/apps/server/internal/share"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

// Comments and notifications (M29) resolve against real grants, sessions and
// cascades, so these run the production router against a real Postgres, or
// skip loudly like the rest of this package's database tests.

// socialRig is a collabRig with the comments and notifications services.
type socialRig struct {
	*collabRig
	notes *notifications.Service
}

func newSocialRig(t *testing.T) socialRig {
	t.Helper()
	rig := newCollabRig(t, collab.Config{})
	notes := notifications.NewService(rig.q, rig.srv.blobs, rig.srv.share)
	rig.srv.notifications = notes
	rig.srv.comments = comments.NewService(rig.q, notes)
	return socialRig{collabRig: rig, notes: notes}
}

func ids(us ...store.User) []string {
	out := make([]string, 0, len(us))
	for _, u := range us {
		out = append(out, uuidString(u.ID))
	}
	return out
}

func (rig socialRig) openThread(t *testing.T, u store.User, doc store.Node, body string, mentions ...store.User) (threadDTO, int) {
	t.Helper()
	res := rig.do(t, u, http.MethodPost, "/api/v1/nodes/"+uuidString(doc.ID)+"/comment-threads",
		map[string]any{"body": body, "mentions": ids(mentions...)})
	return decodeThread(t, res, http.StatusCreated)
}

func (rig socialRig) reply(t *testing.T, u store.User, threadID, body string, mentions ...store.User) (threadDTO, int) {
	t.Helper()
	res := rig.do(t, u, http.MethodPost, "/api/v1/comment-threads/"+threadID+"/comments",
		map[string]any{"body": body, "mentions": ids(mentions...)})
	return decodeThread(t, res, http.StatusCreated)
}

func (rig socialRig) setResolved(t *testing.T, u store.User, threadID string, resolved bool) (threadDTO, int) {
	t.Helper()
	verb := "reopen"
	if resolved {
		verb = "resolve"
	}
	return decodeThread(t, rig.do(t, u, http.MethodPost, "/api/v1/comment-threads/"+threadID+"/"+verb, nil), http.StatusOK)
}

func decodeThread(t *testing.T, res rigResponse, ok int) (threadDTO, int) {
	t.Helper()
	var th threadDTO
	if res.code == ok {
		if err := json.Unmarshal(res.body, &th); err != nil {
			t.Fatalf("decode thread: %v; body=%s", err, res.body)
		}
	}
	return th, res.code
}

func (rig socialRig) threads(t *testing.T, u store.User, doc store.Node) []threadDTO {
	t.Helper()
	res := rig.do(t, u, http.MethodGet, "/api/v1/nodes/"+uuidString(doc.ID)+"/comments", nil)
	if res.code != http.StatusOK {
		t.Fatalf("list comments as %s: %d; body=%s", u.Name, res.code, res.body)
	}
	var out struct {
		Threads []threadDTO `json:"threads"`
	}
	if err := json.Unmarshal(res.body, &out); err != nil {
		t.Fatalf("decode threads: %v", err)
	}
	return out.Threads
}

type inboxPage struct {
	Items      []notificationDTO `json:"items"`
	NextCursor string            `json:"next_cursor"`
}

func (rig socialRig) inbox(t *testing.T, u store.User, query string) inboxPage {
	t.Helper()
	res := rig.do(t, u, http.MethodGet, "/api/v1/notifications"+query, nil)
	if res.code != http.StatusOK {
		t.Fatalf("list notifications as %s: %d; body=%s", u.Name, res.code, res.body)
	}
	var p inboxPage
	if err := json.Unmarshal(res.body, &p); err != nil {
		t.Fatalf("decode notifications: %v", err)
	}
	return p
}

// kinds is u's inbox as "kind by actor" lines, oldest first.
func (rig socialRig) kinds(t *testing.T, u store.User) []string {
	t.Helper()
	var out []string
	for _, it := range rig.inbox(t, u, "").Items {
		out = append(out, it.Kind+" by "+it.Actor.Name)
	}
	slices.Reverse(out)
	return out
}

func (rig socialRig) unread(t *testing.T, u store.User) int64 {
	t.Helper()
	res := rig.do(t, u, http.MethodGet, "/api/v1/notifications/unread-count", nil)
	if res.code != http.StatusOK {
		t.Fatalf("unread count: %d", res.code)
	}
	var out struct {
		Count int64 `json:"count"`
	}
	if err := json.Unmarshal(res.body, &out); err != nil {
		t.Fatal(err)
	}
	return out.Count
}

// socialFixture adds a second reader (view + download) to a collabFixture, so
// "can comment" and "opened this thread" are different people.
func (rig socialRig) fixture(t *testing.T, label string) (collabFixture, store.User) {
	t.Helper()
	f := rig.collabRig.fixture(t, label, "# Spec\n\nSome text.\n")
	reader2 := feedUser(t, rig.q, label+"-reader2")
	if err := rig.srv.spaces.AddMember(context.Background(), f.space.ID, reader2.ID, false); err != nil {
		t.Fatalf("add member: %v", err)
	}
	grantDirect(t, rig.srv, f.doc, reader2, f.owner, share.GrantCaps{Level: share.LevelView, CanDownload: true})
	rig.login(t, reader2)
	return f, reader2
}

// Every comments route takes view on the document — whoever can read it can
// discuss it — and the per-comment rules sit on top: edit is the author's,
// delete the author's or an editor's, resolve and reopen the thread opener's
// or an editor's.
func TestCommentRoutesPermissionMatrix(t *testing.T) {
	rig := newSocialRig(t)
	f, reader2 := rig.fixture(t, "cmatrix")
	txt := rig.putMarkdown(t, f.space, f.owner, "notes.txt", "plain\n")
	docPath := "/api/v1/nodes/" + uuidString(f.doc.ID)

	for _, tc := range []struct {
		name string
		u    store.User
		want int
	}{
		{"owner", f.owner, http.StatusOK},
		{"editor", f.editor, http.StatusOK},
		{"reader", f.reader, http.StatusOK},
		{"view only", f.viewer, http.StatusOK},
		{"member without a grant", f.stranger, http.StatusForbidden},
	} {
		if got := rig.do(t, tc.u, http.MethodGet, docPath+"/comments", nil).code; got != tc.want {
			t.Errorf("list as %s: %d, want %d", tc.name, got, tc.want)
		}
		want := tc.want
		if want == http.StatusOK {
			want = http.StatusCreated
		}
		if _, got := rig.openThread(t, tc.u, f.doc, "hello from "+tc.name); got != want {
			t.Errorf("open thread as %s: %d, want %d", tc.name, got, want)
		}
	}
	if got := rig.do(t, f.owner, http.MethodGet, "/api/v1/nodes/"+uuidString(txt.ID)+"/comments", nil).code; got != http.StatusBadRequest {
		t.Errorf("comments on a non-markdown file: %d, want 400", got)
	}
	if got := rig.do(t, f.owner, http.MethodGet, "/api/v1/nodes/00000000-0000-0000-0000-000000000001/comments", nil).code; got != http.StatusNotFound {
		t.Errorf("comments on a missing node: %d, want 404", got)
	}
	for _, body := range []string{"", "   \n\t", strings.Repeat("x", comments.MaxBodyRunes+1)} {
		if _, got := rig.openThread(t, f.reader, f.doc, body); got != http.StatusBadRequest {
			t.Errorf("open thread with a %d-byte body: %d, want 400", len(body), got)
		}
	}
	bad := rig.do(t, f.reader, http.MethodPost, docPath+"/comment-threads", map[string]any{"body": "hi", "mentions": []string{"nobody"}})
	if bad.code != http.StatusBadRequest {
		t.Errorf("a malformed mention: %d, want 400", bad.code)
	}

	// The reader opens a thread; everybody who can open the document may
	// answer in it.
	th, code := rig.openThread(t, f.reader, f.doc, "  Is this right?  ")
	if code != http.StatusCreated || len(th.Comments) != 1 || th.Comments[0].Body != "Is this right?" ||
		th.CreatedBy.ID != uuidString(f.reader.ID) || th.Comments[0].Author.Name != f.reader.Name || th.Resolved {
		t.Fatalf("open thread: %d %+v", code, th)
	}
	for _, tc := range []struct {
		name string
		u    store.User
		want int
	}{
		{"reader2", reader2, http.StatusCreated},
		{"editor", f.editor, http.StatusCreated},
		{"view only", f.viewer, http.StatusCreated},
		{"member without a grant", f.stranger, http.StatusForbidden},
	} {
		if _, got := rig.reply(t, tc.u, th.ID, "reply from "+tc.name); got != tc.want {
			t.Errorf("reply as %s: %d, want %d", tc.name, got, tc.want)
		}
	}
	if _, got := rig.reply(t, f.reader, "00000000-0000-0000-0000-000000000001", "x"); got != http.StatusNotFound {
		t.Errorf("reply to a missing thread: %d, want 404", got)
	}
	if _, got := rig.reply(t, f.reader, "not-a-uuid", "x"); got != http.StatusBadRequest {
		t.Errorf("reply to a malformed thread id: %d, want 400", got)
	}

	// Edit: the author alone — not an editor, not the owner.
	first := th.Comments[0].ID
	for _, tc := range []struct {
		name string
		u    store.User
		want int
	}{
		{"owner", f.owner, http.StatusForbidden},
		{"editor", f.editor, http.StatusForbidden},
		{"reader2", reader2, http.StatusForbidden},
		{"view only", f.viewer, http.StatusForbidden},
		{"member without a grant", f.stranger, http.StatusForbidden},
		{"author", f.reader, http.StatusOK},
	} {
		res := rig.do(t, tc.u, http.MethodPatch, "/api/v1/comments/"+first, map[string]string{"body": "edited by " + tc.name})
		if res.code != tc.want {
			t.Errorf("edit as %s: %d, want %d", tc.name, res.code, tc.want)
		}
		if res.code == http.StatusOK {
			var c commentDTO
			if err := json.Unmarshal(res.body, &c); err != nil || c.Body != "edited by author" || c.EditedAt == "" {
				t.Errorf("edited comment: %+v (%v)", c, err)
			}
		}
	}
	if got := rig.do(t, f.reader, http.MethodPatch, "/api/v1/comments/"+first, map[string]string{"body": " "}).code; got != http.StatusBadRequest {
		t.Errorf("edit to an empty body: %d, want 400", got)
	}

	// Resolve and reopen: the thread's opener or an editor.
	for _, tc := range []struct {
		name     string
		u        store.User
		resolved bool
		want     int
	}{
		{"reader2 resolves", reader2, true, http.StatusForbidden},
		{"view only resolves", f.viewer, true, http.StatusForbidden},
		{"stranger resolves", f.stranger, true, http.StatusForbidden},
		{"opener resolves", f.reader, true, http.StatusOK},
		{"reader2 reopens", reader2, false, http.StatusForbidden},
		{"editor reopens", f.editor, false, http.StatusOK},
		{"editor resolves", f.editor, true, http.StatusOK},
		{"owner reopens", f.owner, false, http.StatusOK},
	} {
		if _, got := rig.setResolved(t, tc.u, th.ID, tc.resolved); got != tc.want {
			t.Errorf("%s: %d, want %d", tc.name, got, tc.want)
		}
	}

	// Delete: the author, or an editor of the document.
	cur := rig.threads(t, f.owner, f.doc)
	idx := slices.IndexFunc(cur, func(x threadDTO) bool { return x.ID == th.ID })
	if idx < 0 || len(cur[idx].Comments) != 4 {
		t.Fatalf("thread before deletes: %+v", cur)
	}
	byReader2, byEditor, byViewer := cur[idx].Comments[1].ID, cur[idx].Comments[2].ID, cur[idx].Comments[3].ID
	for _, tc := range []struct {
		name, id string
		u        store.User
		want     int
	}{
		{"reader deletes reader2's", byReader2, f.reader, http.StatusForbidden},
		{"view only deletes reader2's", byReader2, f.viewer, http.StatusForbidden},
		{"stranger deletes reader2's", byReader2, f.stranger, http.StatusForbidden},
		{"editor deletes reader2's", byReader2, f.editor, http.StatusNoContent},
		{"deleted again", byReader2, f.editor, http.StatusNotFound},
		{"author deletes own", byEditor, f.editor, http.StatusNoContent},
		{"editor deletes viewer's", byViewer, f.editor, http.StatusNoContent},
	} {
		if got := rig.do(t, tc.u, http.MethodDelete, "/api/v1/comments/"+tc.id, nil).code; got != tc.want {
			t.Errorf("%s: %d, want %d", tc.name, got, tc.want)
		}
	}
	// The last comment takes its thread with it.
	if got := rig.do(t, f.reader, http.MethodDelete, "/api/v1/comments/"+first, nil).code; got != http.StatusNoContent {
		t.Fatalf("delete the last comment: %d", got)
	}
	if slices.ContainsFunc(rig.threads(t, f.owner, f.doc), func(x threadDTO) bool { return x.ID == th.ID }) {
		t.Fatal("a thread with no comment left is still listed")
	}
	if _, got := rig.reply(t, f.reader, th.ID, "anyone?"); got != http.StatusNotFound {
		t.Errorf("reply to a deleted thread: %d, want 404", got)
	}
}

// Resolving is idempotent and keeps who resolved it; a reply reopens.
func TestCommentThreadResolveRules(t *testing.T) {
	rig := newSocialRig(t)
	f, _ := rig.fixture(t, "cresolve")
	th, _ := rig.openThread(t, f.reader, f.doc, "Typo in the title")

	resolved, code := rig.setResolved(t, f.reader, th.ID, true)
	if code != http.StatusOK || !resolved.Resolved || resolved.ResolvedAt == "" ||
		resolved.ResolvedBy == nil || resolved.ResolvedBy.ID != uuidString(f.reader.ID) {
		t.Fatalf("resolve: %d %+v", code, resolved)
	}
	again, _ := rig.setResolved(t, f.editor, th.ID, true)
	if !again.Resolved || again.ResolvedBy == nil || again.ResolvedBy.ID != uuidString(f.reader.ID) || again.ResolvedAt != resolved.ResolvedAt {
		t.Fatalf("resolving again must keep the first resolution: %+v", again)
	}
	replied, code := rig.reply(t, f.editor, th.ID, "Fixed now")
	if code != http.StatusCreated || replied.Resolved || replied.ResolvedBy != nil || len(replied.Comments) != 2 {
		t.Fatalf("a reply must reopen the thread: %d %+v", code, replied)
	}
	reopened, _ := rig.setResolved(t, f.reader, th.ID, false)
	if reopened.Resolved {
		t.Fatalf("reopen an open thread: %+v", reopened)
	}
}

// Purging the document takes its threads, comments and notifications.
func TestCommentsGoWithTheirDocument(t *testing.T) {
	rig := newSocialRig(t)
	ctx := context.Background()
	f, _ := rig.fixture(t, "ccascade")
	th, _ := rig.openThread(t, f.reader, f.doc, "note")
	if n := rig.unread(t, f.owner); n != 1 {
		t.Fatalf("owner unread before purge: %d", n)
	}
	if err := rig.srv.fs.Delete(ctx, f.doc.ID, f.owner.ID); err != nil {
		t.Fatalf("trash: %v", err)
	}
	if n := rig.unread(t, f.owner); n != 0 {
		t.Fatalf("a trashed document's notifications must not count: %d", n)
	}
	if _, err := rig.srv.fs.Purge(ctx, f.doc.ID); err != nil {
		t.Fatalf("purge: %v", err)
	}
	var id pgtype.UUID
	if err := id.Scan(th.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := rig.q.GetCommentThread(ctx, id); err == nil {
		t.Fatal("thread survived its document's purge")
	}
}

// Who is told: the document's owner about a new thread; participants and the
// owner about a reply; the people a comment names as a mention — only those
// who can open the document, each once, and never the actor.
func TestCommentNotificationRecipients(t *testing.T) {
	rig := newSocialRig(t)
	f, reader2 := rig.fixture(t, "crecip")

	// reader opens a thread naming the editor (can open), the viewer (view
	// only: can read, so can be told), the stranger (no access) and
	// themselves.
	th, code := rig.openThread(t, f.reader, f.doc, "Please  check\nthis section", f.editor, f.viewer, f.stranger, f.reader)
	if code != http.StatusCreated {
		t.Fatalf("open thread: %d", code)
	}
	// editor replies; the owner replies naming reader.
	if _, code := rig.reply(t, f.editor, th.ID, "Checked."); code != http.StatusCreated {
		t.Fatalf("editor reply: %d", code)
	}
	if _, code := rig.reply(t, f.owner, th.ID, "Thanks", f.reader); code != http.StatusCreated {
		t.Fatalf("owner reply: %d", code)
	}

	want := map[string][]string{
		"owner":    {"comment by crecip-reader", "reply by crecip-editor"},
		"editor":   {"mention by crecip-reader", "reply by crecip-owner"},
		"reader":   {"reply by crecip-editor", "mention by crecip-owner"},
		"reader2":  nil,
		"viewer":   {"mention by crecip-reader"},
		"stranger": nil,
	}
	for name, u := range map[string]store.User{"owner": f.owner, "editor": f.editor, "reader": f.reader,
		"reader2": reader2, "viewer": f.viewer, "stranger": f.stranger} {
		if got := rig.kinds(t, u); !slices.Equal(got, want[name]) {
			t.Errorf("%s was told %q, want %q", name, got, want[name])
		}
	}

	items := rig.inbox(t, f.owner, "").Items
	first := items[len(items)-1]
	if first.Node.ID != uuidString(f.doc.ID) || first.Node.Name != f.doc.Name || first.Node.SpaceID != uuidString(f.space.ID) ||
		first.ThreadID != th.ID || first.CommentID != th.Comments[0].ID || first.Excerpt != "Please check this section" ||
		first.Actor.ID != uuidString(f.reader.ID) || first.ReadAt != "" || first.CreatedAt == "" {
		t.Errorf("owner's comment notification: %+v", first)
	}

	// A revoked grant hides what the person was told about the document.
	shares, err := rig.q.ListSharesForNode(context.Background(), f.doc.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, sh := range shares {
		if sh.GranteeUserID == f.editor.ID {
			if _, err := rig.q.DeleteShare(context.Background(), store.DeleteShareParams{ID: sh.ID, NodeID: f.doc.ID}); err != nil {
				t.Fatal(err)
			}
		}
	}
	if got := rig.kinds(t, f.editor); len(got) != 0 {
		t.Errorf("editor without access still sees %q", got)
	}
	if n := rig.unread(t, f.editor); n != 0 {
		t.Errorf("editor without access still counts %d unread", n)
	}
}

// The unread count, marking read by id or all at once, and paging.
func TestNotificationsUnreadAndMarkRead(t *testing.T) {
	rig := newSocialRig(t)
	f, _ := rig.fixture(t, "cread")
	for i := range 3 {
		if _, code := rig.openThread(t, f.reader, f.doc, "thread "+string(rune('A'+i))); code != http.StatusCreated {
			t.Fatalf("open thread: %d", code)
		}
	}
	if n := rig.unread(t, f.owner); n != 3 {
		t.Fatalf("owner unread: %d, want 3", n)
	}

	// Paging, newest first.
	p1 := rig.inbox(t, f.owner, "?limit=2")
	if len(p1.Items) != 2 || p1.NextCursor == "" || p1.Items[0].Excerpt != "thread C" || p1.Items[1].Excerpt != "thread B" {
		t.Fatalf("page 1: %+v", p1)
	}
	p2 := rig.inbox(t, f.owner, "?limit=2&cursor="+p1.NextCursor)
	if len(p2.Items) != 1 || p2.NextCursor != "" || p2.Items[0].Excerpt != "thread A" {
		t.Fatalf("page 2: %+v", p2)
	}
	for _, q := range []string{"?cursor=garbage", "?limit=0", "?limit=x"} {
		if got := rig.do(t, f.owner, http.MethodGet, "/api/v1/notifications"+q, nil).code; got != http.StatusBadRequest {
			t.Errorf("GET notifications%s: %d, want 400", q, got)
		}
	}

	mark := func(u store.User, body any) (int, int64) {
		res := rig.do(t, u, http.MethodPost, "/api/v1/notifications/read", body)
		var out struct {
			Count int64 `json:"count"`
		}
		_ = json.Unmarshal(res.body, &out)
		return res.code, out.Count
	}
	if code, _ := mark(f.owner, map[string]any{}); code != http.StatusBadRequest {
		t.Errorf("mark nothing: %d, want 400", code)
	}
	// Somebody else's ids change nothing.
	if code, _ := mark(f.editor, map[string]any{"ids": []string{p1.Items[0].ID}}); code != http.StatusOK {
		t.Errorf("mark another's: %d", code)
	}
	if n := rig.unread(t, f.owner); n != 3 {
		t.Fatalf("another user marked the owner's notification read: %d", n)
	}
	if code, left := mark(f.owner, map[string]any{"ids": []string{p1.Items[0].ID}}); code != http.StatusOK || left != 2 {
		t.Fatalf("mark one: %d, %d left, want 2", code, left)
	}
	if got := rig.inbox(t, f.owner, "").Items[0]; got.ReadAt == "" {
		t.Errorf("a marked notification has no read_at: %+v", got)
	}
	if code, left := mark(f.owner, map[string]any{"all": true}); code != http.StatusOK || left != 0 {
		t.Fatalf("mark all: %d, %d left", code, left)
	}

	// The retention sweep takes read rows, not unread ones.
	if _, code := rig.openThread(t, f.reader, f.doc, "fresh"); code != http.StatusCreated {
		t.Fatal(code)
	}
	if _, err := rig.notes.Purge(context.Background(), time.Now().Add(time.Minute)); err != nil {
		t.Fatalf("purge: %v", err)
	}
	if got := rig.inbox(t, f.owner, "").Items; len(got) != 1 || got[0].Excerpt != "fresh" {
		t.Fatalf("after purge: %+v", got)
	}
}

// sseStream is an open GET /notifications/stream, its events as raw blocks.
type sseStream struct {
	events chan string
	close  func()
}

func (rig socialRig) stream(t *testing.T, u store.User) *sseStream {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rig.url+"/api/v1/notifications/stream", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Cookie", rig.cookie(u))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("open stream: %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	s := &sseStream{events: make(chan string, 64)}
	go func() {
		defer close(s.events)
		br := bufio.NewReader(resp.Body)
		var block strings.Builder
		for {
			line, err := br.ReadString('\n')
			if err != nil {
				return
			}
			if line == "\n" {
				s.events <- block.String()
				block.Reset()
				continue
			}
			block.WriteString(line)
		}
	}()
	s.close = func() { cancel(); _ = resp.Body.Close() }
	t.Cleanup(s.close)
	// The first block is the retry hint: the stream is subscribed.
	if first := s.next(t); first != "retry: 5000\n" {
		t.Fatalf("first block %q", first)
	}
	return s
}

// next is the next block, skipping heartbeats; "" when the stream ended.
func (s *sseStream) next(t *testing.T) string {
	t.Helper()
	for {
		select {
		case ev, ok := <-s.events:
			if !ok {
				return ""
			}
			if strings.HasPrefix(ev, ":") {
				continue
			}
			return ev
		case <-time.After(10 * time.Second):
			t.Fatal("timed out waiting for a stream event")
		}
	}
}

// quiet fails if a non-heartbeat block arrives within d.
func (s *sseStream) quiet(t *testing.T, d time.Duration) {
	t.Helper()
	deadline := time.After(d)
	for {
		select {
		case ev, ok := <-s.events:
			if ok && !strings.HasPrefix(ev, ":") {
				t.Fatalf("unexpected stream event %q", ev)
			}
			if !ok {
				return
			}
		case <-deadline:
			return
		}
	}
}

// A new notification reaches its recipient's open streams, and only theirs;
// heartbeats keep the stream alive; a revoked session's stream ends.
func TestNotificationStreamDeliversToTheRecipientOnly(t *testing.T) {
	oldBeat, oldRecheck := streamHeartbeat, streamRecheck
	streamHeartbeat, streamRecheck = 50*time.Millisecond, 100*time.Millisecond
	t.Cleanup(func() { streamHeartbeat, streamRecheck = oldBeat, oldRecheck })

	rig := newSocialRig(t)
	f, _ := rig.fixture(t, "cstream")
	ownerTab1 := rig.stream(t, f.owner)
	ownerTab2 := rig.stream(t, f.owner)
	editorTab := rig.stream(t, f.editor)
	strangerTab := rig.stream(t, f.stranger)

	th, code := rig.openThread(t, f.reader, f.doc, "Look here")
	if code != http.StatusCreated {
		t.Fatalf("open thread: %d", code)
	}
	for i, tab := range []*sseStream{ownerTab1, ownerTab2} {
		ev := tab.next(t)
		lines := strings.Split(strings.TrimSuffix(ev, "\n"), "\n")
		if len(lines) != 3 || !strings.HasPrefix(lines[0], "id: ") || lines[1] != "event: notification" || !strings.HasPrefix(lines[2], "data: ") {
			t.Fatalf("owner tab %d event %q", i, ev)
		}
		var it notificationDTO
		if err := json.Unmarshal([]byte(strings.TrimPrefix(lines[2], "data: ")), &it); err != nil {
			t.Fatalf("decode event: %v", err)
		}
		if it.Kind != "comment" || it.ThreadID != th.ID || it.Excerpt != "Look here" || "id: "+it.ID != lines[0] ||
			it.Actor.ID != uuidString(f.reader.ID) || it.Node.ID != uuidString(f.doc.ID) {
			t.Fatalf("owner tab %d item %+v", i, it)
		}
	}
	editorTab.quiet(t, 300*time.Millisecond)
	strangerTab.quiet(t, 0)

	// A mention reaches the editor.
	if _, code := rig.reply(t, f.reader, th.ID, "@editor?", f.editor); code != http.StatusCreated {
		t.Fatal(code)
	}
	var it notificationDTO
	ev := editorTab.next(t)
	if err := json.Unmarshal([]byte(ev[strings.Index(ev, "data: ")+len("data: "):]), &it); err != nil || it.Kind != "mention" {
		t.Fatalf("editor event %q (%v)", ev, err)
	}
	strangerTab.quiet(t, 300*time.Millisecond)

	// Heartbeats are comments; the stream stays open between events.
	select {
	case ev := <-strangerTab.events:
		if ev != ": heartbeat\n" {
			t.Fatalf("heartbeat %q", ev)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no heartbeat")
	}

	// Revoking the session ends its streams at the next recheck, and the
	// reconnect is refused.
	sessions, err := rig.q.ListUserSessions(context.Background(), f.stranger.ID)
	if err != nil || len(sessions) == 0 {
		t.Fatalf("sessions: %v", err)
	}
	for _, s := range sessions {
		if _, err := rig.q.DeleteUserSession(context.Background(), store.DeleteUserSessionParams{ID: s.ID, UserID: f.stranger.ID}); err != nil {
			t.Fatal(err)
		}
	}
	waitUntil(t, "the revoked stream to end", func() bool {
		select {
		case _, ok := <-strangerTab.events:
			return !ok
		default:
			return false
		}
	})
	if got := rig.do(t, f.stranger, http.MethodGet, "/api/v1/notifications/stream", nil).code; got != http.StatusUnauthorized {
		t.Fatalf("reconnect after revocation: %d, want 401", got)
	}
}

// mentionDoc is markdown whose one paragraph mentions each id, in the inline
// marker form mdoc stores a mention as.
func mentionDoc(t *testing.T, text string, mentioned ...string) string {
	t.Helper()
	md := text
	for _, id := range mentioned {
		label := "L" + id[len(id)-4:]
		md += ` <!-- slash:mention {"id":"` + id + `","label":"` + label + `"} -->@` + label + `<!-- /slash:mention -->`
	}
	md += "\n"
	parsed, err := mdoc.Parse([]byte(md))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	// A `doc:` mention reads back as a page link (M43), so only the people
	// remain mentions.
	people := slices.DeleteFunc(slices.Clone(mentioned), func(id string) bool { return !strings.HasPrefix(id, "user:") })
	if got := mdoc.Mentions(parsed.Doc); !slices.Equal(got, people) {
		t.Fatalf("markdown %q does not parse to its mentions: %q", md, got)
	}
	return md
}

// A document mention notifies the people a new version adds — who can read
// the document, never its writer, once per version however often the job
// runs — and the job runs off the version hook.
func TestDocumentMentionsNotifyAddedReaders(t *testing.T) {
	rig := newSocialRig(t)
	ctx := context.Background()
	f, reader2 := rig.fixture(t, "cmention")
	user := func(u store.User) string { return "user:" + uuidString(u.ID) }

	// v2 by the owner names the editor, the viewer (view only), the
	// stranger, the owner and another document (a page link, telling nobody).
	v2 := rig.putVersion(t, f.doc, f.owner, mentionDoc(t, "Hi", user(f.editor), user(f.viewer), user(f.stranger), user(f.owner), "doc:"+uuidString(f.doc.ID)))
	if err := rig.notes.NotifyVersionMentions(ctx, v2.ID); err != nil {
		t.Fatalf("v2: %v", err)
	}
	// v3 by the editor keeps those and adds reader and the editor.
	v3 := rig.putVersion(t, f.doc, f.editor, mentionDoc(t, "Hi", user(f.editor), user(f.viewer), user(f.stranger), user(f.owner), user(f.reader)))
	for range 2 { // a retried job tells nobody twice
		if err := rig.notes.NotifyVersionMentions(ctx, v3.ID); err != nil {
			t.Fatalf("v3: %v", err)
		}
	}
	want := map[string][]string{
		"owner":    nil,
		"editor":   {"mention by cmention-owner"},
		"reader":   {"mention by cmention-editor"},
		"reader2":  nil,
		"viewer":   {"mention by cmention-owner"},
		"stranger": nil,
	}
	for name, u := range map[string]store.User{"owner": f.owner, "editor": f.editor, "reader": f.reader,
		"reader2": reader2, "viewer": f.viewer, "stranger": f.stranger} {
		if got := rig.kinds(t, u); !slices.Equal(got, want[name]) {
			t.Errorf("%s was told %q, want %q", name, got, want[name])
		}
	}
	it := rig.inbox(t, f.reader, "").Items[0]
	if it.ThreadID != "" || it.CommentID != "" || !strings.HasPrefix(it.Excerpt, "Hi @L") || it.Node.ID != uuidString(f.doc.ID) {
		t.Errorf("document mention item: %+v", it)
	}

	// Through the job: the version hook enqueues, Run claims and delivers
	// to reader2's open stream. Non-markdown versions enqueue nothing.
	tab := rig.stream(t, reader2)
	txt := rig.putMarkdown(t, f.space, f.owner, "plain.txt", "user:"+uuidString(reader2.ID))
	txtVer, err := rig.q.GetCurrentVersion(ctx, txt.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := rig.notes.EnqueueMentions(ctx, txtVer); err != nil {
		t.Fatal(err)
	}
	if n := pendingMentionJobs(t, rig.q); n != 0 {
		t.Fatalf("a text file's version enqueued %d mention jobs", n)
	}
	v4 := rig.putVersion(t, f.doc, f.owner, mentionDoc(t, "Hi", user(f.reader), user(reader2)))
	if err := rig.notes.EnqueueMentions(ctx, v4); err != nil {
		t.Fatal(err)
	}
	runCtx, stop := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() { rig.notes.Run(runCtx); close(done) }()
	t.Cleanup(func() { stop(); <-done })
	ev := tab.next(t)
	if !strings.Contains(ev, `"kind":"mention"`) || !strings.Contains(ev, uuidString(f.doc.ID)) {
		t.Fatalf("reader2's stream: %q", ev)
	}
	if got := rig.kinds(t, f.reader); len(got) != 1 {
		t.Errorf("reader was already mentioned in v3, told again: %q", got)
	}
	waitUntil(t, "the mention job to complete", func() bool { return pendingMentionJobs(t, rig.q) == 0 })
}

// pendingMentionJobs counts mention jobs not done.
func pendingMentionJobs(t *testing.T, q *store.Queries) int64 {
	t.Helper()
	stats, err := q.JobQueueStats(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var n int64
	for _, s := range stats {
		if s.Kind == notifications.KindMentions {
			n += s.Jobs
		}
	}
	return n
}
