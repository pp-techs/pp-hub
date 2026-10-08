package httpapi

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nhatbui/pp-hub/apps/server/ext/checklist"
	"github.com/nhatbui/pp-hub/apps/server/ext/kanban"
	"github.com/nhatbui/pp-hub/apps/server/internal/collab"
	"github.com/nhatbui/pp-hub/apps/server/internal/comments"
	"github.com/nhatbui/pp-hub/apps/server/internal/exthost"
	"github.com/nhatbui/pp-hub/apps/server/internal/notifications"
	"github.com/nhatbui/pp-hub/apps/server/internal/share"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

// Extensions (M44) and the kanban board (M45) through the production router
// against a real Postgres, or skipped like the rest of this package's
// database tests:
//
//	PP_TEST_DATABASE_URL=postgres://pphub:pphub@localhost:5433/pphub go test ./internal/httpapi/ -run Ext

type extFixture struct {
	rig      *collabRig
	space    store.Space
	admin    store.User // instance administrator and space admin
	owner    store.User // creates the boards
	member   store.User // a member of the space, granted nothing
	stranger store.User // not in the space
}

func newExtFixture(t *testing.T, label string) extFixture {
	t.Helper()
	ctx := context.Background()
	rig := newCollabRigWith(t, collab.Config{}, func(s *Server) {
		notes := notifications.NewService(s.store, s.blobs, s.share)
		s.notifications = notes
		s.comments = comments.NewService(s.store, notes)
		reg, err := exthost.New(exthost.Config{
			Pool: s.store.DB().(*pgxpool.Pool), Store: s.store, FS: s.fs, Share: s.share,
			Notifications: notes, StreamRecheck: 150 * time.Millisecond,
		}, kanban.New(), checklist.New())
		if err != nil {
			t.Fatalf("exthost: %v", err)
		}
		s.fs.SetExtHooks(reg)
		if err := reg.Start(ctx); err != nil {
			t.Fatalf("start extensions: %v", err)
		}
		s.useExt(reg)
	})
	t.Cleanup(func() {
		_, _ = rig.srv.store.DB().Exec(ctx, `DELETE FROM ext_state WHERE ext_id IN ('kanban', 'checklist')`)
	})
	// A leftover switch from another run must not decide this one.
	_, _ = rig.srv.store.DB().Exec(ctx, `DELETE FROM ext_state WHERE ext_id IN ('kanban', 'checklist')`)

	f := extFixture{rig: rig}
	f.admin = feedUser(t, rig.q, label+"-admin")
	if _, err := rig.srv.store.DB().Exec(ctx, `UPDATE users SET is_admin = true WHERE id = $1`, f.admin.ID); err != nil {
		t.Fatal(err)
	}
	f.admin.IsAdmin = true
	f.owner = feedUser(t, rig.q, label+"-owner")
	f.member = feedUser(t, rig.q, label+"-member")
	f.stranger = feedUser(t, rig.q, label+"-stranger")
	sp, err := rig.srv.spaces.Create(ctx, fmt.Sprintf("ext-%s-%d", label, os.Getpid()), f.admin.ID)
	if err != nil {
		t.Fatalf("create space: %v", err)
	}
	t.Cleanup(func() { _ = rig.q.DeleteSpace(ctx, sp.ID) })
	f.space = sp
	for _, u := range []store.User{f.owner, f.member} {
		if err := rig.srv.spaces.AddMember(ctx, sp.ID, u.ID, false); err != nil {
			t.Fatalf("add member: %v", err)
		}
	}
	for _, u := range []store.User{f.admin, f.owner, f.member, f.stranger} {
		rig.login(t, u)
	}
	return f
}

func (f extFixture) spacePath(rest string) string {
	return "/api/v1/spaces/" + uuidString(f.space.ID) + rest
}

func (f extFixture) enable(t *testing.T) {
	t.Helper()
	res := f.rig.do(t, f.admin, http.MethodPatch, "/api/v1/admin/extensions/kanban", map[string]bool{"enabled": true})
	if res.code != http.StatusOK {
		t.Fatalf("enable kanban: %d %s", res.code, res.body)
	}
}

// newBoard creates a board as the owner.
func (f extFixture) newBoard(t *testing.T, name string) nodeDTO {
	t.Helper()
	res := f.rig.do(t, f.owner, http.MethodPost, f.spacePath("/ext-nodes"),
		map[string]string{"ext_kind": "kanban.board", "name": name})
	if res.code != http.StatusCreated {
		t.Fatalf("create board: %d %s", res.code, res.body)
	}
	return decodeJSON[nodeDTO](t, res)
}

type boardView struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Caps    struct{ Edit, View bool }
	Columns []struct {
		ID       string `json:"id"`
		Title    string `json:"title"`
		Position string `json:"position"`
		WipLimit *int   `json:"wip_limit"`
	} `json:"columns"`
	Cards []struct {
		ID        string   `json:"id"`
		ColumnID  string   `json:"column_id"`
		Title     string   `json:"title"`
		Position  string   `json:"position"`
		Rev       int32    `json:"rev"`
		LabelIDs  []string `json:"label_ids"`
		Assignees []struct {
			ID string `json:"id"`
		} `json:"assignees"`
	} `json:"cards"`
	Labels []struct {
		ID, Name, Color string
	} `json:"labels"`
}

func boardPath(b nodeDTO, rest string) string {
	return "/api/v1/ext/kanban/boards/" + b.ID + rest
}

func (f extFixture) board(t *testing.T, u store.User, b nodeDTO) (boardView, int) {
	t.Helper()
	res := f.rig.do(t, u, http.MethodGet, boardPath(b, "/"), nil)
	if res.code != http.StatusOK {
		return boardView{}, res.code
	}
	return decodeJSON[boardView](t, res), res.code
}

func (f extFixture) grant(t *testing.T, node nodeDTO, to store.User, level share.Level) {
	t.Helper()
	var id pgtype.UUID
	if err := id.Scan(node.ID); err != nil {
		t.Fatal(err)
	}
	_, err := f.rig.srv.share.UpsertDirect(context.Background(), id, to.ID,
		share.GrantCaps{Level: level, CanDownload: true}, pgtype.Timestamptz{}, f.owner.ID, false)
	if err != nil {
		t.Fatalf("grant: %v", err)
	}
}

func (f extFixture) addCard(t *testing.T, u store.User, b nodeDTO, col, title string) (string, rigResponse) {
	t.Helper()
	res := f.rig.do(t, u, http.MethodPost, boardPath(b, "/cards"), map[string]any{"column_id": col, "title": title})
	if res.code != http.StatusCreated {
		return "", res
	}
	var c struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(res.body, &c); err != nil {
		t.Fatal(err)
	}
	return c.ID, res
}

// An extension ships off, an instance admin switches it on, and a space admin
// may switch it off for a space; every board route answers 403 meanwhile.
func TestExtSwitchesGateEveryRoute(t *testing.T) {
	f := newExtFixture(t, "switch")
	rig := f.rig

	// Off: no board can be made.
	res := rig.do(t, f.owner, http.MethodPost, f.spacePath("/ext-nodes"),
		map[string]string{"ext_kind": "kanban.board", "name": "Plan"})
	if res.code != http.StatusForbidden {
		t.Fatalf("create while off: %d %s", res.code, res.body)
	}
	// Only an instance admin flips the instance switch.
	if res := rig.do(t, f.owner, http.MethodPatch, "/api/v1/admin/extensions/kanban", map[string]bool{"enabled": true}); res.code != http.StatusForbidden {
		t.Fatalf("member flips instance switch: %d", res.code)
	}
	f.enable(t)
	board := f.newBoard(t, "Plan")
	if board.Kind != "ext" || board.ExtKind != "kanban.board" {
		t.Fatalf("board node: %+v", board)
	}
	if _, code := f.board(t, f.owner, board); code != http.StatusOK {
		t.Fatalf("owner reads board: %d", code)
	}

	// A space admin turns it off for the space; a plain member cannot.
	if res := rig.do(t, f.owner, http.MethodPut, f.spacePath("/extensions/kanban"), map[string]bool{"enabled": false}); res.code != http.StatusForbidden {
		t.Fatalf("member flips space switch: %d", res.code)
	}
	if res := rig.do(t, f.admin, http.MethodPut, f.spacePath("/extensions/kanban"), map[string]bool{"enabled": false}); res.code != http.StatusOK {
		t.Fatalf("admin flips space switch: %d %s", res.code, res.body)
	}
	if _, code := f.board(t, f.owner, board); code != http.StatusForbidden {
		t.Fatalf("board while off for the space: %d", code)
	}
	list := decodeJSON[[]extensionDTO](t, rig.do(t, f.owner, http.MethodGet, "/api/v1/extensions?space_id="+uuidString(f.space.ID), nil))
	// Every compiled-in extension is listed; only kanban was switched on.
	byID := map[string]extensionDTO{}
	for _, e := range list {
		byID[e.ID] = e
	}
	if len(list) != 2 || !byID["kanban"].Instance || byID["kanban"].Enabled || byID["checklist"].Instance {
		t.Fatalf("listing: %+v", list)
	}
	for id, e := range byID {
		if e.Version != "1.0.0" || e.Description == "" || e.Author != "pp-hub" {
			t.Fatalf("%s listing lacks manifest metadata: %+v", id, e)
		}
	}
	// The node stays, under Apps with its kind — and nowhere in the drive. The
	// app's own list answers 403 while the app is off for the space.
	if res := rig.do(t, f.owner, http.MethodGet, f.spacePath("/extensions/kanban/nodes"), nil); res.code != http.StatusForbidden {
		t.Fatalf("apps list while off: %d", res.code)
	}
	if kids := decodeJSON[[]nodeDTO](t, rig.do(t, f.owner, http.MethodGet, f.spacePath("/children"), nil)); len(kids) != 0 {
		t.Fatalf("drive listing shows an app node: %+v", kids)
	}

	// Back on, and the instance switch outranks it.
	rig.do(t, f.admin, http.MethodPut, f.spacePath("/extensions/kanban"), map[string]bool{"enabled": true})
	if kids := decodeJSON[[]nodeDTO](t, rig.do(t, f.owner, http.MethodGet, f.spacePath("/extensions/kanban/nodes"), nil)); len(kids) != 1 || kids[0].ExtKind != "kanban.board" {
		t.Fatalf("apps list: %+v", kids)
	}
	if _, code := f.board(t, f.owner, board); code != http.StatusOK {
		t.Fatalf("board after re-enable: %d", code)
	}
	rig.do(t, f.admin, http.MethodPatch, "/api/v1/admin/extensions/kanban", map[string]bool{"enabled": false})
	if res := rig.do(t, f.owner, http.MethodGet, boardPath(board, "/"), nil); res.code != http.StatusForbidden {
		t.Fatalf("board while off for the instance: %d", res.code)
	}
}

// A board is a node: access is the drive's, and inherits from a folder.
func TestExtBoardAccessIsTheNodes(t *testing.T) {
	f := newExtFixture(t, "access")
	rig := f.rig
	f.enable(t)
	board := f.newBoard(t, "Roadmap")
	view, _ := f.board(t, f.owner, board)
	col := view.Columns[0].ID

	if _, code := f.board(t, f.member, board); code != http.StatusForbidden {
		t.Fatalf("member with no grant: %d", code)
	}
	if _, code := f.board(t, f.stranger, board); code != http.StatusForbidden {
		t.Fatalf("stranger: %d", code)
	}
	f.grant(t, board, f.member, share.LevelView)
	if _, code := f.board(t, f.member, board); code != http.StatusOK {
		t.Fatalf("viewer: %d", code)
	}
	if _, res := f.addCard(t, f.member, board, col, "nope"); res.code != http.StatusForbidden {
		t.Fatalf("viewer adds a card: %d", res.code)
	}
	f.grant(t, board, f.member, share.LevelEdit)
	if _, res := f.addCard(t, f.member, board, col, "yes"); res.code != http.StatusCreated {
		t.Fatalf("editor adds a card: %d %s", res.code, res.body)
	}

	// Inheritance: a board inside a shared folder is the folder's.
	folder := decodeJSON[nodeDTO](t, rig.do(t, f.owner, http.MethodPost, f.spacePath("/dirs"), map[string]string{"name": "Projects"}))
	inner := decodeJSON[nodeDTO](t, rig.do(t, f.owner, http.MethodPost, f.spacePath("/ext-nodes"),
		map[string]string{"ext_kind": "kanban.board", "name": "Inner", "parent_id": folder.ID}))
	if _, code := f.board(t, f.stranger, inner); code != http.StatusForbidden {
		t.Fatalf("before the folder grant: %d", code)
	}
	f.grant(t, folder, f.member, share.LevelView)
	if _, code := f.board(t, f.member, inner); code != http.StatusOK {
		t.Fatalf("grant on the folder does not reach the board: %d", code)
	}

	// A board cannot hold a node, and a file kind is not a board.
	res := rig.do(t, f.owner, http.MethodPost, f.spacePath("/dirs"), map[string]string{"name": "x", "parent_id": board.ID})
	if res.code != http.StatusBadRequest {
		t.Fatalf("mkdir under a board: %d %s", res.code, res.body)
	}
	if res := rig.do(t, f.owner, http.MethodGet, "/api/v1/ext/kanban/boards/"+folder.ID+"/", nil); res.code != http.StatusNotFound {
		t.Fatalf("a folder read as a board: %d", res.code)
	}
	// It has no bytes to download.
	if res := rig.do(t, f.owner, http.MethodGet, "/api/v1/nodes/"+board.ID+"/download", nil); res.code != http.StatusBadRequest {
		t.Fatalf("download of a board: %d", res.code)
	}
	// A link to a board is refused: it has no link view in this release.
	res = rig.do(t, f.owner, http.MethodPost, "/api/v1/nodes/"+board.ID+"/shares", map[string]any{"kind": "link", "level": "view", "can_download": true})
	if res.code != http.StatusCreated {
		t.Fatalf("link to a board: %d %s", res.code, res.body)
	}
	link := decodeJSON[shareDTO](t, res)
	token := link.URL[strings.LastIndex(link.URL, "/")+1:]
	if res := rig.anon(t, "/s/"+token+"/", nil); res.code != http.StatusUnprocessableEntity {
		t.Fatalf("link landing for a board: %d %s", res.code, res.body)
	}
}

// Cards: append order, moves, the rev check, assignees and labels, and the
// column delete rule.
func TestExtKanbanCards(t *testing.T) {
	f := newExtFixture(t, "cards")
	rig := f.rig
	f.enable(t)
	board := f.newBoard(t, "Sprint")
	view, _ := f.board(t, f.owner, board)
	if len(view.Columns) != 3 || view.Columns[0].Title != "To do" {
		t.Fatalf("default columns: %+v", view.Columns)
	}
	todo, doing, done := view.Columns[0].ID, view.Columns[1].ID, view.Columns[2].ID

	a, _ := f.addCard(t, f.owner, board, todo, "A")
	b, _ := f.addCard(t, f.owner, board, todo, "B")
	c, _ := f.addCard(t, f.owner, board, todo, "C")
	order := func() string {
		v, _ := f.board(t, f.owner, board)
		names := map[string]string{a: "A", b: "B", c: "C"}
		var out []string
		for _, col := range v.Columns {
			var in []string
			for _, cd := range v.Cards {
				if cd.ColumnID == col.ID {
					in = append(in, names[cd.ID])
				}
			}
			out = append(out, strings.Join(in, ""))
		}
		return strings.Join(out, "|")
	}
	if got := order(); got != "ABC||" {
		t.Fatalf("order %s", got)
	}
	move := func(card, col, after string) rigResponse {
		return rig.do(t, f.owner, http.MethodPost, boardPath(board, "/cards/"+card+"/move"),
			map[string]string{"column_id": col, "after_id": after})
	}
	// C to the top, A to another column, B into the middle of a column.
	if res := move(c, todo, ""); res.code != http.StatusOK {
		t.Fatalf("move to top: %d %s", res.code, res.body)
	}
	move(a, doing, "")
	move(b, doing, a)
	if got := order(); got != "C|AB|" {
		t.Fatalf("after moves: %s", got)
	}
	move(c, doing, a) // between A and B
	if got := order(); got != "|ACB|" {
		t.Fatalf("between: %s", got)
	}
	// An anchor from another column, and a column from another board.
	if res := move(b, todo, a); res.code != http.StatusBadRequest {
		t.Fatalf("anchor in another column: %d", res.code)
	}
	other := f.newBoard(t, "Other")
	ov, _ := f.board(t, f.owner, other)
	if res := move(b, ov.Columns[0].ID, ""); res.code != http.StatusBadRequest {
		t.Fatalf("column of another board: %d", res.code)
	}

	// Edits carry the rev they were made against.
	v, _ := f.board(t, f.owner, board)
	var rev int32
	for _, cd := range v.Cards {
		if cd.ID == a {
			rev = cd.Rev
		}
	}
	patch := func(rev int32, body map[string]any) rigResponse {
		body["if_rev"] = rev
		return rig.do(t, f.owner, http.MethodPatch, boardPath(board, "/cards/"+a), body)
	}
	if res := patch(rev, map[string]any{"title": "A2", "due_at": "2030-01-02T03:04:05Z"}); res.code != http.StatusOK {
		t.Fatalf("edit: %d %s", res.code, res.body)
	}
	if res := patch(rev, map[string]any{"title": "stale"}); res.code != http.StatusConflict {
		t.Fatalf("stale edit: %d", res.code)
	}
	if res := rig.do(t, f.owner, http.MethodPatch, boardPath(board, "/cards/"+a), map[string]any{"title": "no rev"}); res.code != http.StatusBadRequest {
		t.Fatalf("edit without rev: %d", res.code)
	}

	// Assignees must be able to open the board; labels must be the board's.
	rev++
	if res := patch(rev, map[string]any{"assignees": []string{uuidString(f.member.ID)}}); res.code != http.StatusConflict {
		t.Fatalf("assigning someone without access: %d %s", res.code, res.body)
	}
	f.grant(t, board, f.member, share.LevelView)
	if res := patch(rev, map[string]any{"assignees": []string{uuidString(f.member.ID)}}); res.code != http.StatusOK {
		t.Fatalf("assigning a viewer: %d %s", res.code, res.body)
	}
	lab := decodeJSON[struct{ ID string }](t, rig.do(t, f.owner, http.MethodPost, boardPath(board, "/labels"),
		map[string]string{"name": "bug", "color": "#ff0000"}))
	foreign := decodeJSON[struct{ ID string }](t, rig.do(t, f.owner, http.MethodPost, boardPath(other, "/labels"),
		map[string]string{"name": "bug", "color": "#ff0000"}))
	rev += 1
	if res := patch(rev, map[string]any{"labels": []string{foreign.ID}}); res.code != http.StatusConflict {
		t.Fatalf("another board's label: %d", res.code)
	}
	if res := patch(rev, map[string]any{"labels": []string{lab.ID}}); res.code != http.StatusOK {
		t.Fatalf("label: %d %s", res.code, res.body)
	}
	if res := rig.do(t, f.owner, http.MethodPost, boardPath(board, "/labels"), map[string]string{"name": "bug", "color": "#00ff00"}); res.code != http.StatusConflict {
		t.Fatalf("duplicate label: %d", res.code)
	}
	if res := rig.do(t, f.owner, http.MethodPost, boardPath(board, "/labels"), map[string]string{"name": "x", "color": "red"}); res.code != http.StatusBadRequest {
		t.Fatalf("bad colour: %d", res.code)
	}
	v, _ = f.board(t, f.owner, board)
	for _, cd := range v.Cards {
		if cd.ID == a && (len(cd.LabelIDs) != 1 || len(cd.Assignees) != 1) {
			t.Fatalf("card A: %+v", cd)
		}
	}

	// A column with cards is not deleted away; its cards can move on.
	if res := rig.do(t, f.owner, http.MethodDelete, boardPath(board, "/columns/"+doing), nil); res.code != http.StatusConflict {
		t.Fatalf("delete a full column: %d", res.code)
	}
	if res := rig.do(t, f.owner, http.MethodDelete, boardPath(board, "/columns/"+doing+"?move_to="+done), nil); res.code != http.StatusNoContent {
		t.Fatalf("delete with move_to: %d", res.code)
	}
	if got := order(); got != "|ACB" {
		t.Fatalf("after column delete: %s", got)
	}
	// Columns reorder.
	if res := rig.do(t, f.owner, http.MethodPatch, boardPath(board, "/columns/"+done), map[string]any{"after_id": nil}); res.code != http.StatusOK {
		t.Fatalf("move column: %d %s", res.code, res.body)
	}
	v, _ = f.board(t, f.owner, board)
	if v.Columns[0].ID != done {
		t.Fatalf("column order: %+v", v.Columns)
	}
	if res := rig.do(t, f.owner, http.MethodDelete, boardPath(board, "/cards/"+a), nil); res.code != http.StatusNoContent {
		t.Fatalf("delete card: %d", res.code)
	}
}

// A copy brings columns, cards and labels; a purge takes every row with it.
func TestExtBoardCopyAndPurge(t *testing.T) {
	f := newExtFixture(t, "copy")
	rig := f.rig
	f.enable(t)
	board := f.newBoard(t, "Original")
	v, _ := f.board(t, f.owner, board)
	card, _ := f.addCard(t, f.owner, board, v.Columns[0].ID, "Keep me")
	lab := decodeJSON[struct{ ID string }](t, rig.do(t, f.owner, http.MethodPost, boardPath(board, "/labels"),
		map[string]string{"name": "p1", "color": "#123456"}))
	cv, _ := f.board(t, f.owner, board)
	rig.do(t, f.owner, http.MethodPatch, boardPath(board, "/cards/"+card),
		map[string]any{"if_rev": cv.Cards[0].Rev, "labels": []string{lab.ID}})

	res := rig.do(t, f.owner, http.MethodPost, "/api/v1/nodes/"+board.ID+"/copy", map[string]string{})
	if res.code != http.StatusCreated && res.code != http.StatusOK {
		t.Fatalf("copy: %d %s", res.code, res.body)
	}
	cp := decodeJSON[nodeDTO](t, res)
	if cp.ID == board.ID || cp.ExtKind != "kanban.board" || cp.Name == board.Name {
		t.Fatalf("copy node: %+v", cp)
	}
	copied, code := f.board(t, f.owner, cp)
	if code != http.StatusOK || len(copied.Columns) != 3 || len(copied.Cards) != 1 || len(copied.Labels) != 1 ||
		copied.Cards[0].Title != "Keep me" || len(copied.Cards[0].LabelIDs) != 1 || copied.Cards[0].LabelIDs[0] != copied.Labels[0].ID {
		t.Fatalf("copy contents: %d %+v", code, copied)
	}
	if copied.Cards[0].ID == card || copied.Columns[0].ID == v.Columns[0].ID {
		t.Fatal("copy shares rows with the original")
	}
	// Editing the copy leaves the original alone.
	f.addCard(t, f.owner, cp, copied.Columns[0].ID, "Only in the copy")
	if o, _ := f.board(t, f.owner, board); len(o.Cards) != 1 {
		t.Fatalf("original changed: %+v", o.Cards)
	}

	count := func(table string, nodeID string) int {
		var n int
		if err := rig.srv.store.DB().QueryRow(context.Background(),
			"SELECT count(*) FROM ext_kanban."+table+" WHERE board_id = $1", nodeID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if count("cards", board.ID) != 1 || count("columns", board.ID) != 3 {
		t.Fatal("rows missing before the purge")
	}
	if res := rig.do(t, f.owner, http.MethodDelete, "/api/v1/nodes/"+board.ID, nil); res.code >= 300 {
		t.Fatalf("trash: %d %s", res.code, res.body)
	}
	// Trashed: the board answers 404 but its rows are kept for a restore.
	if res := rig.do(t, f.owner, http.MethodGet, boardPath(board, "/"), nil); res.code != http.StatusNotFound {
		t.Fatalf("trashed board: %d", res.code)
	}
	if count("cards", board.ID) != 1 {
		t.Fatal("trash dropped the rows")
	}
	if res := rig.do(t, f.owner, http.MethodDelete, "/api/v1/nodes/"+board.ID+"/purge", nil); res.code >= 300 {
		t.Fatalf("purge: %d %s", res.code, res.body)
	}
	for _, table := range []string{"cards", "columns", "labels"} {
		if n := count(table, board.ID); n != 0 {
			t.Fatalf("%s survived the purge: %d", table, n)
		}
	}
	if count("cards", cp.ID) != 2 {
		t.Fatal("the purge reached the copy")
	}
}

// An open stream sees other people's changes, and ends when the viewer loses
// access.
func TestExtBoardStream(t *testing.T) {
	f := newExtFixture(t, "stream")
	rig := f.rig
	f.enable(t)
	board := f.newBoard(t, "Live")
	v, _ := f.board(t, f.owner, board)
	f.grant(t, board, f.member, share.LevelView)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, rig.url+boardPath(board, "/events"), nil)
	req.Header.Set("Cookie", rig.cookie(f.member))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("stream: %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	blocks := make(chan string, 16)
	go func() {
		defer close(blocks)
		br := bufio.NewReader(resp.Body)
		var b strings.Builder
		for {
			line, err := br.ReadString('\n')
			if err != nil {
				return
			}
			if line == "\n" {
				blocks <- b.String()
				b.Reset()
				continue
			}
			b.WriteString(line)
		}
	}()
	next := func(d time.Duration) (string, bool) {
		timeout := time.After(d)
		for {
			select {
			case blk, ok := <-blocks:
				if !ok {
					return "", false
				}
				if strings.HasPrefix(blk, ":") || strings.HasPrefix(blk, "retry") {
					continue
				}
				return blk, true
			case <-timeout:
				return "timeout", true
			}
		}
	}

	f.addCard(t, f.owner, board, v.Columns[0].ID, "Seen live")
	blk, ok := next(2 * time.Second)
	if !ok || !strings.Contains(blk, "event: board") || !strings.Contains(blk, `"type":"card.upsert"`) || !strings.Contains(blk, "Seen live") {
		t.Fatalf("event %q", blk)
	}

	// Revoked: the next recheck closes the stream.
	shares, err := rig.srv.share.ListForNode(context.Background(), mustUUID(t, board.ID))
	if err != nil || len(shares) == 0 {
		t.Fatalf("shares: %v %d", err, len(shares))
	}
	for _, sh := range shares {
		if _, err := rig.srv.share.Revoke(context.Background(), mustUUID(t, board.ID), sh.ID); err != nil {
			t.Fatal(err)
		}
	}
	for {
		blk, ok := next(3 * time.Second)
		if !ok {
			break // closed
		}
		if blk == "timeout" {
			t.Fatal("the stream outlived the share")
		}
	}
}

func mustUUID(t *testing.T, s string) pgtype.UUID {
	t.Helper()
	var id pgtype.UUID
	if err := id.Scan(s); err != nil {
		t.Fatal(err)
	}
	return id
}

type cardView struct {
	ID           string   `json:"id"`
	Rev          int32    `json:"rev"`
	CommentCount int64    `json:"comment_count"`
	LinkIDs      []string `json:"link_ids"`
	Links        []struct {
		ID         string `json:"id"`
		Accessible bool   `json:"accessible"`
		Name       string `json:"name"`
	} `json:"links"`
}

func (f extFixture) cards(t *testing.T, u store.User, b nodeDTO) []cardView {
	t.Helper()
	res := f.rig.do(t, u, http.MethodGet, boardPath(b, "/"), nil)
	if res.code != http.StatusOK {
		t.Fatalf("board: %d %s", res.code, res.body)
	}
	return decodeJSON[struct {
		Cards []cardView `json:"cards"`
	}](t, res).Cards
}

// A card carries a discussion: threads about "card:<id>", mentions that
// notify, a count on the card, and a deleted card takes its threads along.
func TestExtCardDiscussion(t *testing.T) {
	f := newExtFixture(t, "discuss")
	rig := f.rig
	f.enable(t)
	board := f.newBoard(t, "Talk")
	v, _ := f.board(t, f.owner, board)
	card, _ := f.addCard(t, f.owner, board, v.Columns[0].ID, "Discuss me")
	f.grant(t, board, f.member, share.LevelView)
	subject := "card:" + card
	post := func(u store.User, node nodeDTO, body map[string]any) rigResponse {
		return rig.do(t, u, http.MethodPost, "/api/v1/nodes/"+node.ID+"/comment-threads", body)
	}

	// The subject must name a card of this board; a document takes none.
	if res := post(f.owner, board, map[string]any{"body": "x"}); res.code != http.StatusBadRequest {
		t.Fatalf("thread without subject: %d", res.code)
	}
	if res := post(f.owner, board, map[string]any{"body": "x", "subject": "card:" + uuidString(f.owner.ID)}); res.code != http.StatusBadRequest {
		t.Fatalf("thread on a card that is not there: %d", res.code)
	}
	other := f.newBoard(t, "Elsewhere")
	if res := post(f.owner, other, map[string]any{"body": "x", "subject": subject}); res.code != http.StatusBadRequest {
		t.Fatalf("thread on another board's card: %d", res.code)
	}
	doc := decodeJSON[nodeDTO](t, rig.do(t, f.owner, http.MethodPost, f.spacePath("/documents"), map[string]string{"kind": "markdown", "name": "Spec"}))
	if res := post(f.owner, doc, map[string]any{"body": "x", "subject": subject}); res.code != http.StatusBadRequest {
		t.Fatalf("subject on a document: %d", res.code)
	}

	// A viewer joins in and mentions the owner.
	res := post(f.member, board, map[string]any{"body": "Looks good @owner", "subject": subject, "mentions": []string{uuidString(f.owner.ID)}})
	if res.code != http.StatusCreated {
		t.Fatalf("open thread: %d %s", res.code, res.body)
	}
	th := decodeJSON[threadDTO](t, res)
	if th.Subject != subject {
		t.Fatalf("thread subject %q", th.Subject)
	}
	if res := post(f.stranger, board, map[string]any{"body": "x", "subject": subject}); res.code != http.StatusForbidden {
		t.Fatalf("stranger comments: %d", res.code)
	}
	list := decodeJSON[struct{ Threads []threadDTO }](t, rig.do(t, f.owner, http.MethodGet, "/api/v1/nodes/"+board.ID+"/comments?subject="+subject, nil))
	if len(list.Threads) != 1 || list.Threads[0].ID != th.ID {
		t.Fatalf("threads for the card: %+v", list.Threads)
	}
	none := decodeJSON[struct{ Threads []threadDTO }](t, rig.do(t, f.owner, http.MethodGet, "/api/v1/nodes/"+board.ID+"/comments?subject=card:nothing", nil))
	if len(none.Threads) != 0 {
		t.Fatalf("threads for another subject: %+v", none.Threads)
	}
	if cs := f.cards(t, f.owner, board); len(cs) != 1 || cs[0].CommentCount != 1 {
		t.Fatalf("comment count: %+v", cs)
	}

	// The owner was told, and the note points at the card.
	inbox := decodeJSON[struct {
		Items []notificationDTO `json:"items"`
	}](t, rig.do(t, f.owner, http.MethodGet, "/api/v1/notifications", nil))
	if len(inbox.Items) != 1 || inbox.Items[0].Kind != "mention" || inbox.Items[0].Subject != subject || inbox.Items[0].Node.ID != board.ID {
		t.Fatalf("inbox: %+v", inbox.Items)
	}

	// Deleting the card deletes its discussion.
	if res := rig.do(t, f.owner, http.MethodDelete, boardPath(board, "/cards/"+card), nil); res.code != http.StatusNoContent {
		t.Fatalf("delete card: %d", res.code)
	}
	var left int
	if err := rig.srv.store.DB().QueryRow(context.Background(),
		"SELECT count(*) FROM comment_threads WHERE node_id = $1", board.ID).Scan(&left); err != nil || left != 0 {
		t.Fatalf("threads left: %d %v", left, err)
	}
}

// A link shows each viewer only what they may open, and a card can only link
// to what its writer can see.
func TestExtCardLinksAreResolvedPerViewer(t *testing.T) {
	f := newExtFixture(t, "links")
	rig := f.rig
	f.enable(t)
	board := f.newBoard(t, "Links")
	v, _ := f.board(t, f.owner, board)
	card, _ := f.addCard(t, f.owner, board, v.Columns[0].ID, "See spec")
	f.grant(t, board, f.member, share.LevelView)

	spec := decodeJSON[nodeDTO](t, rig.do(t, f.owner, http.MethodPost, f.spacePath("/documents"), map[string]string{"kind": "markdown", "name": "Secret plan"}))
	// Someone who cannot open the file cannot link it.
	hidden := decodeJSON[nodeDTO](t, rig.do(t, f.member, http.MethodPost, f.spacePath("/documents"), map[string]string{"kind": "markdown", "name": "Mine"}))
	f.grant(t, board, f.member, share.LevelEdit)
	rev := f.cards(t, f.owner, board)[0].Rev
	patch := func(u store.User, rev int32, links []string) rigResponse {
		return rig.do(t, u, http.MethodPatch, boardPath(board, "/cards/"+card), map[string]any{"if_rev": rev, "links": links})
	}
	if res := patch(f.member, rev, []string{spec.ID}); res.code != http.StatusConflict {
		t.Fatalf("linking a file the writer cannot open: %d %s", res.code, res.body)
	}
	if res := patch(f.owner, rev, []string{spec.ID, hidden.ID}); res.code != http.StatusConflict {
		t.Fatalf("linking another person's private file: %d", res.code)
	}
	if res := patch(f.owner, rev, []string{spec.ID, board.ID}); res.code != http.StatusConflict {
		t.Fatalf("a card linking its own board: %d", res.code)
	}
	if res := patch(f.owner, rev, []string{spec.ID}); res.code != http.StatusOK {
		t.Fatalf("link: %d %s", res.code, res.body)
	}

	ownerView := f.cards(t, f.owner, board)[0]
	if len(ownerView.Links) != 1 || !ownerView.Links[0].Accessible || ownerView.Links[0].Name != "Secret plan.md" {
		t.Fatalf("owner's links: %+v", ownerView.Links)
	}
	memberView := f.cards(t, f.member, board)[0]
	if len(memberView.LinkIDs) != 1 || len(memberView.Links) != 1 || memberView.Links[0].Accessible || memberView.Links[0].Name != "" {
		t.Fatalf("member must see a link, not its name: %+v", memberView.Links)
	}
	if strings.Contains(string(rig.do(t, f.member, http.MethodGet, boardPath(board, "/"), nil).body), "Secret plan") {
		t.Fatal("a name leaked to a viewer without access")
	}
	f.grant(t, spec, f.member, share.LevelView)
	if l := f.cards(t, f.member, board)[0].Links; len(l) != 1 || !l[0].Accessible || l[0].Name != "Secret plan.md" {
		t.Fatalf("after the grant: %+v", l)
	}

	// An open stream never carries resolved links: a broadcast reaches everyone.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, rig.url+boardPath(board, "/events"), nil)
	req.Header.Set("Cookie", rig.cookie(f.member))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	br := bufio.NewReader(resp.Body)
	if line, _ := br.ReadString('\n'); !strings.HasPrefix(line, "retry") {
		t.Fatalf("first line %q", line)
	}
	br.ReadString('\n')
	rev = f.cards(t, f.owner, board)[0].Rev
	if res := rig.do(t, f.owner, http.MethodPatch, boardPath(board, "/cards/"+card), map[string]any{"if_rev": rev, "title": "Renamed"}); res.code != http.StatusOK {
		t.Fatalf("rename: %d", res.code)
	}
	var event strings.Builder
	for {
		line, err := br.ReadString('\n')
		if err != nil || line == "\n" {
			break
		}
		event.WriteString(line)
	}
	if !strings.Contains(event.String(), "link_ids") || strings.Contains(event.String(), "Secret plan") || strings.Contains(event.String(), "accessible") {
		t.Fatalf("event carries resolved links: %s", event.String())
	}
}

// Being put on a card notifies the person, once, and not the one doing it.
func TestExtAssignmentNotifies(t *testing.T) {
	f := newExtFixture(t, "assign")
	rig := f.rig
	f.enable(t)
	board := f.newBoard(t, "Assign")
	v, _ := f.board(t, f.owner, board)
	card, _ := f.addCard(t, f.owner, board, v.Columns[0].ID, "Do the thing")
	f.grant(t, board, f.member, share.LevelView)

	inbox := func(u store.User) []notificationDTO {
		return decodeJSON[struct {
			Items []notificationDTO `json:"items"`
		}](t, rig.do(t, u, http.MethodGet, "/api/v1/notifications", nil)).Items
	}
	rev := f.cards(t, f.owner, board)[0].Rev
	set := func(rev int32, who ...store.User) rigResponse {
		return rig.do(t, f.owner, http.MethodPatch, boardPath(board, "/cards/"+card),
			map[string]any{"if_rev": rev, "assignees": ids(who...)})
	}
	if res := set(rev, f.member, f.owner); res.code != http.StatusOK {
		t.Fatalf("assign: %d %s", res.code, res.body)
	}
	got := inbox(f.member)
	if len(got) != 1 || got[0].Kind != "ext:kanban:assigned" || got[0].Subject != "card:"+card || got[0].Excerpt != "Do the thing" || got[0].Node.ID != board.ID {
		t.Fatalf("assignee inbox: %+v", got)
	}
	if len(inbox(f.owner)) != 0 {
		t.Fatal("the person assigning was notified of their own act")
	}
	// Saving again with the same people tells nobody twice.
	if res := set(rev+1, f.member, f.owner); res.code != http.StatusOK {
		t.Fatalf("reassign: %d", res.code)
	}
	if n := len(inbox(f.member)); n != 1 {
		t.Fatalf("assignee notified %d times", n)
	}
}

// A second extension, written against extsdk alone, lives beside kanban: each
// route answers only for its own kind, access is still the node's, and a purge
// takes its rows.
func TestExtSecondExtensionSharesTheHost(t *testing.T) {
	f := newExtFixture(t, "second")
	rig := f.rig
	f.enable(t)
	if res := rig.do(t, f.admin, http.MethodPatch, "/api/v1/admin/extensions/checklist", map[string]bool{"enabled": true}); res.code != http.StatusOK {
		t.Fatalf("enable checklist: %d %s", res.code, res.body)
	}
	mk := func(kind, name string) nodeDTO {
		res := rig.do(t, f.owner, http.MethodPost, f.spacePath("/ext-nodes"), map[string]string{"ext_kind": kind, "name": name})
		if res.code != http.StatusCreated {
			t.Fatalf("create %s: %d %s", kind, res.code, res.body)
		}
		return decodeJSON[nodeDTO](t, res)
	}
	list := mk("checklist.list", "Packing")
	board := mk("kanban.board", "Plan")
	listPath := func(rest string) string { return "/api/v1/ext/checklist/lists/" + list.ID + rest }

	// A route trusts its kind, not the id: a board is no checklist, and back.
	if res := rig.do(t, f.owner, http.MethodGet, "/api/v1/ext/checklist/lists/"+board.ID+"/", nil); res.code != http.StatusNotFound {
		t.Fatalf("board read as a checklist: %d", res.code)
	}
	if res := rig.do(t, f.owner, http.MethodGet, boardPath(list, "/"), nil); res.code != http.StatusNotFound {
		t.Fatalf("checklist read as a board: %d", res.code)
	}

	add := rig.do(t, f.owner, http.MethodPost, listPath("/items"), map[string]string{"text": "Passport"})
	if add.code != http.StatusCreated {
		t.Fatalf("add: %d %s", add.code, add.body)
	}
	item := decodeJSON[struct {
		ID   string `json:"id"`
		Done bool   `json:"done"`
	}](t, add)
	if res := rig.do(t, f.owner, http.MethodPost, listPath("/items"), map[string]string{"text": "  "}); res.code != http.StatusBadRequest {
		t.Fatalf("blank item: %d", res.code)
	}
	if res := rig.do(t, f.owner, http.MethodPatch, listPath("/items/"+item.ID), map[string]bool{"done": true}); res.code != http.StatusOK {
		t.Fatalf("tick: %d %s", res.code, res.body)
	}

	// Access is the node's: nothing, then view (reads, no writes), then edit.
	if res := rig.do(t, f.member, http.MethodGet, listPath("/"), nil); res.code != http.StatusForbidden && res.code != http.StatusNotFound {
		t.Fatalf("ungranted read: %d", res.code)
	}
	f.grant(t, list, f.member, share.LevelView)
	if res := rig.do(t, f.member, http.MethodGet, listPath("/"), nil); res.code != http.StatusOK {
		t.Fatalf("view read: %d", res.code)
	}
	if res := rig.do(t, f.member, http.MethodPatch, listPath("/items/"+item.ID), map[string]bool{"done": false}); res.code != http.StatusForbidden {
		t.Fatalf("view-only write: %d", res.code)
	}

	// A copy carries the items, ticked or not, and is independent.
	res := rig.do(t, f.owner, http.MethodPost, "/api/v1/nodes/"+list.ID+"/copy", map[string]string{})
	if res.code != http.StatusCreated && res.code != http.StatusOK {
		t.Fatalf("copy: %d %s", res.code, res.body)
	}
	cp := decodeJSON[nodeDTO](t, res)
	count := func(id string) int {
		var n int
		if err := rig.srv.store.DB().QueryRow(context.Background(),
			"SELECT count(*) FROM ext_checklist.items WHERE list_id = $1", id).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if count(cp.ID) != 1 || count(list.ID) != 1 {
		t.Fatalf("copy rows: original %d, copy %d", count(list.ID), count(cp.ID))
	}

	// Switching the extension off for the space stops its routes, and only its own.
	if res := rig.do(t, f.admin, http.MethodPatch, "/api/v1/admin/extensions/checklist", map[string]bool{"enabled": false}); res.code != http.StatusOK {
		t.Fatalf("disable: %d", res.code)
	}
	if res := rig.do(t, f.owner, http.MethodGet, listPath("/"), nil); res.code != http.StatusForbidden {
		t.Fatalf("switched off: %d", res.code)
	}
	if res := rig.do(t, f.owner, http.MethodGet, boardPath(board, "/"), nil); res.code != http.StatusOK {
		t.Fatalf("kanban went off with checklist: %d", res.code)
	}

	for _, id := range []string{list.ID} {
		if res := rig.do(t, f.owner, http.MethodDelete, "/api/v1/nodes/"+id, nil); res.code >= 300 {
			t.Fatalf("trash: %d", res.code)
		}
		if res := rig.do(t, f.owner, http.MethodDelete, "/api/v1/nodes/"+id+"/purge", nil); res.code >= 300 {
			t.Fatalf("purge: %d %s", res.code, res.body)
		}
	}
	if n := count(list.ID); n != 0 {
		t.Fatalf("items survived the purge: %d", n)
	}
	if count(cp.ID) != 1 {
		t.Fatal("the purge reached the copy")
	}
}

// An app's nodes are listed under Apps and nowhere in the drive: not at the top
// level, not in a folder, not in "Shared with me". Who sees one is still the
// node's own access, wherever it hangs.
func TestExtNodesLiveUnderApps(t *testing.T) {
	f := newExtFixture(t, "apps")
	rig := f.rig
	f.enable(t)
	apps := func(u store.User, ext string) ([]nodeDTO, int) {
		res := rig.do(t, u, http.MethodGet, f.spacePath("/extensions/"+ext+"/nodes"), nil)
		if res.code != http.StatusOK {
			return nil, res.code
		}
		return decodeJSON[[]nodeDTO](t, res), res.code
	}

	board := f.newBoard(t, "Plan")
	if got, code := apps(f.owner, "kanban"); code != http.StatusOK || len(got) != 1 || got[0].ID != board.ID || got[0].Caps == nil || !got[0].Caps.Edit {
		t.Fatalf("owner apps list: %d %+v", code, got)
	}
	// Private until shared: a member with no grant sees nothing.
	if got, code := apps(f.member, "kanban"); code != http.StatusOK || len(got) != 0 {
		t.Fatalf("ungranted member apps list: %d %+v", code, got)
	}
	f.grant(t, board, f.member, share.LevelView)
	if got, _ := apps(f.member, "kanban"); len(got) != 1 || got[0].Caps == nil || got[0].Caps.Edit {
		t.Fatalf("granted member apps list: %+v", got)
	}

	// Not in the drive, however the drive is asked.
	for _, who := range []store.User{f.owner, f.member} {
		if kids := decodeJSON[[]nodeDTO](t, rig.do(t, who, http.MethodGet, f.spacePath("/children"), nil)); len(kids) != 0 {
			t.Fatalf("top level lists an app node: %+v", kids)
		}
	}
	if kids := decodeJSON[[]nodeDTO](t, rig.do(t, f.member, http.MethodGet, "/api/v1/shared?space="+uuidString(f.space.ID), nil)); len(kids) != 0 {
		t.Fatalf("shared with me lists an app node: %+v", kids)
	}

	// A board inside a folder is hidden from that folder and found under Apps
	// by those the folder reaches.
	folder := decodeJSON[nodeDTO](t, rig.do(t, f.owner, http.MethodPost, f.spacePath("/dirs"), map[string]string{"name": "Projects"}))
	res := rig.do(t, f.owner, http.MethodPost, f.spacePath("/ext-nodes"),
		map[string]string{"ext_kind": "kanban.board", "name": "In a folder", "parent_id": folder.ID})
	if res.code != http.StatusCreated {
		t.Fatalf("create in folder: %d %s", res.code, res.body)
	}
	inFolder := decodeJSON[nodeDTO](t, res)
	if kids := decodeJSON[[]nodeDTO](t, rig.do(t, f.owner, http.MethodGet, f.spacePath("/children?parent="+folder.ID), nil)); len(kids) != 0 {
		t.Fatalf("folder lists an app node: %+v", kids)
	}
	if got, _ := apps(f.owner, "kanban"); len(got) != 2 {
		t.Fatalf("owner apps list after folder board: %+v", got)
	}
	if got, _ := apps(f.member, "kanban"); len(got) != 1 {
		t.Fatalf("member reached a board through nothing: %+v", got)
	}
	f.grant(t, folder, f.member, share.LevelView)
	if got, _ := apps(f.member, "kanban"); len(got) != 2 {
		t.Fatalf("folder grant did not reach its board: %+v", got)
	}
	_ = inFolder

	// Other apps' nodes stay out of this list, strangers and unknown apps get no answer.
	if got, _ := apps(f.owner, "checklist"); got != nil {
		t.Fatalf("checklist list while off: %+v", got)
	}
	if _, code := apps(f.owner, "nope"); code != http.StatusNotFound {
		t.Fatalf("unknown app: %d", code)
	}
	if _, code := apps(f.stranger, "kanban"); code != http.StatusForbidden && code != http.StatusNotFound {
		t.Fatalf("non-member apps list: %d", code)
	}
}
