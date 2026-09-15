package httpapi

import (
	"encoding/json"
	"net/http"
	"sort"
	"strconv"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nhatbui/pp-hub/apps/server/internal/spaces"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

const (
	defaultFeedLimit = 25
	maxFeedLimit     = 100
)

// activityDTO is one line of the feed. The actor's display name and the node
// name travel with the row rather than being joined client-side, because the
// node may no longer exist by the time anyone reads the entry.
type activityDTO struct {
	ID        string         `json:"id"`
	SpaceID   string         `json:"space_id"`
	SpaceName string         `json:"space_name"`
	Actor     string         `json:"actor"`
	ActorID   string         `json:"actor_id"`
	NodeID    string         `json:"node_id,omitempty"`
	NodeName  string         `json:"node_name,omitempty"`
	Kind      string         `json:"kind"`
	Detail    map[string]any `json:"detail,omitempty"`
	CreatedAt string         `json:"created_at"`
}

// recentDTO is a node the caller themselves last touched, carrying what they
// did to it so the listing can say "you edited" rather than just a timestamp.
type recentDTO struct {
	searchResultDTO
	LastAction   string `json:"last_action"`
	LastActionAt string `json:"last_action_at"`
}

// usageCategoryDTO is one slice of the storage breakdown.
type usageCategoryDTO struct {
	Category string `json:"category"`
	Bytes    int64  `json:"bytes"`
	Files    int64  `json:"files"`
}

func feedLimit(r *http.Request) int32 {
	n, err := strconv.Atoi(r.URL.Query().Get("limit"))
	if err != nil || n < 1 {
		return defaultFeedLimit
	}
	return int32(min(n, maxFeedLimit))
}

// handleListActivity answers GET /api/v1/activity[?space=].
func (s *Server) handleListActivity(w http.ResponseWriter, r *http.Request) {
	u := s.requireUser(w, r)
	if u == nil {
		return
	}
	limit := feedLimit(r)

	// Membership decides which feed you may ask for; the grants decide which
	// events in it you may see. Both branches filter — the space_members join
	// in ListMyActivity only narrows to the caller's companies.
	if raw := r.URL.Query().Get("space"); raw != "" {
		var spaceID pgtype.UUID
		if err := spaceID.Scan(raw); err != nil {
			writeProblem(w, http.StatusBadRequest, "Bad Request", "invalid space id")
			return
		}
		if err := s.spaces.RequireRole(r.Context(), spaceID, u.ID, spaces.RoleMember); err != nil {
			s.spaceError(w, r, err)
			return
		}
		rows, err := s.activity.ForSpace(r.Context(), spaceID, limit)
		if err != nil {
			Error(w, r, err)
			return
		}
		rows, err = reachableActivity(r.Context(), s, u.ID, rows,
			func(a store.ListSpaceActivityRow) pgtype.UUID { return a.NodeID },
			func(a store.ListSpaceActivityRow) pgtype.UUID { return a.SpaceID },
			func(a store.ListSpaceActivityRow) pgtype.UUID { return a.ActorID })
		if err != nil {
			Error(w, r, err)
			return
		}
		out := make([]activityDTO, 0, len(rows))
		for _, a := range rows {
			out = append(out, spaceActivityToDTO(a))
		}
		writeJSON(w, http.StatusOK, out)
		return
	}

	rows, err := s.activity.ForUser(r.Context(), u.ID, limit)
	if err != nil {
		Error(w, r, err)
		return
	}
	rows, err = reachableActivity(r.Context(), s, u.ID, rows,
		func(a store.ListMyActivityRow) pgtype.UUID { return a.NodeID },
		func(a store.ListMyActivityRow) pgtype.UUID { return a.SpaceID },
		func(a store.ListMyActivityRow) pgtype.UUID { return a.ActorID })
	if err != nil {
		Error(w, r, err)
		return
	}
	out := make([]activityDTO, 0, len(rows))
	for _, a := range rows {
		out = append(out, myActivityToDTO(a))
	}
	writeJSON(w, http.StatusOK, out)
}

// handleListRecent answers GET /api/v1/recent — files the caller themselves
// last touched, newest first.
func (s *Server) handleListRecent(w http.ResponseWriter, r *http.Request) {
	u := s.requireUser(w, r)
	if u == nil {
		return
	}
	rows, err := s.activity.Recent(r.Context(), u.ID)
	if err != nil {
		Error(w, r, err)
		return
	}
	// Scoped by membership in SQL, which only narrows to the caller's
	// companies. Losing access to a folder must also drop its files out of
	// "recent", so the grants decide here.
	rows, err = reachableRows(r.Context(), s, u.ID, rows,
		func(n store.ListRecentNodesRow) pgtype.UUID { return n.ID },
		func(n store.ListRecentNodesRow) pgtype.UUID { return n.SpaceID })
	if err != nil {
		Error(w, r, err)
		return
	}

	// DISTINCT ON must order by the node id, so the rows arrive grouped rather
	// than sorted by time. Re-sort here and trim to the requested page.
	sortRecentByTime(rows)
	limit := int(feedLimit(r))
	if len(rows) > limit {
		rows = rows[:limit]
	}

	out := make([]recentDTO, 0, len(rows))
	for _, n := range rows {
		out = append(out, recentRowToDTO(n))
	}
	writeJSON(w, http.StatusOK, out)
}

// handleListStarred answers GET /api/v1/starred.
func (s *Server) handleListStarred(w http.ResponseWriter, r *http.Request) {
	u := s.requireUser(w, r)
	if u == nil {
		return
	}
	rows, err := s.fs.Starred(r.Context(), u.ID, feedLimit(r))
	if err != nil {
		Error(w, r, err)
		return
	}
	// A star outlives access, which is why the query rechecks membership. That
	// recheck now has to be a grant check: starring already went through
	// requireNodeAccess, so a node reachable only by grant could be starred and
	// then never appear here.
	rows, err = reachableRows(r.Context(), s, u.ID, rows,
		func(n store.ListStarredNodesRow) pgtype.UUID { return n.ID },
		func(n store.ListStarredNodesRow) pgtype.UUID { return n.SpaceID })
	if err != nil {
		Error(w, r, err)
		return
	}
	out := make([]searchResultDTO, 0, len(rows))
	for _, n := range rows {
		out = append(out, starredRowToDTO(n))
	}
	writeJSON(w, http.StatusOK, out)
}

// handleStarNode and handleUnstarNode toggle the caller's own bookmark. A star
// needs only read access — it is the user's private note about the node, not a
// change to it.
func (s *Server) handleStarNode(w http.ResponseWriter, r *http.Request) {
	u, node := s.requireNodeAccess(w, r, 1)
	if u == nil {
		return
	}
	if err := s.fs.Star(r.Context(), u.ID, node.ID); err != nil {
		Error(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleUnstarNode(w http.ResponseWriter, r *http.Request) {
	u, node := s.requireNodeAccess(w, r, 1)
	if u == nil {
		return
	}
	if err := s.fs.Unstar(r.Context(), u.ID, node.ID); err != nil {
		Error(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleUsage answers GET /api/v1/usage[?space=] with the storage breakdown.
func (s *Server) handleUsage(w http.ResponseWriter, r *http.Request) {
	u := s.requireUser(w, r)
	if u == nil {
		return
	}
	var spaceID pgtype.UUID
	if raw := r.URL.Query().Get("space"); raw != "" {
		if err := spaceID.Scan(raw); err != nil {
			writeProblem(w, http.StatusBadRequest, "Bad Request", "invalid space id")
			return
		}
		if err := s.spaces.RequireRole(r.Context(), spaceID, u.ID, spaces.RoleMember); err != nil {
			s.spaceError(w, r, err)
			return
		}
	}

	rows, err := s.fs.UsageByCategory(r.Context(), u.ID, spaceID)
	if err != nil {
		Error(w, r, err)
		return
	}
	out := make([]usageCategoryDTO, 0, len(rows))
	for _, c := range rows {
		out = append(out, usageCategoryDTO{Category: c.Category, Bytes: c.Bytes, Files: c.Files})
	}
	writeJSON(w, http.StatusOK, out)
}

// --- mapping ---

func actorName(display, name, email string) string {
	if display != "" {
		return display
	}
	if name != "" {
		return name
	}
	return email
}

func spaceActivityToDTO(a store.ListSpaceActivityRow) activityDTO {
	return activityRow{
		ID: a.ID, SpaceID: a.SpaceID, SpaceName: a.SpaceName,
		ActorID: a.ActorID, ActorName: a.ActorName,
		ActorDisplayName: a.ActorDisplayName, ActorEmail: a.ActorEmail,
		NodeID: a.NodeID, NodeName: a.NodeName, Kind: a.Kind,
		Detail: a.Detail, CreatedAt: a.CreatedAt,
	}.dto()
}

func myActivityToDTO(a store.ListMyActivityRow) activityDTO {
	return activityRow{
		ID: a.ID, SpaceID: a.SpaceID, SpaceName: a.SpaceName,
		ActorID: a.ActorID, ActorName: a.ActorName,
		ActorDisplayName: a.ActorDisplayName, ActorEmail: a.ActorEmail,
		NodeID: a.NodeID, NodeName: a.NodeName, Kind: a.Kind,
		Detail: a.Detail, CreatedAt: a.CreatedAt,
	}.dto()
}

// activityRow is the shape both feed queries share. sqlc generates a distinct
// struct per query, so one small adapter keeps the mapping in one place.
type activityRow struct {
	ID               pgtype.UUID
	SpaceID          pgtype.UUID
	SpaceName        string
	ActorID          pgtype.UUID
	ActorName        string
	ActorDisplayName string
	ActorEmail       string
	NodeID           pgtype.UUID
	NodeName         string
	Kind             string
	Detail           []byte
	CreatedAt        pgtype.Timestamptz
}

func (a activityRow) dto() activityDTO {
	id, _ := a.ID.Value()
	sid, _ := a.SpaceID.Value()
	aid, _ := a.ActorID.Value()
	out := activityDTO{
		ID:        id.(string),
		SpaceID:   sid.(string),
		SpaceName: a.SpaceName,
		ActorID:   aid.(string),
		Actor:     actorName(a.ActorDisplayName, a.ActorName, a.ActorEmail),
		NodeName:  a.NodeName,
		Kind:      a.Kind,
		CreatedAt: a.CreatedAt.Time.Format(timeLayout),
	}
	if a.NodeID.Valid {
		if n, _ := a.NodeID.Value(); n != nil {
			out.NodeID = n.(string)
		}
	}
	out.Detail = decodeDetail(a.Detail)
	return out
}

// decodeDetail turns the stored JSONB into a map, treating anything
// unparseable as absent — a malformed detail must not break the whole feed.
func decodeDetail(raw []byte) map[string]any {
	if len(raw) == 0 {
		return nil
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil || len(out) == 0 {
		return nil
	}
	return out
}

// sortRecentByTime puts the most recently touched node first. The query has to
// ORDER BY node id for DISTINCT ON, so the time ordering is applied here.
func sortRecentByTime(rows []store.ListRecentNodesRow) {
	sort.Slice(rows, func(i, j int) bool {
		return rows[i].LastActionAt.Time.After(rows[j].LastActionAt.Time)
	})
}

// nodeParts is the column set the search, starred and recent queries share.
type nodeParts struct {
	ID         pgtype.UUID
	SpaceID    pgtype.UUID
	ParentID   pgtype.UUID
	Kind       store.NodeKind
	Name       string
	CreatedAt  pgtype.Timestamptz
	VID        pgtype.UUID
	VSize      pgtype.Int8
	Mime       pgtype.Text
	VersionNo  pgtype.Int4
	VCreatedAt pgtype.Timestamptz
	OwnerUid   pgtype.UUID
	OwnerName  pgtype.Text
	OwnerEmail pgtype.Text
	SpaceName  string
	ParentName string
}

func (n nodeParts) dto() searchResultDTO {
	id, _ := n.ID.Value()
	sid, _ := n.SpaceID.Value()
	dto := nodeDTO{
		ID:        id.(string),
		SpaceID:   sid.(string),
		Kind:      string(n.Kind),
		Name:      n.Name,
		CreatedAt: n.CreatedAt.Time.Format(timeLayout),
	}
	if n.ParentID.Valid {
		if p, _ := n.ParentID.Value(); p != nil {
			dto.ParentID = p.(string)
		}
	}
	if n.VID.Valid {
		dto.Size = n.VSize.Int64
		dto.Mime = n.Mime.String
		dto.Version = n.VersionNo.Int32
		dto.ModifiedAt = n.VCreatedAt.Time.Format(timeLayout)
	}
	if n.Kind == store.NodeKindDir && dto.ModifiedAt == "" {
		dto.ModifiedAt = n.CreatedAt.Time.Format(timeLayout)
	}
	if n.OwnerName.Valid && n.OwnerName.String != "" {
		dto.Owner = n.OwnerName.String
	} else if n.OwnerEmail.Valid {
		dto.Owner = n.OwnerEmail.String
	}
	if n.OwnerUid.Valid {
		if o, _ := n.OwnerUid.Value(); o != nil {
			dto.OwnerID = o.(string)
		}
	}
	return searchResultDTO{nodeDTO: dto, SpaceName: n.SpaceName, ParentName: n.ParentName}
}

func starredRowToDTO(n store.ListStarredNodesRow) searchResultDTO {
	return nodeParts{
		ID: n.ID, SpaceID: n.SpaceID, ParentID: n.ParentID, Kind: n.Kind, Name: n.Name,
		CreatedAt: n.CreatedAt, VID: n.VID, VSize: n.VSize, Mime: n.Mime,
		VersionNo: n.VersionNo, VCreatedAt: n.VCreatedAt, OwnerUid: n.OwnerUid,
		OwnerName: n.OwnerName, OwnerEmail: n.OwnerEmail,
		SpaceName: n.SpaceName, ParentName: n.ParentName,
	}.dto()
}

func recentRowToDTO(n store.ListRecentNodesRow) recentDTO {
	return recentDTO{
		searchResultDTO: nodeParts{
			ID: n.ID, SpaceID: n.SpaceID, ParentID: n.ParentID, Kind: n.Kind, Name: n.Name,
			CreatedAt: n.CreatedAt, VID: n.VID, VSize: n.VSize, Mime: n.Mime,
			VersionNo: n.VersionNo, VCreatedAt: n.VCreatedAt, OwnerUid: n.OwnerUid,
			OwnerName: n.OwnerName, OwnerEmail: n.OwnerEmail,
			SpaceName: n.SpaceName, ParentName: n.ParentName,
		}.dto(),
		LastAction:   n.LastAction,
		LastActionAt: n.LastActionAt.Time.Format(timeLayout),
	}
}
