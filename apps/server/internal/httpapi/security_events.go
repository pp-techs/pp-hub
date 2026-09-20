package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/jackc/pgx/v5/pgtype"

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
		CreatedAt: e.CreatedAt.Time.Format(timeLayout),
	}
	if e.UserID.Valid {
		d.UserID = e.UserID.String()
	}
	// `{}` carries nothing; omitting it keeps the wire shape honest about
	// which events actually have extras.
	if len(e.Detail) > 0 && string(e.Detail) != "{}" {
		d.Detail = json.RawMessage(e.Detail)
	}
	return d
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

	var kind pgtype.Text
	if k := r.URL.Query().Get("kind"); k != "" {
		kind = pgtype.Text{String: k, Valid: true}
	}

	rows, err := s.store.ListSecurityEvents(r.Context(), store.ListSecurityEventsParams{
		Limit:  int32(limit),
		Offset: int32(offset),
		Kind:   kind,
	})
	if err != nil {
		Error(w, r, err)
		return
	}
	total, err := s.store.CountSecurityEvents(r.Context(), kind)
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
