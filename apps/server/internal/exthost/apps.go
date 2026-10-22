package exthost

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nhatbui/pp-hub/apps/server/internal/apppkg"
	"github.com/nhatbui/pp-hub/apps/server/internal/fs"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

// The runtime every packaged app shares (M51): one set of routes under
// /api/v1/apps/{appID}/nodes/{nodeID}, answered by the host for whichever app
// the URL names. The app's frame never calls them itself — it has an opaque
// origin and no session — the page hosting it does, on its behalf, over the
// bridge (apps/web/src/ext/app-frame). An app's backend reaches the same
// handlers through backend.go with a signed token instead of a session.

const (
	// maxRecords bounds one node's records.
	maxRecords = 10000
	// maxPage is the most records one list call returns.
	maxPage = 1000
	// maxBroadcastBytes bounds an ephemeral event's payload.
	maxBroadcastBytes = 16 << 10
	// maxIDs bounds a users or links lookup.
	maxIDs = 100
)

var eventRe = regexp.MustCompile(`^[a-z][a-z0-9_.:-]{0,63}$`)

// appReq is what an app route established before its handler runs.
type appReq struct {
	e    *entry
	node store.Node
	user pgtype.UUID
	caps Caps
}

type appReqKey struct{}

func appRequest(r *http.Request) appReq {
	return r.Context().Value(appReqKey{}).(appReq)
}

func (r *Registry) mountApps(api chi.Router) {
	api.Route("/apps/{appID}/nodes/{nodeID}", func(rt chi.Router) {
		rt.Group(func(rt chi.Router) {
			rt.Use(r.sessionGuard(CapView))
			r.readRoutes(rt)
			rt.Get("/events", r.handleAppEvents)
			rt.Get("/people", r.handleAppPeople)
			rt.Get("/users", r.handleAppUsers)
			rt.Get("/links", r.handleAppLinks)
			rt.Get("/comments/counts", r.handleAppCommentCounts)
			// The backend decides what its own routes need from the caps
			// the token carries; reaching it takes view.
			rt.HandleFunc("/backend", r.handleBackendProxy)
			rt.HandleFunc("/backend/*", r.handleBackendProxy)
		})
		rt.Group(func(rt chi.Router) {
			rt.Use(r.sessionGuard(CapEdit))
			r.writeRoutes(rt)
		})
	})
}

// readRoutes and writeRoutes are the record routes both callers reach: the
// session (mountApps) and a backend's token (MountBackendAPI).
func (r *Registry) readRoutes(rt chi.Router) {
	rt.Get("/records", r.handleListRecords)
	rt.Get("/records/{recordID}", r.handleGetRecord)
	rt.Post("/broadcast", r.handleBroadcast)
}

func (r *Registry) writeRoutes(rt chi.Router) {
	rt.Post("/records", r.handleCreateRecord)
	rt.Patch("/records/{recordID}", r.handleUpdateRecord)
	rt.Delete("/records/{recordID}", r.handleDeleteRecord)
	rt.Post("/notify", r.handleAppNotify)
}

// refusal is why authorizeApp said no: the HTTP status and the detail.
type refusal struct {
	status int
	detail string
}

// authorizeApp is the one decision every app route and stream asks: the app
// is installed, the node is live and of one of its kinds, the app is on for
// the node's space, and user holds need on the node.
func (r *Registry) authorizeApp(ctx context.Context, appID string, nodeID, user pgtype.UUID, need Cap) (appReq, *refusal) {
	e := r.lookup(appID)
	if e == nil {
		return appReq{}, &refusal{http.StatusNotFound, "app not found"}
	}
	node, err := r.cfg.FS.Get(ctx, nodeID)
	if errors.Is(err, fs.ErrNotFound) {
		return appReq{}, &refusal{http.StatusNotFound, "node not found"}
	}
	if err != nil {
		return appReq{}, &refusal{http.StatusInternalServerError, ""}
	}
	if node.Kind != store.NodeKindExt || r.kindOf(node.ExtKind.String) != e {
		return appReq{}, &refusal{http.StatusNotFound, "node not found"}
	}
	on, err := r.Enabled(ctx, appID, node.SpaceID)
	if err != nil {
		return appReq{}, &refusal{http.StatusInternalServerError, ""}
	}
	if !on {
		return appReq{}, &refusal{http.StatusForbidden, ErrDisabled.Error()}
	}
	caps, err := r.cfg.Share.EffectiveCapsForNode(ctx, node, user)
	if err != nil {
		return appReq{}, &refusal{http.StatusInternalServerError, ""}
	}
	c := toCaps(caps)
	if !c.Has(need) {
		return appReq{}, &refusal{http.StatusForbidden, "insufficient permission"}
	}
	return appReq{e: e, node: node, user: user, caps: c}, nil
}

func (r *Registry) sessionGuard(need Cap) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			nodeID, err := parseUUID(chi.URLParam(req, "nodeID"))
			if err != nil {
				writeProblem(w, http.StatusBadRequest, "invalid node id")
				return
			}
			u := r.caller(req)
			rq, no := r.authorizeApp(req.Context(), chi.URLParam(req, "appID"), nodeID, u.ID, need)
			if no != nil {
				writeProblem(w, no.status, no.detail)
				return
			}
			next.ServeHTTP(w, req.WithContext(context.WithValue(req.Context(), appReqKey{}, rq)))
		})
	}
}

func appTopic(rq appReq) string {
	return "app:" + rq.e.m.ID + ":" + uuidString(rq.node.ID)
}

// recordDTO is a record on the wire (contract: AppRecord).
type recordDTO struct {
	ID         string          `json:"id"`
	Collection string          `json:"collection"`
	Data       json.RawMessage `json:"data"`
	Rev        int64           `json:"rev"`
	CreatedBy  *string         `json:"created_by"`
	UpdatedBy  *string         `json:"updated_by"`
	CreatedAt  string          `json:"created_at"`
	UpdatedAt  string          `json:"updated_at"`
}

func optUUID(id pgtype.UUID) *string {
	if !id.Valid {
		return nil
	}
	s := uuidString(id)
	return &s
}

func toRecordDTO(rec store.ExtRecord) recordDTO {
	return recordDTO{
		ID: uuidString(rec.ID), Collection: rec.Collection, Data: rec.Data, Rev: rec.Rev,
		CreatedBy: optUUID(rec.CreatedBy), UpdatedBy: optUUID(rec.UpdatedBy),
		CreatedAt: rec.CreatedAt.Time.UTC().Format(time.RFC3339Nano),
		UpdatedAt: rec.UpdatedAt.Time.UTC().Format(time.RFC3339Nano),
	}
}

func (r *Registry) publishRecord(rq appReq, rec store.ExtRecord) {
	r.rt.publish(appTopic(rq), "app", map[string]any{"type": "record.upsert", "record": toRecordDTO(rec)})
}

// encodeCursor and decodeCursor are the opaque "after" of a list page: the
// last record's (created_at, id).
func encodeCursor(rec store.ExtRecord) string {
	raw := strconv.FormatInt(rec.CreatedAt.Time.UnixMicro(), 10) + "." + uuidString(rec.ID)
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func decodeCursor(s string) (pgtype.Timestamptz, pgtype.UUID, bool) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return pgtype.Timestamptz{}, pgtype.UUID{}, false
	}
	at, id, ok := strings.Cut(string(raw), ".")
	if !ok {
		return pgtype.Timestamptz{}, pgtype.UUID{}, false
	}
	micros, err := strconv.ParseInt(at, 10, 64)
	if err != nil {
		return pgtype.Timestamptz{}, pgtype.UUID{}, false
	}
	uid, err := parseUUID(id)
	if err != nil {
		return pgtype.Timestamptz{}, pgtype.UUID{}, false
	}
	return pgtype.Timestamptz{Time: time.UnixMicro(micros), Valid: true}, uid, true
}

// handleListRecords is GET …/records[?collection=&after=&limit=]: a page in
// creation order, and the cursor of the next one ("" at the end).
func (r *Registry) handleListRecords(w http.ResponseWriter, req *http.Request) {
	rq := appRequest(req)
	q := req.URL.Query()
	p := store.ListExtRecordsParams{NodeID: rq.node.ID, MaxRows: maxPage}
	if c := q.Get("collection"); c != "" {
		if !apppkg.ValidCollection(c) {
			writeProblem(w, http.StatusBadRequest, "invalid collection")
			return
		}
		p.Collection = pgtype.Text{String: c, Valid: true}
	}
	if raw := q.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > maxPage {
			writeProblem(w, http.StatusBadRequest, "limit must be between 1 and 1000")
			return
		}
		p.MaxRows = int32(n)
	}
	if after := q.Get("after"); after != "" {
		at, id, ok := decodeCursor(after)
		if !ok {
			writeProblem(w, http.StatusBadRequest, "invalid cursor")
			return
		}
		p.AfterAt, p.AfterID = at, id
	}
	rows, err := r.cfg.Store.ListExtRecords(req.Context(), p)
	if err != nil {
		fail(w, err)
		return
	}
	out := struct {
		Records []recordDTO `json:"records"`
		Next    string      `json:"next"`
	}{Records: make([]recordDTO, 0, len(rows))}
	for _, rec := range rows {
		out.Records = append(out.Records, toRecordDTO(rec))
	}
	if len(rows) == int(p.MaxRows) {
		out.Next = encodeCursor(rows[len(rows)-1])
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, out)
}

func recordParam(w http.ResponseWriter, req *http.Request) (pgtype.UUID, bool) {
	id, err := parseUUID(chi.URLParam(req, "recordID"))
	if err != nil {
		writeProblem(w, http.StatusNotFound, "record not found")
		return pgtype.UUID{}, false
	}
	return id, true
}

func (r *Registry) handleGetRecord(w http.ResponseWriter, req *http.Request) {
	rq := appRequest(req)
	id, ok := recordParam(w, req)
	if !ok {
		return
	}
	rec, err := r.cfg.Store.GetExtRecord(req.Context(), store.GetExtRecordParams{NodeID: rq.node.ID, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		writeProblem(w, http.StatusNotFound, "record not found")
		return
	}
	if err != nil {
		fail(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, toRecordDTO(rec))
}

// objectJSON checks that raw is one JSON object of at most max bytes.
func objectJSON(w http.ResponseWriter, raw json.RawMessage, field string) bool {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' || !json.Valid(trimmed) {
		writeProblem(w, http.StatusBadRequest, field+" must be a JSON object")
		return false
	}
	if len(trimmed) > apppkg.MaxRecordBytes {
		writeProblem(w, http.StatusRequestEntityTooLarge, "a record holds at most 64 KiB")
		return false
	}
	return true
}

func (r *Registry) handleCreateRecord(w http.ResponseWriter, req *http.Request) {
	rq := appRequest(req)
	var body struct {
		Collection string          `json:"collection"`
		Data       json.RawMessage `json:"data"`
	}
	if !decodeBody(w, req, &body) {
		return
	}
	if !apppkg.ValidCollection(body.Collection) {
		writeProblem(w, http.StatusBadRequest, "collection must match "+apppkg.CollectionPattern)
		return
	}
	if !objectJSON(w, body.Data, "data") {
		return
	}
	ctx := req.Context()
	n, err := r.cfg.Store.CountExtRecords(ctx, rq.node.ID)
	if err != nil {
		fail(w, err)
		return
	}
	if n >= maxRecords {
		writeProblem(w, http.StatusConflict, "an item holds at most 10000 records")
		return
	}
	rec, err := r.cfg.Store.CreateExtRecord(ctx, store.CreateExtRecordParams{
		NodeID: rq.node.ID, Collection: body.Collection, Data: body.Data, By: rq.user,
	})
	if err != nil {
		fail(w, err)
		return
	}
	r.publishRecord(rq, rec)
	writeJSON(w, http.StatusCreated, toRecordDTO(rec))
}

// handleUpdateRecord is PATCH …/records/{id} with exactly one of data (the
// new data) and merge (keys to set; null removes one), optionally if_rev.
func (r *Registry) handleUpdateRecord(w http.ResponseWriter, req *http.Request) {
	rq := appRequest(req)
	id, ok := recordParam(w, req)
	if !ok {
		return
	}
	var body struct {
		Data  json.RawMessage `json:"data"`
		Merge json.RawMessage `json:"merge"`
		IfRev *int64          `json:"if_rev"`
	}
	if !decodeBody(w, req, &body) {
		return
	}
	hasData := len(body.Data) > 0 && string(body.Data) != "null"
	hasMerge := len(body.Merge) > 0 && string(body.Merge) != "null"
	if hasData == hasMerge {
		writeProblem(w, http.StatusBadRequest, "send exactly one of data and merge")
		return
	}
	var ifRev pgtype.Int8
	if body.IfRev != nil {
		ifRev = pgtype.Int8{Int64: *body.IfRev, Valid: true}
	}
	ctx := req.Context()
	var (
		rec store.ExtRecord
		err error
	)
	if hasData {
		if !objectJSON(w, body.Data, "data") {
			return
		}
		rec, err = r.cfg.Store.ReplaceExtRecord(ctx, store.ReplaceExtRecordParams{
			NodeID: rq.node.ID, ID: id, Data: body.Data, By: rq.user, IfRev: ifRev,
		})
	} else {
		if !objectJSON(w, body.Merge, "merge") {
			return
		}
		rec, err = r.cfg.Store.MergeExtRecord(ctx, store.MergeExtRecordParams{
			NodeID: rq.node.ID, ID: id, Patch: body.Merge, By: rq.user, IfRev: ifRev,
		})
	}
	if errors.Is(err, pgx.ErrNoRows) {
		// Either there is no such record or if_rev is stale; say which.
		if _, gerr := r.cfg.Store.GetExtRecord(ctx, store.GetExtRecordParams{NodeID: rq.node.ID, ID: id}); gerr == nil {
			writeProblem(w, http.StatusConflict, "the record has changed since it was read")
			return
		}
		writeProblem(w, http.StatusNotFound, "record not found")
		return
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23514" {
		writeProblem(w, http.StatusRequestEntityTooLarge, "a record holds at most 64 KiB")
		return
	}
	if err != nil {
		fail(w, err)
		return
	}
	r.publishRecord(rq, rec)
	writeJSON(w, http.StatusOK, toRecordDTO(rec))
}

func (r *Registry) handleDeleteRecord(w http.ResponseWriter, req *http.Request) {
	rq := appRequest(req)
	id, ok := recordParam(w, req)
	if !ok {
		return
	}
	ctx := req.Context()
	collection, err := r.cfg.Store.DeleteExtRecord(ctx, store.DeleteExtRecordParams{NodeID: rq.node.ID, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		writeProblem(w, http.StatusNotFound, "record not found")
		return
	}
	if err != nil {
		fail(w, err)
		return
	}
	// A record's discussion goes with it.
	if err := r.forgetSubject(ctx, rq.node.ID, subjectPrefix+uuidString(id)); err != nil {
		fail(w, err)
		return
	}
	r.rt.publish(appTopic(rq), "app", map[string]any{
		"type": "record.delete", "id": uuidString(id), "collection": collection,
	})
	w.WriteHeader(http.StatusNoContent)
}

// handleAppEvents is the node's event stream: record changes and broadcasts,
// for as long as the viewer may still open the node.
func (r *Registry) handleAppEvents(w http.ResponseWriter, req *http.Request) {
	rq := appRequest(req)
	rt := realtime{b: r.rt, active: r.sessionActive, recheck: r.recheck()}
	rt.Stream(w, req, appTopic(rq), func(ctx context.Context) bool {
		_, no := r.authorizeApp(ctx, rq.e.m.ID, rq.node.ID, rq.user, CapView)
		return no == nil
	})
}

// handleBroadcast is POST …/broadcast: an event for everyone watching the
// node that is not stored — presence, a cursor, "someone is typing".
func (r *Registry) handleBroadcast(w http.ResponseWriter, req *http.Request) {
	rq := appRequest(req)
	var body struct {
		Event string          `json:"event"`
		Data  json.RawMessage `json:"data"`
	}
	if !decodeBody(w, req, &body) {
		return
	}
	if !eventRe.MatchString(body.Event) {
		writeProblem(w, http.StatusBadRequest, "event must match "+eventRe.String())
		return
	}
	if len(body.Data) == 0 {
		body.Data = json.RawMessage("null")
	}
	if len(body.Data) > maxBroadcastBytes {
		writeProblem(w, http.StatusRequestEntityTooLarge, "a broadcast carries at most 16 KiB")
		return
	}
	r.rt.publish(appTopic(rq), "app", map[string]any{
		"type": "broadcast", "event": body.Event, "data": body.Data, "from": uuidString(rq.user),
	})
	w.WriteHeader(http.StatusNoContent)
}

type personDTO struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email,omitempty"`
}

func (r *Registry) handleAppPeople(w http.ResponseWriter, req *http.Request) {
	rq := appRequest(req)
	limit, _ := strconv.Atoi(req.URL.Query().Get("limit"))
	users, err := r.searchPeople(req.Context(), rq.node, req.URL.Query().Get("q"), limit)
	if err != nil {
		fail(w, err)
		return
	}
	out := make([]personDTO, 0, len(users))
	for _, u := range users {
		out = append(out, personDTO{ID: uuidString(u.ID), Name: u.Name, Email: u.Email})
	}
	writeJSON(w, http.StatusOK, out)
}

// idsParam reads ?ids=a,b,… (at most maxIDs).
func idsParam(w http.ResponseWriter, req *http.Request) ([]pgtype.UUID, bool) {
	raw := req.URL.Query().Get("ids")
	if raw == "" {
		return nil, true
	}
	parts := strings.Split(raw, ",")
	if len(parts) > maxIDs {
		writeProblem(w, http.StatusBadRequest, "at most 100 ids")
		return nil, false
	}
	ids := make([]pgtype.UUID, 0, len(parts))
	for _, p := range parts {
		id, err := parseUUID(strings.TrimSpace(p))
		if err != nil {
			writeProblem(w, http.StatusBadRequest, "invalid id")
			return nil, false
		}
		ids = append(ids, id)
	}
	return ids, true
}

// handleAppUsers is GET …/users?ids=: names for the people an app's records
// mention. Only members of the node's space and the node's own authors are
// answered, so an id cannot be turned into a stranger's name and address.
func (r *Registry) handleAppUsers(w http.ResponseWriter, req *http.Request) {
	rq := appRequest(req)
	ids, ok := idsParam(w, req)
	if !ok {
		return
	}
	out := []personDTO{}
	if len(ids) > 0 {
		rows, err := r.cfg.Store.ListAppVisibleUsers(req.Context(), store.ListAppVisibleUsersParams{
			Ids: ids, SpaceID: rq.node.SpaceID, NodeID: rq.node.ID,
		})
		if err != nil {
			fail(w, err)
			return
		}
		for _, u := range rows {
			out = append(out, personDTO{ID: uuidString(u.ID), Name: displayName(u.DisplayName, u.Name, u.Email), Email: u.Email})
		}
	}
	writeJSON(w, http.StatusOK, out)
}

type linkDTO struct {
	ID      string `json:"id"`
	SpaceID string `json:"space_id"`
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	ExtKind string `json:"ext_kind,omitempty"`
}

// handleAppLinks is GET …/links?ids=: the drive nodes an app points at that
// this viewer may open; the others are simply absent.
func (r *Registry) handleAppLinks(w http.ResponseWriter, req *http.Request) {
	rq := appRequest(req)
	ids, ok := idsParam(w, req)
	if !ok {
		return
	}
	res, err := r.resolve(req.Context(), rq.user, ids)
	if err != nil {
		fail(w, err)
		return
	}
	out := make([]linkDTO, 0, len(res))
	for _, t := range res {
		out = append(out, linkDTO{
			ID: uuidString(t.ID), SpaceID: uuidString(t.SpaceID), Name: t.Name, Kind: string(t.Kind), ExtKind: t.ExtKind.String,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (r *Registry) handleAppCommentCounts(w http.ResponseWriter, req *http.Request) {
	rq := appRequest(req)
	counts, err := r.commentCounts(req.Context(), rq.node.ID)
	if err != nil {
		fail(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, counts)
}

// handleAppNotify is POST …/notify: tell somebody who can open the node.
// Their bell words it with the manifest's template for the verb.
func (r *Registry) handleAppNotify(w http.ResponseWriter, req *http.Request) {
	rq := appRequest(req)
	var body struct {
		To      string `json:"to"`
		Verb    string `json:"verb"`
		Excerpt string `json:"excerpt"`
		Subject string `json:"subject"`
	}
	if !decodeBody(w, req, &body) {
		return
	}
	to, err := parseUUID(body.To)
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "to must be a user id")
		return
	}
	if !apppkg.ValidVerb(body.Verb) {
		writeProblem(w, http.StatusBadRequest, "verb must match "+apppkg.VerbPattern)
		return
	}
	ctx := req.Context()
	subject := ""
	if body.Subject != "" {
		ok, err := r.recordSubject(ctx, rq.node.ID, body.Subject)
		if err != nil {
			fail(w, err)
			return
		}
		if !ok {
			writeProblem(w, http.StatusBadRequest, "subject must be record:<id> of a record of this item")
			return
		}
		subject = body.Subject
	}
	if err := r.notify(ctx, notice{
		appID: rq.e.m.ID, to: to, actor: rq.user, node: rq.node.ID, verb: body.Verb,
		excerpt: body.Excerpt, subject: subject,
	}); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
