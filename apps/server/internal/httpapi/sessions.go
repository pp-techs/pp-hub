package httpapi

import (
	"net/http"

	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

// A live login, as shown on the account's Sessions tab.
//
// The token is not here and cannot be: the database holds only its hash. What
// a person can do from this list is recognise a session and end it.
type sessionDTO struct {
	ID         string `json:"id"`
	UserAgent  string `json:"user_agent"`
	IP         string `json:"ip"`
	CreatedAt  string `json:"created_at"`
	LastUsedAt string `json:"last_used_at"`
	ExpiresAt  string `json:"expires_at"`
	Current    bool   `json:"current"`
}

func sessionToDTO(s store.Session, current bool) sessionDTO {
	return sessionDTO{
		ID:         s.ID.String(),
		UserAgent:  s.UserAgent,
		IP:         s.Ip,
		CreatedAt:  s.CreatedAt.Time.Format(timeLayout),
		LastUsedAt: s.LastUsedAt.Time.Format(timeLayout),
		ExpiresAt:  s.ExpiresAt.Time.Format(timeLayout),
		Current:    current,
	}
}

func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	sess := callerSession(r)
	rows, err := s.auth.ListSessions(r.Context(), sess.User.ID)
	if err != nil {
		Error(w, r, err)
		return
	}
	out := make([]sessionDTO, 0, len(rows))
	for _, row := range rows {
		out = append(out, sessionToDTO(row, row.ID == sess.ID))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleRevokeSession(w http.ResponseWriter, r *http.Request) {
	sess := callerSession(r)
	id, err := uuidParam(r, "sessionID")
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "invalid session id")
		return
	}
	ok, err := s.auth.RevokeSession(r, sess.User, id)
	if err != nil {
		Error(w, r, err)
		return
	}
	if !ok {
		writeProblem(w, http.StatusNotFound, "Not Found", "no such session")
		return
	}
	// Revoking the session you are holding is signing out. The cookie has to
	// go with the row, or the browser keeps presenting a handle to something
	// that no longer exists.
	if id == sess.ID {
		s.auth.ClearSessionCookie(w)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleRevokeOtherSessions(w http.ResponseWriter, r *http.Request) {
	sess := callerSession(r)
	if _, err := s.auth.RevokeOtherSessions(r, sess.User, sess.ID); err != nil {
		Error(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
