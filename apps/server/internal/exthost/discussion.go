package exthost

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nhatbui/pp-hub/apps/server/internal/notifications"
	"github.com/nhatbui/pp-hub/apps/server/internal/share"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

// What an app gets from the rest of pp-hub: the discussion on its records
// (comment threads with a subject, kept by the comments module), in-app
// notifications, people and links — each decided by the resolver, never by
// the app.

// commentCounts is the number of comments per subject on a node.
func (r *Registry) commentCounts(ctx context.Context, node pgtype.UUID) (map[string]int64, error) {
	rows, err := r.cfg.Store.CountSubjectComments(ctx, node)
	if err != nil {
		return nil, err
	}
	out := make(map[string]int64, len(rows))
	for _, row := range rows {
		out[row.Subject] = row.Comments
	}
	return out, nil
}

// forgetSubject deletes the threads about a part of a node that is gone.
func (r *Registry) forgetSubject(ctx context.Context, node pgtype.UUID, subject string) error {
	return r.cfg.Store.DeleteSubjectThreads(ctx, store.DeleteSubjectThreadsParams{
		NodeID: node, Subject: pgtype.Text{String: subject, Valid: true},
	})
}

// notice is one thing an app tells somebody.
type notice struct {
	appID   string
	to      pgtype.UUID
	actor   pgtype.UUID
	node    pgtype.UUID
	verb    string // the kind is "ext:<app id>:<verb>"
	subject string
	excerpt string
}

// notify writes one notification and pushes it to the person's open tabs. It
// tells nobody who is the actor, nor anybody who could not open the node: a
// notification quotes the node's content.
func (r *Registry) notify(ctx context.Context, n notice) error {
	svc := r.cfg.Notifications
	if svc == nil || n.to == n.actor {
		return nil
	}
	readers, err := svc.Readers(ctx, store.Node{ID: n.node}, []pgtype.UUID{n.to})
	if err != nil || len(readers) == 0 {
		return err
	}
	ids, err := svc.Write(ctx, r.cfg.Store, n.actor, n.node, []notifications.New{{
		UserID:  n.to,
		Kind:    notifications.Kind("ext:" + n.appID + ":" + n.verb),
		Excerpt: notifications.Excerpt(n.excerpt),
		Subject: n.subject,
	}})
	if err != nil {
		return err
	}
	svc.Deliver(ctx, ids)
	return nil
}

// person is somebody as an app sees them.
type person struct {
	ID    pgtype.UUID
	Name  string
	Email string
}

// searchPeople is who could open node among the members of its space whose
// name or address matches query.
func (r *Registry) searchPeople(ctx context.Context, node store.Node, query string, limit int) ([]person, error) {
	if limit < 1 || limit > 50 {
		limit = 20
	}
	// Candidates are members of the node's space; each is then checked
	// against the resolver, so the list is exactly who could open the node.
	// Fetching a few more than asked absorbs the members who cannot.
	rows, err := r.cfg.Store.SearchSpaceUsers(ctx, store.SearchSpaceUsersParams{
		SpaceID: node.SpaceID, Q: query, MaxRows: int32(limit * 3),
	})
	if err != nil {
		return nil, err
	}
	out := make([]person, 0, limit)
	for _, u := range rows {
		c, err := r.cfg.Share.EffectiveCapsForNode(ctx, node, u.ID)
		if err != nil {
			return nil, err
		}
		if !c.View {
			continue
		}
		out = append(out, person{ID: u.ID, Name: displayName(u.DisplayName, u.Name, u.Email), Email: u.Email})
		if len(out) == limit {
			break
		}
	}
	return out, nil
}

// resolve is the live nodes among ids that viewer can open, in no order.
func (r *Registry) resolve(ctx context.Context, viewer pgtype.UUID, ids []pgtype.UUID) ([]store.GetLiveNodesByIDsRow, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := r.cfg.Store.GetLiveNodesByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	live := make([]pgtype.UUID, 0, len(rows))
	for _, n := range rows {
		live = append(live, n.ID)
	}
	res, err := r.cfg.Share.AccessCaps(ctx, live, viewer)
	if err != nil {
		return nil, err
	}
	out := rows[:0]
	for _, n := range rows {
		if share.EffectiveCaps(res[n.ID]).View {
			out = append(out, n)
		}
	}
	return out, nil
}
