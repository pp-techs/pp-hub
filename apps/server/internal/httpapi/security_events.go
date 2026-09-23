package httpapi

import (
	"encoding/csv"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nhatbui/pp-hub/apps/server/internal/audit"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

// One row of the security log, as the admin console reads it.
type securityEventDTO struct {
	ID        string          `json:"id"`
	Kind      string          `json:"kind"`
	UserID    string          `json:"user_id,omitempty"`
	UserEmail string          `json:"user_email,omitempty"`
	UserName  string          `json:"user_name,omitempty"`
	Subject   string          `json:"subject,omitempty"`
	IP        string          `json:"ip,omitempty"`
	UserAgent string          `json:"user_agent,omitempty"`
	RequestID string          `json:"request_id,omitempty"`
	NodeID    string          `json:"node_id,omitempty"`
	NodeName  string          `json:"node_name,omitempty"`
	Detail    json.RawMessage `json:"detail,omitempty"`
	CreatedAt string          `json:"created_at"`
}

type securityEventPage struct {
	Events []securityEventDTO `json:"events"`
	Total  int64              `json:"total"`
}

func securityEventToDTO(e store.ListSecurityEventsRow) securityEventDTO {
	d := securityEventDTO{
		ID:        e.ID.String(),
		Kind:      e.Kind,
		UserEmail: e.UserEmail,
		UserName:  e.UserName,
		Subject:   e.Subject,
		IP:        e.Ip,
		UserAgent: e.UserAgent,
		RequestID: e.RequestID,
		NodeName:  e.NodeName,
		CreatedAt: e.CreatedAt.Time.Format(timeLayout),
	}
	if e.UserID.Valid {
		d.UserID = e.UserID.String()
	}
	if e.NodeID.Valid {
		d.NodeID = e.NodeID.String()
	}
	// `{}` carries nothing; omitting it keeps the wire shape honest about
	// which events actually have extras.
	if len(e.Detail) > 0 && string(e.Detail) != "{}" {
		d.Detail = json.RawMessage(e.Detail)
	}
	return d
}

// securityEventFilter is what the console and the export both narrow by.
// `To` is exclusive, so a day range is [midnight, next midnight).
type securityEventFilter struct {
	Kind     pgtype.Text
	Node     pgtype.UUID
	From, To pgtype.Timestamptz
}

// parseSecurityEventFilter reads ?kind, ?node, ?from and ?to. The second
// return is a problem detail, empty when the query is valid. Unlike ?limit,
// a bad filter is refused: silently dropping it would answer a narrower
// question with the whole log.
func parseSecurityEventFilter(r *http.Request) (securityEventFilter, string) {
	q := r.URL.Query()
	var f securityEventFilter
	if k := q.Get("kind"); k != "" {
		f.Kind = pgtype.Text{String: k, Valid: true}
	}
	if n := q.Get("node"); n != "" {
		if err := f.Node.Scan(n); err != nil {
			return f, "invalid node id"
		}
	}
	for _, p := range []struct {
		name string
		dst  *pgtype.Timestamptz
	}{{"from", &f.From}, {"to", &f.To}} {
		raw := q.Get(p.name)
		if raw == "" {
			continue
		}
		t, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return f, "invalid " + p.name
		}
		*p.dst = pgtype.Timestamptz{Time: t, Valid: true}
	}
	return f, ""
}

// handleAdminSecurityEvents pages the security log, newest first.
//
// Admin-only and deliberately not scoped to a space: the events it holds —
// sign-ins and their failures, admin and suspension changes, grants — belong
// to the instance, and the whole value of a run of failed sign-ins is seeing
// it across accounts and addresses rather than one at a time.
func (s *Server) handleAdminSecurityEvents(w http.ResponseWriter, r *http.Request) {
	if s.requireAdmin(w, r) == nil {
		return
	}
	limit := clampQueryInt(r, "limit", 100, 1, 500)
	offset := clampQueryInt(r, "offset", 0, 0, 1<<20)
	f, bad := parseSecurityEventFilter(r)
	if bad != "" {
		writeProblem(w, http.StatusBadRequest, "Bad Request", bad)
		return
	}

	rows, err := s.store.ListSecurityEvents(r.Context(), store.ListSecurityEventsParams{
		Limit:  int32(limit),
		Offset: int32(offset),
		Kind:   f.Kind,
		Node:   f.Node,
		FromAt: f.From,
		ToAt:   f.To,
	})
	if err != nil {
		Error(w, r, err)
		return
	}
	total, err := s.store.CountSecurityEvents(r.Context(), store.CountSecurityEventsParams{
		Kind:   f.Kind,
		Node:   f.Node,
		FromAt: f.From,
		ToAt:   f.To,
	})
	if err != nil {
		Error(w, r, err)
		return
	}

	out := make([]securityEventDTO, 0, len(rows))
	for _, row := range rows {
		out = append(out, securityEventToDTO(row))
	}
	writeJSON(w, http.StatusOK, securityEventPage{Events: out, Total: total})
}

// exportBatch is how many rows the export reads per keyset page.
const exportBatch = 1000

var securityEventCSVHeader = []string{
	"time", "kind", "user_id", "user_email", "user_name", "subject", "ip",
	"user_agent", "request_id", "node_id", "node_name", "detail",
}

// handleAdminSecurityEventsExport streams the filtered security log as CSV.
//
// Paged by keyset rather than offset, so rows written during a long export
// neither shift nor duplicate what is already out. The first page is read
// before any header is written, so a failing query still answers with a
// proper error; after that the response is committed, and a later failure
// can only be marked in the body — the same compromise as the zip stream.
// The export itself is an audited event: the log leaving the building is
// something the log should know about.
func (s *Server) handleAdminSecurityEventsExport(w http.ResponseWriter, r *http.Request) {
	admin := s.requireAdmin(w, r)
	if admin == nil {
		return
	}
	f, bad := parseSecurityEventFilter(r)
	if bad != "" {
		writeProblem(w, http.StatusBadRequest, "Bad Request", bad)
		return
	}
	params := store.ListSecurityEventsExportParams{
		Kind:   f.Kind,
		Node:   f.Node,
		FromAt: f.From,
		ToAt:   f.To,
		Lim:    exportBatch,
	}
	rows, err := s.store.ListSecurityEventsExport(r.Context(), params)
	if err != nil {
		Error(w, r, err)
		return
	}

	detail := map[string]any{}
	q := r.URL.Query()
	for _, k := range []string{"kind", "node", "from", "to"} {
		if v := q.Get(k); v != "" {
			detail[k] = v
		}
	}
	s.audit.Record(r, audit.Event{
		Kind:    audit.KindSecurityLogExported,
		UserID:  admin.ID,
		Subject: admin.Email,
		Detail:  detail,
	})

	name := "security-events-" + time.Now().UTC().Format("20060102-150405") + ".csv"
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", contentDisposition("attachment", name))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store")

	// The BOM is what makes Excel read the file as UTF-8 rather than the
	// system code page, which would mangle Vietnamese names.
	_, _ = w.Write([]byte("\ufeff"))
	cw := csv.NewWriter(w)
	_ = cw.Write(securityEventCSVHeader)
	for {
		for _, row := range rows {
			e := securityEventToDTO(store.ListSecurityEventsRow(row))
			_ = cw.Write([]string{
				csvSafe(e.CreatedAt), csvSafe(e.Kind), csvSafe(e.UserID), csvSafe(e.UserEmail),
				csvSafe(e.UserName), csvSafe(e.Subject), csvSafe(e.IP), csvSafe(e.UserAgent),
				csvSafe(e.RequestID), csvSafe(e.NodeID), csvSafe(e.NodeName), csvSafe(string(e.Detail)),
			})
		}
		cw.Flush()
		if len(rows) < exportBatch {
			return
		}
		last := rows[len(rows)-1]
		params.BeforeAt, params.BeforeID = last.CreatedAt, last.ID
		rows, err = s.store.ListSecurityEventsExport(r.Context(), params)
		if err != nil {
			slog.ErrorContext(r.Context(), "security log export failed mid-stream", "err", err)
			_, _ = w.Write([]byte("[pp-hub] export incomplete\n"))
			return
		}
	}
}

// csvSafe defuses spreadsheet formula injection: a cell a spreadsheet would
// evaluate — a subject or user agent is attacker-chosen text — is prefixed
// with an apostrophe so it reads as a literal. Mirrors the web's csvCell.
func csvSafe(v string) string {
	if v != "" && strings.ContainsRune("=+-@\t\r", rune(v[0])) {
		return "'" + v
	}
	return v
}

// clampQueryInt reads a bounded integer query parameter, falling back to the
// default on anything unparseable rather than erroring: a bad `?limit=` is a
// client bug, and refusing the whole page teaches the caller nothing a
// clamped page would not.
func clampQueryInt(r *http.Request, name string, def, min, max int) int {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return def
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return def
	}
	if n < min {
		return min
	}
	if n > max {
		return max
	}
	return n
}
