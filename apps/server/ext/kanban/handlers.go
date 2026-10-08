package kanban

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nhatbui/pp-hub/apps/server/extsdk"
)

// --- wire types ---

type columnDTO struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Position string `json:"position"`
	WipLimit *int32 `json:"wip_limit"`
}

type personDTO struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email,omitempty"`
}

type labelDTO struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
}

type cardDTO struct {
	ID          string      `json:"id"`
	ColumnID    string      `json:"column_id"`
	Title       string      `json:"title"`
	Description string      `json:"description"`
	Position    string      `json:"position"`
	DueAt       *string     `json:"due_at"`
	Rev         int32       `json:"rev"`
	Assignees   []personDTO `json:"assignees"`
	LabelIDs    []string    `json:"label_ids"`
	// CommentCount is how many comments the card's threads hold.
	CommentCount int64 `json:"comment_count"`
	// LinkIDs are the nodes the card points at; Links resolves them for the
	// person asking and is only on REST answers: a broadcast event goes to
	// everybody, and a name resolved for one viewer must not reach another.
	LinkIDs   []string  `json:"link_ids"`
	Links     []linkDTO `json:"links,omitempty"`
	CreatedBy string    `json:"created_by,omitempty"`
	CreatedAt string    `json:"created_at"`
	UpdatedAt string    `json:"updated_at"`
}

// linkDTO is a node a card points at, as one viewer may see it. A node the
// viewer cannot open is Accessible false and carries no name.
type linkDTO struct {
	ID         string `json:"id"`
	Accessible bool   `json:"accessible"`
	Name       string `json:"name,omitempty"`
	// Kind is "dir", "file" or "ext"; ExtKind names an extension node's kind.
	Kind    string `json:"kind,omitempty"`
	ExtKind string `json:"ext_kind,omitempty"`
	SpaceID string `json:"space_id,omitempty"`
}

type boardDTO struct {
	ID      string      `json:"id"`
	Name    string      `json:"name"`
	Caps    extsdk.Caps `json:"caps"`
	Columns []columnDTO `json:"columns"`
	Cards   []cardDTO   `json:"cards"`
	Labels  []labelDTO  `json:"labels"`
}

const timeLayout = "2006-01-02T15:04:05Z07:00"

var colorRe = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// --- plumbing ---

func id(s string) (pgtype.UUID, bool) {
	v, err := extsdk.ParseUUID(s)
	return v, err == nil
}

// publish tells everyone watching the board.
func (s *service) publish(board pgtype.UUID, typ string, payload map[string]any) {
	payload["type"] = typ
	s.h.Realtime().Publish("board:"+extsdk.UUIDString(board), "board", payload)
}

// --- reading ---

func (s *service) loadColumns(ctx context.Context, board pgtype.UUID) ([]columnDTO, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id, title, position, wip_limit FROM ext_kanban.columns
		WHERE board_id = $1 ORDER BY position, id`, board)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []columnDTO{}
	for rows.Next() {
		var c columnDTO
		var cid pgtype.UUID
		if err := rows.Scan(&cid, &c.Title, &c.Position, &c.WipLimit); err != nil {
			return nil, err
		}
		c.ID = extsdk.UUIDString(cid)
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *service) loadLabels(ctx context.Context, board pgtype.UUID) ([]labelDTO, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id, name, color FROM ext_kanban.labels WHERE board_id = $1 ORDER BY name, id`, board)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []labelDTO{}
	for rows.Next() {
		var l labelDTO
		var lid pgtype.UUID
		if err := rows.Scan(&lid, &l.Name, &l.Color); err != nil {
			return nil, err
		}
		l.ID = extsdk.UUIDString(lid)
		out = append(out, l)
	}
	return out, rows.Err()
}

type cardRow struct {
	dto      cardDTO
	id       pgtype.UUID
	createdB pgtype.UUID
}

// loadCards reads the board's cards, or one when only is set, with their
// assignees, labels, links and comment counts. withLinks resolves the links
// for viewer; without it a card carries only the ids.
func (s *service) loadCards(ctx context.Context, board, only, viewer pgtype.UUID, withLinks bool) ([]cardDTO, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id, column_id, title, description_md, position, due_at, rev, created_by, created_at, updated_at
		FROM ext_kanban.cards
		WHERE board_id = $1 AND ($2::uuid IS NULL OR id = $2)
		ORDER BY position, id`, board, only)
	if err != nil {
		return nil, err
	}
	var cards []cardRow
	index := map[pgtype.UUID]int{}
	for rows.Next() {
		var c cardRow
		var colID pgtype.UUID
		var due pgtype.Timestamptz
		var created, updated time.Time
		if err := rows.Scan(&c.id, &colID, &c.dto.Title, &c.dto.Description, &c.dto.Position,
			&due, &c.dto.Rev, &c.createdB, &created, &updated); err != nil {
			rows.Close()
			return nil, err
		}
		c.dto.ID = extsdk.UUIDString(c.id)
		c.dto.ColumnID = extsdk.UUIDString(colID)
		c.dto.CreatedBy = extsdk.UUIDString(c.createdB)
		c.dto.CreatedAt = created.Format(timeLayout)
		c.dto.UpdatedAt = updated.Format(timeLayout)
		if due.Valid {
			s := due.Time.Format(timeLayout)
			c.dto.DueAt = &s
		}
		c.dto.Assignees = []personDTO{}
		c.dto.LabelIDs = []string{}
		c.dto.LinkIDs = []string{}
		index[c.id] = len(cards)
		cards = append(cards, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(cards) == 0 {
		return []cardDTO{}, nil
	}

	// Assignees: ids from the board's card_assignees, names from the host.
	arows, err := s.db.Query(ctx, `
		SELECT a.card_id, a.user_id FROM ext_kanban.card_assignees a
		JOIN ext_kanban.cards c ON c.id = a.card_id
		WHERE c.board_id = $1 AND ($2::uuid IS NULL OR c.id = $2)`, board, only)
	if err != nil {
		return nil, err
	}
	type pair struct{ card, user pgtype.UUID }
	var pairs []pair
	seen := map[pgtype.UUID]bool{}
	var userIDs []pgtype.UUID
	for arows.Next() {
		var p pair
		if err := arows.Scan(&p.card, &p.user); err != nil {
			arows.Close()
			return nil, err
		}
		pairs = append(pairs, p)
		if !seen[p.user] {
			seen[p.user] = true
			userIDs = append(userIDs, p.user)
		}
	}
	arows.Close()
	if err := arows.Err(); err != nil {
		return nil, err
	}
	names, err := s.h.Users(ctx, userIDs)
	if err != nil {
		return nil, err
	}
	for _, p := range pairs {
		i, ok := index[p.card]
		if !ok {
			continue
		}
		u := names[p.user]
		cards[i].dto.Assignees = append(cards[i].dto.Assignees, personDTO{
			ID: extsdk.UUIDString(p.user), Name: u.Name, Email: u.Email,
		})
	}

	lrows, err := s.db.Query(ctx, `
		SELECT cl.card_id, cl.label_id FROM ext_kanban.card_labels cl
		JOIN ext_kanban.cards c ON c.id = cl.card_id
		WHERE c.board_id = $1 AND ($2::uuid IS NULL OR c.id = $2)
		ORDER BY cl.label_id`, board, only)
	if err != nil {
		return nil, err
	}
	for lrows.Next() {
		var cardID, labelID pgtype.UUID
		if err := lrows.Scan(&cardID, &labelID); err != nil {
			lrows.Close()
			return nil, err
		}
		if i, ok := index[cardID]; ok {
			cards[i].dto.LabelIDs = append(cards[i].dto.LabelIDs, extsdk.UUIDString(labelID))
		}
	}
	lrows.Close()
	if err := lrows.Err(); err != nil {
		return nil, err
	}

	krows, err := s.db.Query(ctx, `
		SELECT l.card_id, l.node_id FROM ext_kanban.card_links l
		JOIN ext_kanban.cards c ON c.id = l.card_id
		WHERE c.board_id = $1 AND ($2::uuid IS NULL OR c.id = $2)
		ORDER BY l.node_id`, board, only)
	if err != nil {
		return nil, err
	}
	var linked []pgtype.UUID
	seenNode := map[pgtype.UUID]bool{}
	for krows.Next() {
		var cardID, nodeID pgtype.UUID
		if err := krows.Scan(&cardID, &nodeID); err != nil {
			krows.Close()
			return nil, err
		}
		if i, ok := index[cardID]; ok {
			cards[i].dto.LinkIDs = append(cards[i].dto.LinkIDs, extsdk.UUIDString(nodeID))
		}
		if !seenNode[nodeID] {
			seenNode[nodeID] = true
			linked = append(linked, nodeID)
		}
	}
	krows.Close()
	if err := krows.Err(); err != nil {
		return nil, err
	}
	var targets map[pgtype.UUID]extsdk.LinkTarget
	if withLinks {
		if targets, err = s.h.Resolve(ctx, viewer, linked); err != nil {
			return nil, err
		}
	}

	counts, err := s.h.Comments().Counts(ctx, board)
	if err != nil {
		return nil, err
	}

	out := make([]cardDTO, len(cards))
	for i, c := range cards {
		c.dto.CommentCount = counts[cardSubject(c.id)]
		if withLinks {
			c.dto.Links = make([]linkDTO, 0, len(c.dto.LinkIDs))
			for _, raw := range c.dto.LinkIDs {
				nid, _ := extsdk.ParseUUID(raw)
				tg, ok := targets[nid]
				if !ok {
					c.dto.Links = append(c.dto.Links, linkDTO{ID: raw})
					continue
				}
				c.dto.Links = append(c.dto.Links, linkDTO{
					ID: raw, Accessible: true, Name: tg.Name, Kind: tg.Kind, ExtKind: tg.ExtKind,
					SpaceID: extsdk.UUIDString(tg.SpaceID),
				})
			}
		}
		out[i] = c.dto
	}
	return out, nil
}

func (s *service) handleBoard(w http.ResponseWriter, r *http.Request) {
	q := extsdk.MustRequest(r)
	ctx := r.Context()
	cols, err := s.loadColumns(ctx, q.Node.ID)
	if err != nil {
		extsdk.Fail(w, err)
		return
	}
	cards, err := s.loadCards(ctx, q.Node.ID, pgtype.UUID{}, q.User.ID, true)
	if err != nil {
		extsdk.Fail(w, err)
		return
	}
	labels, err := s.loadLabels(ctx, q.Node.ID)
	if err != nil {
		extsdk.Fail(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	extsdk.WriteJSON(w, http.StatusOK, boardDTO{
		ID: extsdk.UUIDString(q.Node.ID), Name: q.Node.Name, Caps: q.Caps,
		Columns: cols, Cards: cards, Labels: labels,
	})
}

func (s *service) handleEvents(w http.ResponseWriter, r *http.Request) {
	q := extsdk.MustRequest(r)
	s.h.Realtime().Stream(w, r, "board:"+extsdk.UUIDString(q.Node.ID), func(ctx context.Context) bool {
		return s.h.Allowed(ctx, q.Node.ID, q.User.ID, BoardKind, extsdk.CapView)
	})
}

func (s *service) handlePeople(w http.ResponseWriter, r *http.Request) {
	q := extsdk.MustRequest(r)
	people, err := s.h.SearchPeople(r.Context(), q.Node, strings.TrimSpace(r.URL.Query().Get("q")), 20)
	if err != nil {
		extsdk.Fail(w, err)
		return
	}
	out := make([]personDTO, 0, len(people))
	for _, p := range people {
		out = append(out, personDTO{ID: extsdk.UUIDString(p.ID), Name: p.Name, Email: p.Email})
	}
	extsdk.WriteJSON(w, http.StatusOK, out)
}

// --- columns ---

// lastPosition is the greatest position in a column's scope, "" when empty.
func lastPosition(ctx context.Context, tx extsdk.Querier, sql string, arg pgtype.UUID) (string, error) {
	var pos string
	err := tx.QueryRow(ctx, sql, arg).Scan(&pos)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return pos, err
}

func (s *service) handleCreateColumn(w http.ResponseWriter, r *http.Request) {
	q := extsdk.MustRequest(r)
	var body struct {
		Title    string `json:"title"`
		WipLimit *int32 `json:"wip_limit"`
	}
	if !extsdk.DecodeBody(w, r, &body) {
		return
	}
	title, ok := extsdk.Trim(w, body.Title, "title", 200, true)
	if !ok {
		return
	}
	if body.WipLimit != nil && *body.WipLimit < 1 {
		extsdk.WriteProblem(w, http.StatusBadRequest, "wip_limit must be positive")
		return
	}
	ctx := r.Context()
	tx, err := s.db.Begin(ctx)
	if err != nil {
		extsdk.Fail(w, err)
		return
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rolled back unless committed
	// The board row serialises column writes, so two appends never share a key.
	if _, err := tx.Exec(ctx, `SELECT 1 FROM public.nodes WHERE id = $1 FOR UPDATE`, q.Node.ID); err != nil {
		extsdk.Fail(w, err)
		return
	}
	var n int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM ext_kanban.columns WHERE board_id = $1`, q.Node.ID).Scan(&n); err != nil {
		extsdk.Fail(w, err)
		return
	}
	if n >= maxColumns {
		extsdk.Fail(w, extsdk.Conflict("a board holds at most 30 columns"))
		return
	}
	last, err := lastPosition(ctx, tx,
		`SELECT position FROM ext_kanban.columns WHERE board_id = $1 ORDER BY position DESC, id DESC LIMIT 1`, q.Node.ID)
	if err != nil {
		extsdk.Fail(w, err)
		return
	}
	var col columnDTO
	var cid pgtype.UUID
	err = tx.QueryRow(ctx, `
		INSERT INTO ext_kanban.columns (board_id, title, position, wip_limit) VALUES ($1, $2, $3, $4)
		RETURNING id, title, position, wip_limit`, q.Node.ID, title, After(last), body.WipLimit).
		Scan(&cid, &col.Title, &col.Position, &col.WipLimit)
	if err != nil {
		extsdk.Fail(w, err)
		return
	}
	if err := tx.Commit(ctx); err != nil {
		extsdk.Fail(w, err)
		return
	}
	col.ID = extsdk.UUIDString(cid)
	s.publish(q.Node.ID, "column.upsert", map[string]any{"column": col})
	extsdk.WriteJSON(w, http.StatusCreated, col)
}

func (s *service) handleUpdateColumn(w http.ResponseWriter, r *http.Request) {
	q := extsdk.MustRequest(r)
	colID, ok := id(chi.URLParam(r, "columnID"))
	if !ok {
		notFound(w, "column")
		return
	}
	var body struct {
		Title    *string         `json:"title"`
		WipLimit json.RawMessage `json:"wip_limit"`
		// AfterID moves the column: absent leaves it, null puts it first, an
		// id puts it after that column.
		AfterID json.RawMessage `json:"after_id"`
	}
	if !extsdk.DecodeBody(w, r, &body) {
		return
	}
	ctx := r.Context()
	tx, err := s.db.Begin(ctx)
	if err != nil {
		extsdk.Fail(w, err)
		return
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rolled back unless committed
	if _, err := tx.Exec(ctx, `SELECT 1 FROM public.nodes WHERE id = $1 FOR UPDATE`, q.Node.ID); err != nil {
		extsdk.Fail(w, err)
		return
	}
	var cur columnDTO
	var cid pgtype.UUID
	err = tx.QueryRow(ctx, `SELECT id, title, position, wip_limit FROM ext_kanban.columns WHERE id = $1 AND board_id = $2`,
		colID, q.Node.ID).Scan(&cid, &cur.Title, &cur.Position, &cur.WipLimit)
	if err != nil {
		extsdk.Fail(w, err)
		return
	}
	if body.Title != nil {
		t, ok := extsdk.Trim(w, *body.Title, "title", 200, true)
		if !ok {
			return
		}
		cur.Title = t
	}
	if body.WipLimit != nil {
		if string(body.WipLimit) == "null" {
			cur.WipLimit = nil
		} else {
			var n int32
			if err := json.Unmarshal(body.WipLimit, &n); err != nil || n < 1 {
				extsdk.WriteProblem(w, http.StatusBadRequest, "wip_limit must be a positive number or null")
				return
			}
			cur.WipLimit = &n
		}
	}
	rebalanced := false
	if body.AfterID != nil {
		pos, did, err := s.columnPositionAfter(ctx, tx, q.Node.ID, colID, body.AfterID)
		if err != nil {
			if errors.Is(err, errBadAnchor) {
				extsdk.WriteProblem(w, http.StatusBadRequest, "after_id is not a column of this board")
				return
			}
			extsdk.Fail(w, err)
			return
		}
		cur.Position, rebalanced = pos, did
	}
	if _, err := tx.Exec(ctx, `UPDATE ext_kanban.columns SET title = $2, wip_limit = $3, position = $4 WHERE id = $1`,
		colID, cur.Title, cur.WipLimit, cur.Position); err != nil {
		extsdk.Fail(w, err)
		return
	}
	if err := tx.Commit(ctx); err != nil {
		extsdk.Fail(w, err)
		return
	}
	cur.ID = extsdk.UUIDString(cid)
	if rebalanced {
		s.publish(q.Node.ID, "reload", map[string]any{})
	} else {
		s.publish(q.Node.ID, "column.upsert", map[string]any{"column": cur})
	}
	extsdk.WriteJSON(w, http.StatusOK, cur)
}

var errBadAnchor = errors.New("anchor is not on this board")

// columnPositionAfter finds the position for column moving after the column
// named by raw (null = first). A key grown past MaxPositionLen renumbers the
// whole board, reported by the bool; the caller then sends a reload.
func (s *service) columnPositionAfter(ctx context.Context, tx pgx.Tx, board, moving pgtype.UUID, raw json.RawMessage) (string, bool, error) {
	var after pgtype.UUID
	if string(raw) != "null" {
		var text string
		if err := json.Unmarshal(raw, &text); err != nil {
			return "", false, errBadAnchor
		}
		v, ok := id(text)
		if !ok {
			return "", false, errBadAnchor
		}
		after = v
	}
	var prev, next string
	if after.Valid {
		err := tx.QueryRow(ctx, `SELECT position FROM ext_kanban.columns WHERE id = $1 AND board_id = $2 AND id <> $3`,
			after, board, moving).Scan(&prev)
		if errors.Is(err, pgx.ErrNoRows) {
			return "", false, errBadAnchor
		}
		if err != nil {
			return "", false, err
		}
	}
	err := tx.QueryRow(ctx, `
		SELECT position FROM ext_kanban.columns
		WHERE board_id = $1 AND id <> $2 AND position COLLATE "C" > $3
		ORDER BY position, id LIMIT 1`, board, moving, prev).Scan(&next)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return "", false, err
	}
	pos := Between(prev, next)
	if len(pos) <= MaxPositionLen {
		return pos, false, nil
	}
	return s.renumberColumns(ctx, tx, board, moving, after)
}

// renumberColumns spreads the board's columns and returns the key for moving,
// placed after `after` (invalid = first).
func (s *service) renumberColumns(ctx context.Context, tx pgx.Tx, board, moving, after pgtype.UUID) (string, bool, error) {
	rows, err := tx.Query(ctx, `SELECT id FROM ext_kanban.columns WHERE board_id = $1 AND id <> $2 ORDER BY position, id`, board, moving)
	if err != nil {
		return "", false, err
	}
	var order []pgtype.UUID
	for rows.Next() {
		var c pgtype.UUID
		if err := rows.Scan(&c); err != nil {
			rows.Close()
			return "", false, err
		}
		order = append(order, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return "", false, err
	}
	at := 0
	for i, c := range order {
		if c == after {
			at = i + 1
		}
	}
	order = append(order[:at], append([]pgtype.UUID{moving}, order[at:]...)...)
	keys := Spread(len(order))
	var movingKey string
	for i, c := range order {
		if c == moving {
			movingKey = keys[i]
			continue
		}
		if _, err := tx.Exec(ctx, `UPDATE ext_kanban.columns SET position = $2 WHERE id = $1`, c, keys[i]); err != nil {
			return "", false, err
		}
	}
	return movingKey, true, nil
}

func (s *service) handleDeleteColumn(w http.ResponseWriter, r *http.Request) {
	q := extsdk.MustRequest(r)
	colID, ok := id(chi.URLParam(r, "columnID"))
	if !ok {
		notFound(w, "column")
		return
	}
	var moveTo pgtype.UUID
	if raw := r.URL.Query().Get("move_to"); raw != "" {
		v, ok := id(raw)
		if !ok || v == colID {
			extsdk.WriteProblem(w, http.StatusBadRequest, "move_to must be another column of this board")
			return
		}
		moveTo = v
	}
	ctx := r.Context()
	tx, err := s.db.Begin(ctx)
	if err != nil {
		extsdk.Fail(w, err)
		return
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rolled back unless committed
	if _, err := tx.Exec(ctx, `SELECT 1 FROM public.nodes WHERE id = $1 FOR UPDATE`, q.Node.ID); err != nil {
		extsdk.Fail(w, err)
		return
	}
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM ext_kanban.columns WHERE id = $1 AND board_id = $2)`,
		colID, q.Node.ID).Scan(&exists); err != nil {
		extsdk.Fail(w, err)
		return
	}
	if !exists {
		notFound(w, "column")
		return
	}
	var cards int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM ext_kanban.cards WHERE column_id = $1`, colID).Scan(&cards); err != nil {
		extsdk.Fail(w, err)
		return
	}
	if cards > 0 {
		if !moveTo.Valid {
			extsdk.Fail(w, extsdk.Conflict("the column still holds cards; move them with move_to"))
			return
		}
		var ok bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM ext_kanban.columns WHERE id = $1 AND board_id = $2)`,
			moveTo, q.Node.ID).Scan(&ok); err != nil {
			extsdk.Fail(w, err)
			return
		}
		if !ok {
			extsdk.WriteProblem(w, http.StatusBadRequest, "move_to must be another column of this board")
			return
		}
		if err := s.appendCards(ctx, tx, colID, moveTo); err != nil {
			extsdk.Fail(w, err)
			return
		}
	}
	if _, err := tx.Exec(ctx, `DELETE FROM ext_kanban.columns WHERE id = $1`, colID); err != nil {
		extsdk.Fail(w, err)
		return
	}
	if err := tx.Commit(ctx); err != nil {
		extsdk.Fail(w, err)
		return
	}
	s.publish(q.Node.ID, "reload", map[string]any{})
	w.WriteHeader(http.StatusNoContent)
}

// appendCards moves every card of from to the end of to, keeping their order.
func (s *service) appendCards(ctx context.Context, tx pgx.Tx, from, to pgtype.UUID) error {
	if _, err := tx.Exec(ctx, `SELECT 1 FROM ext_kanban.columns WHERE id = $1 FOR UPDATE`, to); err != nil {
		return err
	}
	last, err := lastPosition(ctx, tx,
		`SELECT position FROM ext_kanban.cards WHERE column_id = $1 ORDER BY position DESC, id DESC LIMIT 1`, to)
	if err != nil {
		return err
	}
	rows, err := tx.Query(ctx, `SELECT id FROM ext_kanban.cards WHERE column_id = $1 ORDER BY position, id`, from)
	if err != nil {
		return err
	}
	var ids []pgtype.UUID
	for rows.Next() {
		var c pgtype.UUID
		if err := rows.Scan(&c); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, c := range ids {
		last = After(last)
		if _, err := tx.Exec(ctx, `
			UPDATE ext_kanban.cards SET column_id = $2, position = $3, rev = rev + 1, updated_at = now()
			WHERE id = $1`, c, to, last); err != nil {
			return err
		}
	}
	return nil
}

// --- cards ---

type cardInput struct {
	ColumnID    string          `json:"column_id"`
	Title       *string         `json:"title"`
	Description *string         `json:"description"`
	DueAt       json.RawMessage `json:"due_at"`
	Assignees   *[]string       `json:"assignees"`
	Labels      *[]string       `json:"labels"`
	Links       *[]string       `json:"links"`
	IfRev       *int32          `json:"if_rev"`
}

// parseDue reads due_at: absent is "no change", null clears, a string sets.
func parseDue(raw json.RawMessage) (set bool, v pgtype.Timestamptz, err error) {
	if raw == nil {
		return false, v, nil
	}
	if string(raw) == "null" {
		return true, v, nil
	}
	var text string
	if err := json.Unmarshal(raw, &text); err != nil {
		return false, v, errors.New("due_at must be a timestamp or null")
	}
	t, err := time.Parse(time.RFC3339, text)
	if err != nil {
		return false, v, errors.New("due_at must be an RFC 3339 timestamp")
	}
	return true, pgtype.Timestamptz{Time: t, Valid: true}, nil
}

func parseIDs(w http.ResponseWriter, raw []string, field string) ([]pgtype.UUID, bool) {
	out := make([]pgtype.UUID, 0, len(raw))
	seen := map[pgtype.UUID]bool{}
	for _, s := range raw {
		v, ok := id(s)
		if !ok {
			extsdk.WriteProblem(w, http.StatusBadRequest, "invalid id in "+field)
			return nil, false
		}
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	if len(out) > 50 {
		extsdk.WriteProblem(w, http.StatusBadRequest, field+" holds too many entries")
		return nil, false
	}
	return out, true
}

// checkPeople refuses an assignee who could not open the board: an assignment
// is not a way to hand someone a card they may not see.
func (s *service) checkPeople(ctx context.Context, node extsdk.Node, users []pgtype.UUID) error {
	for _, u := range users {
		c, err := s.h.Caps(ctx, node, u)
		if err != nil {
			return err
		}
		if !c.View {
			return extsdk.Conflict("an assignee cannot open this board")
		}
	}
	return nil
}

// parseRelations reads the three id sets of a card write.
func parseRelations(w http.ResponseWriter, body cardInput) (relations, bool) {
	var rel relations
	for _, f := range []struct {
		in   *[]string
		name string
		to   **[]pgtype.UUID
	}{
		{body.Assignees, "assignees", &rel.assignees},
		{body.Labels, "labels", &rel.labels},
		{body.Links, "links", &rel.links},
	} {
		if f.in == nil {
			continue
		}
		ids, ok := parseIDs(w, *f.in, f.name)
		if !ok {
			return relations{}, false
		}
		*f.to = &ids
	}
	return rel, true
}

// checkRelations refuses assignees and links the writer may not give.
func (s *service) checkRelations(ctx context.Context, q extsdk.Request, rel relations) error {
	if rel.assignees != nil {
		if err := s.checkPeople(ctx, q.Node, *rel.assignees); err != nil {
			return err
		}
	}
	if rel.links != nil {
		return s.checkLinks(ctx, q, *rel.links)
	}
	return nil
}

// relations are the sets a card write may replace; nil leaves a set alone.
type relations struct {
	assignees, labels, links *[]pgtype.UUID
}

// setRelations replaces the sets rel names and returns the people newly
// assigned, for the notification they are owed.
func (s *service) setRelations(ctx context.Context, tx pgx.Tx, board, card pgtype.UUID, rel relations) ([]pgtype.UUID, error) {
	assignees, labels, links := rel.assignees, rel.labels, rel.links
	var added []pgtype.UUID
	if assignees != nil {
		old := map[pgtype.UUID]bool{}
		orows, err := tx.Query(ctx, `SELECT user_id FROM ext_kanban.card_assignees WHERE card_id = $1`, card)
		if err != nil {
			return nil, err
		}
		for orows.Next() {
			var u pgtype.UUID
			if err := orows.Scan(&u); err != nil {
				orows.Close()
				return nil, err
			}
			old[u] = true
		}
		orows.Close()
		if err := orows.Err(); err != nil {
			return nil, err
		}
		for _, u := range *assignees {
			if !old[u] {
				added = append(added, u)
			}
		}
	}
	if assignees != nil {
		if _, err := tx.Exec(ctx, `DELETE FROM ext_kanban.card_assignees WHERE card_id = $1`, card); err != nil {
			return nil, err
		}
		for _, u := range *assignees {
			if _, err := tx.Exec(ctx, `INSERT INTO ext_kanban.card_assignees (card_id, user_id) VALUES ($1, $2)`, card, u); err != nil {
				return nil, err
			}
		}
	}
	if links != nil {
		if _, err := tx.Exec(ctx, `DELETE FROM ext_kanban.card_links WHERE card_id = $1`, card); err != nil {
			return nil, err
		}
		for _, n := range *links {
			if _, err := tx.Exec(ctx, `INSERT INTO ext_kanban.card_links (card_id, node_id) VALUES ($1, $2)`, card, n); err != nil {
				return nil, err
			}
		}
	}
	if labels != nil {
		if _, err := tx.Exec(ctx, `DELETE FROM ext_kanban.card_labels WHERE card_id = $1`, card); err != nil {
			return nil, err
		}
		for _, l := range *labels {
			tag, err := tx.Exec(ctx, `
				INSERT INTO ext_kanban.card_labels (card_id, label_id)
				SELECT $1, id FROM ext_kanban.labels WHERE id = $2 AND board_id = $3`, card, l, board)
			if err != nil {
				return nil, err
			}
			if tag.RowsAffected() == 0 {
				return nil, extsdk.Conflict("a label does not belong to this board")
			}
		}
	}
	return added, nil
}

// checkLinks refuses a link the person writing it could not follow: a node
// that is gone, in another space, or that they may not open.
func (s *service) checkLinks(ctx context.Context, q extsdk.Request, links []pgtype.UUID) error {
	if len(links) > 20 {
		return extsdk.Conflict("a card links to at most 20 items")
	}
	found, err := s.h.Resolve(ctx, q.User.ID, links)
	if err != nil {
		return err
	}
	for _, l := range links {
		t, ok := found[l]
		if !ok || t.SpaceID != q.Node.SpaceID || l == q.Node.ID {
			return extsdk.Conflict("a linked item is not available")
		}
	}
	return nil
}

// notifyAssigned tells the people newly put on a card. Best effort: the card
// is saved whatever happens here.
func (s *service) notifyAssigned(ctx context.Context, q extsdk.Request, card pgtype.UUID, title string, added []pgtype.UUID) {
	for _, u := range added {
		if err := s.h.Notify().Send(ctx, extsdk.Notification{
			To: u, Actor: q.User.ID, Node: q.Node, Verb: "assigned",
			Subject: cardSubject(card), Excerpt: title,
		}); err != nil {
			slog.WarnContext(ctx, "kanban: notify assignee", "err", err)
		}
	}
}

func (s *service) handleCreateCard(w http.ResponseWriter, r *http.Request) {
	q := extsdk.MustRequest(r)
	var body cardInput
	if !extsdk.DecodeBody(w, r, &body) {
		return
	}
	colID, ok := id(body.ColumnID)
	if !ok {
		extsdk.WriteProblem(w, http.StatusBadRequest, "column_id is required")
		return
	}
	title := ""
	if body.Title != nil {
		title = *body.Title
	}
	title, ok = extsdk.Trim(w, title, "title", 500, true)
	if !ok {
		return
	}
	desc := ""
	if body.Description != nil {
		desc = *body.Description
		if utf8.RuneCountInString(desc) > 20000 {
			extsdk.WriteProblem(w, http.StatusBadRequest, "description is too long")
			return
		}
	}
	_, due, err := parseDue(body.DueAt)
	if err != nil {
		extsdk.WriteProblem(w, http.StatusBadRequest, err.Error())
		return
	}
	rel, relOK := parseRelations(w, body)
	if !relOK {
		return
	}
	ctx := r.Context()
	if err := s.checkRelations(ctx, q, rel); err != nil {
		extsdk.Fail(w, err)
		return
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		extsdk.Fail(w, err)
		return
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rolled back unless committed
	// Locking the column serialises appends to it.
	var colExists bool
	if err := tx.QueryRow(ctx, `
		SELECT true FROM ext_kanban.columns WHERE id = $1 AND board_id = $2 FOR UPDATE`, colID, q.Node.ID).Scan(&colExists); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			extsdk.WriteProblem(w, http.StatusBadRequest, "column_id is not a column of this board")
			return
		}
		extsdk.Fail(w, err)
		return
	}
	var total int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM ext_kanban.cards WHERE board_id = $1`, q.Node.ID).Scan(&total); err != nil {
		extsdk.Fail(w, err)
		return
	}
	if total >= maxCards {
		extsdk.Fail(w, extsdk.Conflict("a board holds at most 5000 cards"))
		return
	}
	last, err := lastPosition(ctx, tx,
		`SELECT position FROM ext_kanban.cards WHERE column_id = $1 ORDER BY position DESC, id DESC LIMIT 1`, colID)
	if err != nil {
		extsdk.Fail(w, err)
		return
	}
	var cardID pgtype.UUID
	if err := tx.QueryRow(ctx, `
		INSERT INTO ext_kanban.cards (board_id, column_id, title, description_md, position, due_at, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id`,
		q.Node.ID, colID, title, desc, After(last), due, q.User.ID).Scan(&cardID); err != nil {
		extsdk.Fail(w, err)
		return
	}
	added, err := s.setRelations(ctx, tx, q.Node.ID, cardID, rel)
	if err != nil {
		extsdk.Fail(w, err)
		return
	}
	if err := tx.Commit(ctx); err != nil {
		extsdk.Fail(w, err)
		return
	}
	s.notifyAssigned(ctx, q, cardID, title, added)
	s.respondCard(w, r, q, cardID, http.StatusCreated)
}

// respondCard publishes the card's current state and answers it.
func (s *service) respondCard(w http.ResponseWriter, r *http.Request, q extsdk.Request, card pgtype.UUID, status int) {
	cards, err := s.loadCards(r.Context(), q.Node.ID, card, q.User.ID, true)
	if err != nil || len(cards) != 1 {
		extsdk.Fail(w, errOr(err, pgx.ErrNoRows))
		return
	}
	// The broadcast carries no resolved links (see cardDTO.Links).
	public := cards[0]
	public.Links = nil
	s.publish(q.Node.ID, "card.upsert", map[string]any{"card": public})
	extsdk.WriteJSON(w, status, cards[0])
}

func errOr(err, fallback error) error {
	if err != nil {
		return err
	}
	return fallback
}

func (s *service) handleUpdateCard(w http.ResponseWriter, r *http.Request) {
	q := extsdk.MustRequest(r)
	cardID, ok := id(chi.URLParam(r, "cardID"))
	if !ok {
		notFound(w, "card")
		return
	}
	var body cardInput
	if !extsdk.DecodeBody(w, r, &body) {
		return
	}
	if body.IfRev == nil {
		extsdk.WriteProblem(w, http.StatusBadRequest, "if_rev is required")
		return
	}
	var title, desc *string
	if body.Title != nil {
		t, ok := extsdk.Trim(w, *body.Title, "title", 500, true)
		if !ok {
			return
		}
		title = &t
	}
	if body.Description != nil {
		if utf8.RuneCountInString(*body.Description) > 20000 {
			extsdk.WriteProblem(w, http.StatusBadRequest, "description is too long")
			return
		}
		desc = body.Description
	}
	dueSet, due, err := parseDue(body.DueAt)
	if err != nil {
		extsdk.WriteProblem(w, http.StatusBadRequest, err.Error())
		return
	}
	rel, relOK := parseRelations(w, body)
	if !relOK {
		return
	}
	ctx := r.Context()
	if err := s.checkRelations(ctx, q, rel); err != nil {
		extsdk.Fail(w, err)
		return
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		extsdk.Fail(w, err)
		return
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rolled back unless committed
	tag, err := tx.Exec(ctx, `
		UPDATE ext_kanban.cards SET
			title = COALESCE($4, title),
			description_md = COALESCE($5, description_md),
			due_at = CASE WHEN $6::bool THEN $7 ELSE due_at END,
			rev = rev + 1, updated_at = now()
		WHERE id = $1 AND board_id = $2 AND rev = $3`,
		cardID, q.Node.ID, *body.IfRev, title, desc, dueSet, due)
	if err != nil {
		extsdk.Fail(w, err)
		return
	}
	if tag.RowsAffected() == 0 {
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM ext_kanban.cards WHERE id = $1 AND board_id = $2)`,
			cardID, q.Node.ID).Scan(&exists); err != nil {
			extsdk.Fail(w, err)
			return
		}
		if !exists {
			notFound(w, "card")
			return
		}
		extsdk.Fail(w, extsdk.Conflict("the card changed since you read it"))
		return
	}
	added, err := s.setRelations(ctx, tx, q.Node.ID, cardID, rel)
	if err != nil {
		extsdk.Fail(w, err)
		return
	}
	var cardTitle string
	if err := tx.QueryRow(ctx, `SELECT title FROM ext_kanban.cards WHERE id = $1`, cardID).Scan(&cardTitle); err != nil {
		extsdk.Fail(w, err)
		return
	}
	if err := tx.Commit(ctx); err != nil {
		extsdk.Fail(w, err)
		return
	}
	s.notifyAssigned(ctx, q, cardID, cardTitle, added)
	s.respondCard(w, r, q, cardID, http.StatusOK)
}

func (s *service) handleMoveCard(w http.ResponseWriter, r *http.Request) {
	q := extsdk.MustRequest(r)
	cardID, ok := id(chi.URLParam(r, "cardID"))
	if !ok {
		notFound(w, "card")
		return
	}
	var body struct {
		ColumnID string `json:"column_id"`
		// AfterID is the card to land under; empty puts it at the top.
		AfterID string `json:"after_id"`
	}
	if !extsdk.DecodeBody(w, r, &body) {
		return
	}
	colID, ok := id(body.ColumnID)
	if !ok {
		extsdk.WriteProblem(w, http.StatusBadRequest, "column_id is required")
		return
	}
	var after pgtype.UUID
	if body.AfterID != "" {
		v, ok := id(body.AfterID)
		if !ok || v == cardID {
			extsdk.WriteProblem(w, http.StatusBadRequest, "after_id must be another card")
			return
		}
		after = v
	}
	ctx := r.Context()
	tx, err := s.db.Begin(ctx)
	if err != nil {
		extsdk.Fail(w, err)
		return
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rolled back unless committed
	if err := tx.QueryRow(ctx, `
		SELECT true FROM ext_kanban.columns WHERE id = $1 AND board_id = $2 FOR UPDATE`, colID, q.Node.ID).Scan(&ok); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			extsdk.WriteProblem(w, http.StatusBadRequest, "column_id is not a column of this board")
			return
		}
		extsdk.Fail(w, err)
		return
	}
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM ext_kanban.cards WHERE id = $1 AND board_id = $2)`,
		cardID, q.Node.ID).Scan(&exists); err != nil {
		extsdk.Fail(w, err)
		return
	}
	if !exists {
		notFound(w, "card")
		return
	}
	pos, renumbered, err := s.cardPositionAfter(ctx, tx, colID, cardID, after)
	if err != nil {
		if errors.Is(err, errBadAnchor) {
			extsdk.WriteProblem(w, http.StatusBadRequest, "after_id is not a card of that column")
			return
		}
		extsdk.Fail(w, err)
		return
	}
	if _, err := tx.Exec(ctx, `
		UPDATE ext_kanban.cards SET column_id = $2, position = $3, rev = rev + 1, updated_at = now()
		WHERE id = $1`, cardID, colID, pos); err != nil {
		extsdk.Fail(w, err)
		return
	}
	if err := tx.Commit(ctx); err != nil {
		extsdk.Fail(w, err)
		return
	}
	if renumbered {
		s.publish(q.Node.ID, "reload", map[string]any{})
		cards, err := s.loadCards(ctx, q.Node.ID, cardID, q.User.ID, true)
		if err != nil || len(cards) != 1 {
			extsdk.Fail(w, errOr(err, pgx.ErrNoRows))
			return
		}
		extsdk.WriteJSON(w, http.StatusOK, cards[0])
		return
	}
	s.respondCard(w, r, q, cardID, http.StatusOK)
}

// cardPositionAfter finds the key for card moving into column col after the card
// `after` (invalid = the top). The column row is locked by the caller. A key
// grown past MaxPositionLen renumbers the column, reported by the bool.
func (s *service) cardPositionAfter(ctx context.Context, tx pgx.Tx, col, moving, after pgtype.UUID) (string, bool, error) {
	var prev string
	if after.Valid {
		err := tx.QueryRow(ctx, `SELECT position FROM ext_kanban.cards WHERE id = $1 AND column_id = $2`,
			after, col).Scan(&prev)
		if errors.Is(err, pgx.ErrNoRows) {
			return "", false, errBadAnchor
		}
		if err != nil {
			return "", false, err
		}
	}
	var next string
	err := tx.QueryRow(ctx, `
		SELECT position FROM ext_kanban.cards
		WHERE column_id = $1 AND id <> $2 AND position COLLATE "C" > $3
		ORDER BY position, id LIMIT 1`, col, moving, prev).Scan(&next)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return "", false, err
	}
	pos := Between(prev, next)
	if len(pos) <= MaxPositionLen {
		return pos, false, nil
	}
	rows, err := tx.Query(ctx, `SELECT id FROM ext_kanban.cards WHERE column_id = $1 AND id <> $2 ORDER BY position, id`, col, moving)
	if err != nil {
		return "", false, err
	}
	var order []pgtype.UUID
	for rows.Next() {
		var c pgtype.UUID
		if err := rows.Scan(&c); err != nil {
			rows.Close()
			return "", false, err
		}
		order = append(order, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return "", false, err
	}
	at := 0
	for i, c := range order {
		if c == after {
			at = i + 1
		}
	}
	order = append(order[:at], append([]pgtype.UUID{moving}, order[at:]...)...)
	keys := Spread(len(order))
	var movingKey string
	for i, c := range order {
		if c == moving {
			movingKey = keys[i]
			continue
		}
		if _, err := tx.Exec(ctx, `UPDATE ext_kanban.cards SET position = $2, rev = rev + 1 WHERE id = $1`, c, keys[i]); err != nil {
			return "", false, err
		}
	}
	return movingKey, true, nil
}

func (s *service) handleDeleteCard(w http.ResponseWriter, r *http.Request) {
	q := extsdk.MustRequest(r)
	cardID, ok := id(chi.URLParam(r, "cardID"))
	if !ok {
		notFound(w, "card")
		return
	}
	tag, err := s.db.Exec(r.Context(), `DELETE FROM ext_kanban.cards WHERE id = $1 AND board_id = $2`, cardID, q.Node.ID)
	if err != nil {
		extsdk.Fail(w, err)
		return
	}
	if tag.RowsAffected() == 0 {
		notFound(w, "card")
		return
	}
	if err := s.h.Comments().Forget(r.Context(), q.Node.ID, cardSubject(cardID)); err != nil {
		slog.WarnContext(r.Context(), "kanban: forget card discussion", "err", err)
	}
	s.publish(q.Node.ID, "card.delete", map[string]any{"id": extsdk.UUIDString(cardID)})
	w.WriteHeader(http.StatusNoContent)
}

// --- labels ---

func (s *service) handleCreateLabel(w http.ResponseWriter, r *http.Request) {
	q := extsdk.MustRequest(r)
	var body struct {
		Name  string `json:"name"`
		Color string `json:"color"`
	}
	if !extsdk.DecodeBody(w, r, &body) {
		return
	}
	name, ok := extsdk.Trim(w, body.Name, "name", 60, true)
	if !ok {
		return
	}
	if !colorRe.MatchString(body.Color) {
		extsdk.WriteProblem(w, http.StatusBadRequest, "color must be #rrggbb")
		return
	}
	ctx := r.Context()
	var n int
	if err := s.db.QueryRow(ctx, `SELECT count(*) FROM ext_kanban.labels WHERE board_id = $1`, q.Node.ID).Scan(&n); err != nil {
		extsdk.Fail(w, err)
		return
	}
	if n >= maxLabels {
		extsdk.Fail(w, extsdk.Conflict("a board holds at most 50 labels"))
		return
	}
	var l labelDTO
	var lid pgtype.UUID
	err := s.db.QueryRow(ctx, `
		INSERT INTO ext_kanban.labels (board_id, name, color) VALUES ($1, $2, $3)
		ON CONFLICT (board_id, name) DO NOTHING
		RETURNING id, name, color`, q.Node.ID, name, body.Color).Scan(&lid, &l.Name, &l.Color)
	if errors.Is(err, pgx.ErrNoRows) {
		extsdk.Fail(w, extsdk.Conflict("a label with this name exists"))
		return
	}
	if err != nil {
		extsdk.Fail(w, err)
		return
	}
	l.ID = extsdk.UUIDString(lid)
	s.publish(q.Node.ID, "label.upsert", map[string]any{"label": l})
	extsdk.WriteJSON(w, http.StatusCreated, l)
}

func (s *service) handleUpdateLabel(w http.ResponseWriter, r *http.Request) {
	q := extsdk.MustRequest(r)
	labelID, ok := id(chi.URLParam(r, "labelID"))
	if !ok {
		notFound(w, "label")
		return
	}
	var body struct {
		Name  *string `json:"name"`
		Color *string `json:"color"`
	}
	if !extsdk.DecodeBody(w, r, &body) {
		return
	}
	var name *string
	if body.Name != nil {
		n, ok := extsdk.Trim(w, *body.Name, "name", 60, true)
		if !ok {
			return
		}
		name = &n
	}
	if body.Color != nil && !colorRe.MatchString(*body.Color) {
		extsdk.WriteProblem(w, http.StatusBadRequest, "color must be #rrggbb")
		return
	}
	var l labelDTO
	var lid pgtype.UUID
	err := s.db.QueryRow(r.Context(), `
		UPDATE ext_kanban.labels SET name = COALESCE($3, name), color = COALESCE($4, color)
		WHERE id = $1 AND board_id = $2 RETURNING id, name, color`,
		labelID, q.Node.ID, name, body.Color).Scan(&lid, &l.Name, &l.Color)
	if errors.Is(err, pgx.ErrNoRows) {
		notFound(w, "label")
		return
	}
	if err != nil {
		var pg interface{ SQLState() string }
		if errors.As(err, &pg) && pg.SQLState() == "23505" {
			extsdk.Fail(w, extsdk.Conflict("a label with this name exists"))
			return
		}
		extsdk.Fail(w, err)
		return
	}
	l.ID = extsdk.UUIDString(lid)
	s.publish(q.Node.ID, "label.upsert", map[string]any{"label": l})
	extsdk.WriteJSON(w, http.StatusOK, l)
}

func (s *service) handleDeleteLabel(w http.ResponseWriter, r *http.Request) {
	q := extsdk.MustRequest(r)
	labelID, ok := id(chi.URLParam(r, "labelID"))
	if !ok {
		notFound(w, "label")
		return
	}
	tag, err := s.db.Exec(r.Context(), `DELETE FROM ext_kanban.labels WHERE id = $1 AND board_id = $2`, labelID, q.Node.ID)
	if err != nil {
		extsdk.Fail(w, err)
		return
	}
	if tag.RowsAffected() == 0 {
		notFound(w, "label")
		return
	}
	s.publish(q.Node.ID, "label.delete", map[string]any{"id": extsdk.UUIDString(labelID)})
	w.WriteHeader(http.StatusNoContent)
}
