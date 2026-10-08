// Package kanban is pp-hub's first extension: a kanban board for project
// management. A board is a node in the drive (kind "kanban.board"), so sharing,
// trash, search and activity are the drive's own; its columns, cards and labels
// live in the Postgres schema ext_kanban. It uses nothing but extsdk.
//
// Design in docs/architecture/extensions.md.
package kanban

import (
	"context"
	"embed"
	"io/fs"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nhatbui/pp-hub/apps/server/extsdk"
)

// BoardKind is the node kind of a board; pp-app.json declares it too.
const BoardKind = "kanban.board"

// Limits keep one board from becoming an unbounded table scan.
const (
	maxColumns = 30
	maxLabels  = 50
	maxCards   = 5000
)

//go:embed migrations/*.sql
var migrations embed.FS

//go:embed pp-app.json
var manifestJSON []byte

// Ext is the kanban extension.
type Ext struct{}

// New returns the extension for a build to include.
func New() *Ext { return &Ext{} }

var _ extsdk.Ext = (*Ext)(nil)

// Manifest implements extsdk.Ext.
func (*Ext) Manifest() extsdk.Manifest {
	sub, err := fs.Sub(migrations, "migrations")
	if err != nil {
		panic(err) // the directory is embedded above
	}
	m := extsdk.MustManifest(manifestJSON)
	m.Migrations = sub
	k := m.Kind(BoardKind)
	k.OnCreate, k.OnCopy, k.ValidSubject = seedBoard, copyBoard, validSubject
	return m
}

// Register implements extsdk.Ext.
func (*Ext) Register(h extsdk.Host) error {
	s := &service{h: h, db: h.DB()}
	r := h.Router()
	r.Route("/boards/{nodeID}", func(r chi.Router) {
		r.Group(func(r chi.Router) {
			r.Use(h.Guard(BoardKind, extsdk.CapView))
			r.Get("/", s.handleBoard)
			r.Get("/events", s.handleEvents)
			r.Get("/people", s.handlePeople)
		})
		r.Group(func(r chi.Router) {
			r.Use(h.Guard(BoardKind, extsdk.CapEdit))
			r.Post("/columns", s.handleCreateColumn)
			r.Patch("/columns/{columnID}", s.handleUpdateColumn)
			r.Delete("/columns/{columnID}", s.handleDeleteColumn)
			r.Post("/cards", s.handleCreateCard)
			r.Patch("/cards/{cardID}", s.handleUpdateCard)
			r.Post("/cards/{cardID}/move", s.handleMoveCard)
			r.Delete("/cards/{cardID}", s.handleDeleteCard)
			r.Post("/labels", s.handleCreateLabel)
			r.Patch("/labels/{labelID}", s.handleUpdateLabel)
			r.Delete("/labels/{labelID}", s.handleDeleteLabel)
		})
	})
	return nil
}

type service struct {
	h  extsdk.Host
	db extsdk.DB
}

// defaultColumns are what a new board starts with.
var defaultColumns = []string{"To do", "In progress", "Done"}

func seedBoard(ctx context.Context, tx extsdk.Querier, node extsdk.Node) error {
	for i, pos := range Spread(len(defaultColumns)) {
		if _, err := tx.Exec(ctx,
			`INSERT INTO ext_kanban.columns (board_id, title, position) VALUES ($1, $2, $3)`,
			node.ID, defaultColumns[i], pos); err != nil {
			return err
		}
	}
	return nil
}

// validSubject says a comment thread may hang on "card:<id>" when that card is on
// the board.
func validSubject(ctx context.Context, q extsdk.Querier, node extsdk.Node, subject string) (bool, error) {
	raw, ok := strings.CutPrefix(subject, "card:")
	if !ok {
		return false, nil
	}
	card, err := extsdk.ParseUUID(raw)
	if err != nil {
		return false, nil
	}
	var exists bool
	err = q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM ext_kanban.cards WHERE id = $1 AND board_id = $2)`,
		card, node.ID).Scan(&exists)
	return exists, err
}

// cardSubject is the subject of a card's comment threads.
func cardSubject(card pgtype.UUID) string { return "card:" + extsdk.UUIDString(card) }

// copyBoard copies columns, labels and cards with their labels and links, in
// one statement. Assignees stay with the original — the people on a copy are
// whoever the copy is shared with — and so do its comments.
func copyBoard(ctx context.Context, tx extsdk.Querier, src, dst extsdk.Node) error {
	_, err := tx.Exec(ctx, `
		WITH
		cmap AS (SELECT id AS old_id, gen_random_uuid() AS new_id FROM ext_kanban.columns WHERE board_id = $1),
		lmap AS (SELECT id AS old_id, gen_random_uuid() AS new_id FROM ext_kanban.labels WHERE board_id = $1),
		kmap AS (SELECT id AS old_id, gen_random_uuid() AS new_id FROM ext_kanban.cards WHERE board_id = $1),
		ins_columns AS (
			INSERT INTO ext_kanban.columns (id, board_id, title, position, wip_limit)
			SELECT m.new_id, $2, c.title, c.position, c.wip_limit
			FROM ext_kanban.columns c JOIN cmap m ON m.old_id = c.id
			RETURNING 1),
		ins_labels AS (
			INSERT INTO ext_kanban.labels (id, board_id, name, color)
			SELECT m.new_id, $2, l.name, l.color
			FROM ext_kanban.labels l JOIN lmap m ON m.old_id = l.id
			RETURNING 1),
		ins_cards AS (
			INSERT INTO ext_kanban.cards (id, board_id, column_id, title, description_md, position, due_at, created_by)
			SELECT k.new_id, $2, cm.new_id, c.title, c.description_md, c.position, c.due_at, c.created_by
			FROM ext_kanban.cards c
			JOIN kmap k ON k.old_id = c.id
			JOIN cmap cm ON cm.old_id = c.column_id
			RETURNING 1),
		ins_links AS (
			INSERT INTO ext_kanban.card_links (card_id, node_id)
			SELECT k.new_id, l.node_id
			FROM ext_kanban.card_links l JOIN kmap k ON k.old_id = l.card_id
			RETURNING 1)
		INSERT INTO ext_kanban.card_labels (card_id, label_id)
		SELECT k.new_id, lm.new_id
		FROM ext_kanban.card_labels cl
		JOIN kmap k ON k.old_id = cl.card_id
		JOIN lmap lm ON lm.old_id = cl.label_id`, src.ID, dst.ID)
	return err
}

// notFound answers a board row the request named but the board does not hold.
func notFound(w http.ResponseWriter, what string) {
	extsdk.WriteProblem(w, http.StatusNotFound, what+" not found")
}
