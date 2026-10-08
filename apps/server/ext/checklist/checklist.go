// Package checklist is pp-hub's second extension, written to find out whether
// extsdk is enough on its own: a checklist is a node in the drive (kind
// "checklist.list") whose items live in the Postgres schema ext_checklist. It
// uses nothing but extsdk.
package checklist

import (
	"context"
	"embed"
	"io/fs"

	"github.com/go-chi/chi/v5"

	"github.com/nhatbui/pp-hub/apps/server/extsdk"
)

// ListKind is the node kind of a checklist; pp-app.json declares it too.
const ListKind = "checklist.list"

// maxItems keeps one list from becoming an unbounded table scan.
const maxItems = 500

// maxText is the longest item text, in characters.
const maxText = 500

//go:embed migrations/*.sql
var migrations embed.FS

//go:embed pp-app.json
var manifestJSON []byte

// Ext is the checklist extension.
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
	m.Kind(ListKind).OnCopy = copyList
	return m
}

// Register implements extsdk.Ext.
func (*Ext) Register(h extsdk.Host) error {
	s := &service{h: h, db: h.DB()}
	r := h.Router()
	r.Route("/lists/{nodeID}", func(r chi.Router) {
		r.Group(func(r chi.Router) {
			r.Use(h.Guard(ListKind, extsdk.CapView))
			r.Get("/", s.handleList)
			r.Get("/events", s.handleEvents)
		})
		r.Group(func(r chi.Router) {
			r.Use(h.Guard(ListKind, extsdk.CapEdit))
			r.Post("/items", s.handleCreateItem)
			r.Patch("/items/{itemID}", s.handleUpdateItem)
			r.Delete("/items/{itemID}", s.handleDeleteItem)
		})
	})
	return nil
}

type service struct {
	h  extsdk.Host
	db extsdk.DB
}

// copyList copies the items, ticked or not. Who made them is the original's.
func copyList(ctx context.Context, tx extsdk.Querier, src, dst extsdk.Node) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO ext_checklist.items (list_id, text, done)
		SELECT $2, text, done FROM ext_checklist.items WHERE list_id = $1 ORDER BY seq`,
		src.ID, dst.ID)
	return err
}
