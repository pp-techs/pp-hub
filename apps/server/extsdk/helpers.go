package extsdk

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

// MustRequest returns what Guard established. A handler on a route with no
// Guard in front of it is a wiring bug and must fail loudly rather than serve
// a node unchecked, so it panics.
func MustRequest(r *http.Request) Request {
	v, ok := RequestFrom(r.Context())
	if !ok {
		panic("extsdk: handler runs outside Guard")
	}
	return v
}

type conflictError struct{ msg string }

func (e *conflictError) Error() string { return e.msg }

// Conflict is an error Fail answers with 409 and msg.
func Conflict(msg string) error { return &conflictError{msg} }

// Fail answers a handler's error: a Conflict is 409, pgx.ErrNoRows 404, and
// anything else 500 (logged, and not shown to the client).
func Fail(w http.ResponseWriter, err error) {
	var ce *conflictError
	switch {
	case errors.As(err, &ce):
		WriteProblem(w, http.StatusConflict, ce.msg)
	case errors.Is(err, pgx.ErrNoRows):
		WriteProblem(w, http.StatusNotFound, "not found")
	default:
		slog.Error("extension handler failed", "error", err)
		WriteProblem(w, http.StatusInternalServerError, "")
	}
}

// Trim trims v and checks it holds at most max characters (and something, when
// required), answering 400 itself when it does not.
func Trim(w http.ResponseWriter, v, field string, max int, required bool) (string, bool) {
	v = strings.TrimSpace(v)
	if required && v == "" {
		WriteProblem(w, http.StatusBadRequest, field+" is required")
		return "", false
	}
	if utf8.RuneCountInString(v) > max {
		WriteProblem(w, http.StatusBadRequest, field+" is too long")
		return "", false
	}
	return v, true
}
