package checklist

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nhatbui/pp-hub/apps/server/extsdk"
)

type itemDTO struct {
	ID        string `json:"id"`
	Text      string `json:"text"`
	Done      bool   `json:"done"`
	CreatedAt string `json:"created_at"`
}

type listDTO struct {
	ID    string      `json:"id"`
	Name  string      `json:"name"`
	Caps  extsdk.Caps `json:"caps"`
	Items []itemDTO   `json:"items"`
}

// publish tells everyone watching the list.
func (s *service) publish(list pgtype.UUID, typ string, payload map[string]any) {
	payload["type"] = typ
	s.h.Realtime().Publish("list:"+extsdk.UUIDString(list), "list", payload)
}

func scanItem(row pgx.Row) (itemDTO, error) {
	var (
		it itemDTO
		id pgtype.UUID
		at time.Time
	)
	if err := row.Scan(&id, &it.Text, &it.Done, &at); err != nil {
		return it, err
	}
	it.ID = extsdk.UUIDString(id)
	it.CreatedAt = at.UTC().Format(time.RFC3339)
	return it, nil
}

func (s *service) handleList(w http.ResponseWriter, r *http.Request) {
	q := extsdk.MustRequest(r)
	rows, err := s.db.Query(r.Context(), `
		SELECT id, text, done, created_at FROM ext_checklist.items
		WHERE list_id = $1 ORDER BY seq`, q.Node.ID)
	if err != nil {
		extsdk.Fail(w, err)
		return
	}
	defer rows.Close()
	items := []itemDTO{}
	for rows.Next() {
		it, err := scanItem(rows)
		if err != nil {
			extsdk.Fail(w, err)
			return
		}
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		extsdk.Fail(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	extsdk.WriteJSON(w, http.StatusOK, listDTO{
		ID: extsdk.UUIDString(q.Node.ID), Name: q.Node.Name, Caps: q.Caps, Items: items,
	})
}

func (s *service) handleEvents(w http.ResponseWriter, r *http.Request) {
	q := extsdk.MustRequest(r)
	s.h.Realtime().Stream(w, r, "list:"+extsdk.UUIDString(q.Node.ID), func(ctx context.Context) bool {
		return s.h.Allowed(ctx, q.Node.ID, q.User.ID, ListKind, extsdk.CapView)
	})
}

func (s *service) handleCreateItem(w http.ResponseWriter, r *http.Request) {
	q := extsdk.MustRequest(r)
	var body struct {
		Text string `json:"text"`
	}
	if !extsdk.DecodeBody(w, r, &body) {
		return
	}
	t, ok := extsdk.Trim(w, body.Text, "text", maxText, true)
	if !ok {
		return
	}
	ctx := r.Context()
	var n int
	if err := s.db.QueryRow(ctx, `SELECT count(*) FROM ext_checklist.items WHERE list_id = $1`, q.Node.ID).Scan(&n); err != nil {
		extsdk.Fail(w, err)
		return
	}
	if n >= maxItems {
		extsdk.WriteProblem(w, http.StatusConflict, "a checklist holds at most 500 items")
		return
	}
	it, err := scanItem(s.db.QueryRow(ctx, `
		INSERT INTO ext_checklist.items (list_id, text, created_by) VALUES ($1, $2, $3)
		RETURNING id, text, done, created_at`, q.Node.ID, t, q.User.ID))
	if err != nil {
		extsdk.Fail(w, err)
		return
	}
	s.publish(q.Node.ID, "item.upsert", map[string]any{"item": it})
	extsdk.WriteJSON(w, http.StatusCreated, it)
}

func (s *service) handleUpdateItem(w http.ResponseWriter, r *http.Request) {
	q := extsdk.MustRequest(r)
	itemID, err := extsdk.ParseUUID(chi.URLParam(r, "itemID"))
	if err != nil {
		extsdk.WriteProblem(w, http.StatusNotFound, "item not found")
		return
	}
	var body struct {
		Text *string `json:"text"`
		Done *bool   `json:"done"`
	}
	if !extsdk.DecodeBody(w, r, &body) {
		return
	}
	var t *string
	if body.Text != nil {
		v, ok := extsdk.Trim(w, *body.Text, "text", maxText, true)
		if !ok {
			return
		}
		t = &v
	}
	it, err := scanItem(s.db.QueryRow(r.Context(), `
		UPDATE ext_checklist.items SET text = COALESCE($3, text), done = COALESCE($4, done)
		WHERE id = $1 AND list_id = $2
		RETURNING id, text, done, created_at`, itemID, q.Node.ID, t, body.Done))
	if err != nil {
		extsdk.Fail(w, err)
		return
	}
	s.publish(q.Node.ID, "item.upsert", map[string]any{"item": it})
	extsdk.WriteJSON(w, http.StatusOK, it)
}

func (s *service) handleDeleteItem(w http.ResponseWriter, r *http.Request) {
	q := extsdk.MustRequest(r)
	itemID, err := extsdk.ParseUUID(chi.URLParam(r, "itemID"))
	if err != nil {
		extsdk.WriteProblem(w, http.StatusNotFound, "item not found")
		return
	}
	tag, err := s.db.Exec(r.Context(), `DELETE FROM ext_checklist.items WHERE id = $1 AND list_id = $2`, itemID, q.Node.ID)
	if err != nil {
		extsdk.Fail(w, err)
		return
	}
	if tag.RowsAffected() == 0 {
		extsdk.WriteProblem(w, http.StatusNotFound, "item not found")
		return
	}
	s.publish(q.Node.ID, "item.delete", map[string]any{"id": extsdk.UUIDString(itemID)})
	w.WriteHeader(http.StatusNoContent)
}
