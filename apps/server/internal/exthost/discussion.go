package exthost

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nhatbui/pp-hub/apps/server/extsdk"
	"github.com/nhatbui/pp-hub/apps/server/internal/notifications"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

func (h *extHost) Comments() extsdk.Comments { return comments{h} }

func (h *extHost) Notify() extsdk.Notify { return notify{h} }

type comments struct{ h *extHost }

func (c comments) Counts(ctx context.Context, node pgtype.UUID) (map[string]int64, error) {
	rows, err := c.h.r.cfg.Store.CountSubjectComments(ctx, node)
	if err != nil {
		return nil, err
	}
	out := make(map[string]int64, len(rows))
	for _, r := range rows {
		out[r.Subject] = r.Comments
	}
	return out, nil
}

func (c comments) Forget(ctx context.Context, node pgtype.UUID, subject string) error {
	return c.h.r.cfg.Store.DeleteSubjectThreads(ctx, store.DeleteSubjectThreadsParams{
		NodeID: node, Subject: pgtype.Text{String: subject, Valid: true},
	})
}

type notify struct{ h *extHost }

// Send writes one notification and pushes it to the person's open tabs. It
// tells nobody who is the actor, nor anybody who could not open the node: a
// notification quotes the node's content.
func (n notify) Send(ctx context.Context, note extsdk.Notification) error {
	cfg := n.h.r.cfg
	if cfg.Notifications == nil || note.To == note.Actor {
		return nil
	}
	readers, err := cfg.Notifications.Readers(ctx, store.Node{ID: note.Node.ID}, []pgtype.UUID{note.To})
	if err != nil || len(readers) == 0 {
		return err
	}
	ids, err := cfg.Notifications.Write(ctx, cfg.Store, note.Actor, note.Node.ID, []notifications.New{{
		UserID:  note.To,
		Kind:    notifications.Kind(fmt.Sprintf("ext:%s:%s", n.h.e.m.ID, note.Verb)),
		Excerpt: notifications.Excerpt(note.Excerpt),
		Subject: note.Subject,
	}})
	if err != nil {
		return err
	}
	cfg.Notifications.Deliver(ctx, ids)
	return nil
}
