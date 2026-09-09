package httpapi

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
)

// ErrNotFound is the first typed error of the taxonomy in
// docs/code-standard/backend-conventions.md. Add ErrPermission/ErrQuota/...
// as modules land.
var ErrNotFound = errors.New("not found")

// problem is an RFC 7807 problem+json body.
type problem struct {
	Type   string `json:"type"`
	Title  string `json:"title"`
	Status int    `json:"status"`
	Detail string `json:"detail,omitempty"`
}

func writeProblem(w http.ResponseWriter, status int, title, detail string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(problem{
		Type:   "about:blank",
		Title:  title,
		Status: status,
		Detail: detail,
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// Error maps module errors onto problem+json responses.
func Error(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		writeProblem(w, http.StatusNotFound, "Not Found", err.Error())
	default:
		slog.ErrorContext(r.Context(), "internal error", "err", err)
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "")
	}
}
