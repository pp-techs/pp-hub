package exthost

import (
	"context"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nhatbui/pp-hub/apps/server/extsdk"
	"github.com/nhatbui/pp-hub/apps/server/internal/fs"
	"github.com/nhatbui/pp-hub/apps/server/internal/share"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

// extHost is the facade one extension sees.
type extHost struct {
	r *Registry
	e *entry
}

var _ extsdk.Host = (*extHost)(nil)

func (h *extHost) Router() chi.Router { return h.e.router }

func (h *extHost) DB() extsdk.DB { return h.r.cfg.Pool }

func (h *extHost) Realtime() extsdk.Realtime {
	return realtime{b: h.r.rt, active: h.r.sessionActive, recheck: h.r.recheck()}
}

func (h *extHost) Caps(ctx context.Context, node extsdk.Node, user pgtype.UUID) (extsdk.Caps, error) {
	c, err := h.r.cfg.Share.EffectiveCapsForNode(ctx, store.Node{ID: node.ID}, user)
	if err != nil {
		return extsdk.Caps{}, err
	}
	return toCaps(c), nil
}

func toCaps(c share.Caps) extsdk.Caps {
	return extsdk.Caps{View: c.View, Download: c.Download, Share: c.Share, Edit: c.Edit, Owner: c.Owner}
}

// refusal is why authorize said no: the HTTP status and the detail.
type refusal struct {
	status int
	detail string
}

// authorize is the one decision both a request and a long-lived stream ask: the
// node is live and of the extension's kind, the extension is on for its space,
// and the person holds need on it.
func (h *extHost) authorize(ctx context.Context, nodeID, user pgtype.UUID, kind string, need extsdk.Cap) (store.Node, extsdk.Caps, *refusal) {
	node, err := h.r.cfg.FS.Get(ctx, nodeID)
	if errors.Is(err, fs.ErrNotFound) {
		return store.Node{}, extsdk.Caps{}, &refusal{http.StatusNotFound, "node not found"}
	}
	if err != nil {
		return store.Node{}, extsdk.Caps{}, &refusal{http.StatusInternalServerError, ""}
	}
	if node.Kind != store.NodeKindExt || node.ExtKind.String != kind {
		return store.Node{}, extsdk.Caps{}, &refusal{http.StatusNotFound, "node not found"}
	}
	on, err := h.r.Enabled(ctx, h.e.m.ID, node.SpaceID)
	if err != nil {
		return store.Node{}, extsdk.Caps{}, &refusal{http.StatusInternalServerError, ""}
	}
	if !on {
		return store.Node{}, extsdk.Caps{}, &refusal{http.StatusForbidden, ErrDisabled.Error()}
	}
	caps, err := h.r.cfg.Share.EffectiveCapsForNode(ctx, node, user)
	if err != nil {
		return store.Node{}, extsdk.Caps{}, &refusal{http.StatusInternalServerError, ""}
	}
	c := toCaps(caps)
	if !c.Has(need) {
		return store.Node{}, extsdk.Caps{}, &refusal{http.StatusForbidden, "insufficient permission"}
	}
	return node, c, nil
}

func (h *extHost) Allowed(ctx context.Context, node, user pgtype.UUID, kind string, need extsdk.Cap) bool {
	_, _, no := h.authorize(ctx, node, user, kind, need)
	return no == nil
}

func (h *extHost) Guard(kind string, need extsdk.Cap) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			nodeID, err := extsdk.ParseUUID(chi.URLParam(req, "nodeID"))
			if err != nil {
				extsdk.WriteProblem(w, http.StatusBadRequest, "invalid node id")
				return
			}
			u := h.r.caller(req)
			node, caps, no := h.authorize(req.Context(), nodeID, u.ID, kind, need)
			if no != nil {
				extsdk.WriteProblem(w, no.status, no.detail)
				return
			}
			rq := extsdk.Request{
				Node:   toNode(node),
				User:   extsdk.User{ID: u.ID, Name: displayName(u.DisplayName, u.Name, u.Email), Email: u.Email},
				Caps:   caps,
				Method: req.Method,
			}
			next.ServeHTTP(w, req.WithContext(extsdk.WithRequest(req.Context(), rq)))
		})
	}
}

func displayName(display, name, email string) string {
	switch {
	case display != "":
		return display
	case name != "":
		return name
	}
	return email
}

func (h *extHost) SearchPeople(ctx context.Context, node extsdk.Node, query string, limit int) ([]extsdk.User, error) {
	if limit < 1 || limit > 50 {
		limit = 20
	}
	// Candidates are members of the node's space; each is then checked against
	// the resolver, so the list is exactly who could open the node. Fetching a
	// few more than asked absorbs the members who cannot.
	rows, err := h.r.cfg.Store.SearchSpaceUsers(ctx, store.SearchSpaceUsersParams{
		SpaceID: node.SpaceID, Q: query, MaxRows: int32(limit * 3),
	})
	if err != nil {
		return nil, err
	}
	out := make([]extsdk.User, 0, limit)
	for _, u := range rows {
		c, err := h.Caps(ctx, node, u.ID)
		if err != nil {
			return nil, err
		}
		if !c.View {
			continue
		}
		out = append(out, extsdk.User{ID: u.ID, Name: displayName(u.DisplayName, u.Name, u.Email), Email: u.Email})
		if len(out) == limit {
			break
		}
	}
	return out, nil
}

func (h *extHost) Users(ctx context.Context, ids []pgtype.UUID) (map[pgtype.UUID]extsdk.User, error) {
	out := make(map[pgtype.UUID]extsdk.User, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := h.r.cfg.Store.ListUsersByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	for _, u := range rows {
		out[u.ID] = extsdk.User{ID: u.ID, Name: displayName(u.DisplayName, u.Name, u.Email), Email: u.Email}
	}
	return out, nil
}

func (h *extHost) Resolve(ctx context.Context, viewer pgtype.UUID, ids []pgtype.UUID) (map[pgtype.UUID]extsdk.LinkTarget, error) {
	out := map[pgtype.UUID]extsdk.LinkTarget{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := h.r.cfg.Store.GetLiveNodesByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	live := make([]pgtype.UUID, 0, len(rows))
	for _, n := range rows {
		live = append(live, n.ID)
	}
	res, err := h.r.cfg.Share.AccessCaps(ctx, live, viewer)
	if err != nil {
		return nil, err
	}
	for _, n := range rows {
		if !share.EffectiveCaps(res[n.ID]).View {
			continue
		}
		out[n.ID] = extsdk.LinkTarget{
			ID: n.ID, SpaceID: n.SpaceID, Name: n.Name, Kind: string(n.Kind), ExtKind: n.ExtKind.String,
		}
	}
	return out, nil
}
