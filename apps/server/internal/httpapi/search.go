package httpapi

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

const (
	// Shorter than two characters matches most of the drive, which is slower
	// to render than it is useful to read.
	minQueryLen = 2
	// The palette shows a page, not a report. Anything past this is noise the
	// user should narrow instead of scroll.
	defaultSearchLimit = 20
	maxSearchLimit     = 50
)

// searchResultDTO is a node plus the two things a result needs that a listing
// row does not: which space it lives in and which folder holds it. A result
// list spans spaces, so "Q1 forecast.xlsx" alone does not identify a file.
type searchResultDTO struct {
	nodeDTO
	SpaceName  string `json:"space_name"`
	ParentName string `json:"parent_name"`
}

// searchQuery is the parsed, validated form of the query string.
type searchQuery struct {
	query         string
	spaceID       pgtype.UUID
	kind          store.NullNodeKind
	ownerID       pgtype.UUID
	modifiedAfter pgtype.Timestamptz
	limit         int32
	// tooShort marks a query below minQueryLen. Not an error: the palette
	// calls the endpoint on every keystroke, and the first one or two are
	// always below the minimum.
	tooShort bool
}

// parseSearchQuery validates the query string. The error it returns is the
// message sent to the client, so it says what the caller should change.
func parseSearchQuery(v url.Values) (searchQuery, error) {
	out := searchQuery{
		query: strings.TrimSpace(v.Get("q")),
		limit: defaultSearchLimit,
	}
	// Counted in runes: a two-character CJK query is two characters, not six.
	out.tooShort = len([]rune(out.query)) < minQueryLen

	if s := v.Get("space"); s != "" {
		if err := out.spaceID.Scan(s); err != nil {
			return out, errors.New("invalid space id")
		}
	}

	if o := v.Get("owner"); o != "" {
		if err := out.ownerID.Scan(o); err != nil {
			return out, errors.New("invalid owner id")
		}
	}

	// Accepts either a window in days ("modified=7") or an explicit RFC 3339
	// instant. The days form is what the filter chips send; the instant form
	// keeps the endpoint useful to anything else.
	if m := v.Get("modified"); m != "" {
		if days, err := strconv.Atoi(m); err == nil {
			if days < 1 {
				return out, errors.New("modified must be a positive number of days")
			}
			out.modifiedAfter = pgtype.Timestamptz{
				Time:  time.Now().AddDate(0, 0, -days),
				Valid: true,
			}
		} else {
			t, err := time.Parse(time.RFC3339, m)
			if err != nil {
				return out, errors.New("modified must be a day count or an RFC 3339 timestamp")
			}
			out.modifiedAfter = pgtype.Timestamptz{Time: t, Valid: true}
		}
	}

	switch k := v.Get("kind"); k {
	case "":
	case string(store.NodeKindDir), string(store.NodeKindFile):
		out.kind = store.NullNodeKind{NodeKind: store.NodeKind(k), Valid: true}
	default:
		return out, errors.New("kind must be dir|file")
	}

	if l := v.Get("limit"); l != "" {
		n, err := strconv.Atoi(l)
		if err != nil || n < 1 {
			return out, errors.New("limit must be a positive integer")
		}
		out.limit = int32(min(n, maxSearchLimit))
	}

	return out, nil
}

// handleSearch answers GET /api/v1/search?q=&space=&kind=&limit=.
//
// Scope is the caller's space membership, enforced by joining space_members in
// the query rather than filtering afterwards — a result the user may not see
// is never fetched in the first place. Direct shares do not widen search:
// a shared node is reachable through "Shared with me", and widening here would
// mean walking every ancestor chain per candidate row.
func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	u := s.requireUser(w, r)
	if u == nil {
		return
	}

	q, err := parseSearchQuery(r.URL.Query())
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request", err.Error())
		return
	}
	if q.tooShort {
		writeShortQueryResult(w)
		return
	}

	rows, err := s.fs.Search(r.Context(), store.SearchNodesParams{
		UserID:        u.ID,
		SpaceID:       q.spaceID,
		Kind:          q.kind,
		OwnerID:       q.ownerID,
		ModifiedAfter: q.modifiedAfter,
		Query:         q.query,
		ResultLimit:   q.limit,
	})
	if err != nil {
		Error(w, r, err)
		return
	}

	out := make([]searchResultDTO, 0, len(rows))
	for _, n := range rows {
		out = append(out, searchRowToDTO(n))
	}
	writeJSON(w, http.StatusOK, out)
}

func writeShortQueryResult(w http.ResponseWriter) {
	writeJSON(w, http.StatusOK, []searchResultDTO{})
}

func searchRowToDTO(n store.SearchNodesRow) searchResultDTO {
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
