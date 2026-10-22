package exthost

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nhatbui/pp-hub/apps/server/internal/share"
)

// Cap is one capability, ordered as the permission model orders them.
type Cap int

const (
	CapView Cap = iota
	CapEdit
)

// Caps are the capabilities a person holds on a node, as an app sees them
// (the bridge's context.caps and a backend token's caps claim).
type Caps struct {
	View     bool `json:"view"`
	Download bool `json:"download"`
	Share    bool `json:"share"`
	Edit     bool `json:"edit"`
	Owner    bool `json:"owner"`
}

// Has reports whether the set includes want.
func (c Caps) Has(want Cap) bool {
	switch want {
	case CapView:
		return c.View
	case CapEdit:
		return c.Edit
	}
	return false
}

func toCaps(c share.Caps) Caps {
	return Caps{View: c.View, Download: c.Download, Share: c.Share, Edit: c.Edit, Owner: c.Owner}
}

// uuidString is the canonical text of an id, or "" when it is not set.
func uuidString(id pgtype.UUID) string {
	if !id.Valid {
		return ""
	}
	v, _ := id.Value()
	s, _ := v.(string)
	return s
}

func parseUUID(s string) (pgtype.UUID, error) {
	var id pgtype.UUID
	err := id.Scan(s)
	return id, err
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeProblem answers with an RFC 7807 problem, as the rest of the API does.
func writeProblem(w http.ResponseWriter, status int, detail string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"type":   "about:blank",
		"title":  http.StatusText(status),
		"status": status,
		"detail": detail,
	})
}

// fail answers a handler's error: no rows is 404, anything else 500 (logged,
// not shown to the client).
func fail(w http.ResponseWriter, err error) {
	if errors.Is(err, pgx.ErrNoRows) {
		writeProblem(w, http.StatusNotFound, "not found")
		return
	}
	slog.Error("app request failed", "error", err)
	writeProblem(w, http.StatusInternalServerError, "")
}

// maxBodyBytes bounds a JSON request body read through decodeBody.
const maxBodyBytes = 1 << 20

// decodeBody reads a JSON body into v, answering 400 itself on failure.
func decodeBody(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		writeProblem(w, http.StatusBadRequest, "invalid JSON body")
		return false
	}
	return true
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
