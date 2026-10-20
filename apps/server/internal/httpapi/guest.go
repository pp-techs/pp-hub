package httpapi

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/nhatbui/pp-hub/apps/server/internal/audit"
	"github.com/nhatbui/pp-hub/apps/server/internal/guest"
	"github.com/nhatbui/pp-hub/apps/server/internal/share"
)

// Guest sign-in (docs/architecture/guest-access.md): an address is proved
// with a one-time code, with no identity provider and no account to create.

type guestConfigDTO struct {
	Enabled bool `json:"enabled"`
}

// handleGuestConfig tells the sign-in screen whether to offer a code. It is
// public and says only whether the instance admits guests at all.
func (s *Server) handleGuestConfig(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, guestConfigDTO{Enabled: s.guest.Enabled()})
}

type guestCodeBody struct {
	Email string `json:"email"`
	Code  string `json:"code"`
}

func readGuestBody(w http.ResponseWriter, r *http.Request) (guestCodeBody, bool) {
	var body guestCodeBody
	r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "invalid body")
		return body, false
	}
	return body, true
}

// handleGuestCode asks for a code. The answer is 204 whether or not one was
// sent — the address may have nothing shared with it, a real account may use
// it, or it may be over its hourly allowance — so the form is not a way to
// learn who was shared with. The audit row records which it was.
func (s *Server) handleGuestCode(w http.ResponseWriter, r *http.Request) {
	body, ok := readGuestBody(w, r)
	if !ok {
		return
	}
	email := share.NormalizeEmail(body.Email)
	if email == "" || len(email) > 254 {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "an email address is required")
		return
	}
	sent := s.guest.RequestCode(r.Context(), email)
	s.audit.Record(r, audit.Event{
		Kind:    audit.KindGuestCodeRequested,
		Subject: email,
		Detail:  map[string]any{"sent": sent},
	})
	w.WriteHeader(http.StatusNoContent)
}

// handleGuestVerify checks a code and starts a guest session. Invitations
// made to the address at view level become grants first, so the guest lands
// on something.
func (s *Server) handleGuestVerify(w http.ResponseWriter, r *http.Request) {
	body, ok := readGuestBody(w, r)
	if !ok {
		return
	}
	email := share.NormalizeEmail(body.Email)
	u, err := s.guest.Verify(r.Context(), email, body.Code)
	if errors.Is(err, guest.ErrInvalidCode) {
		s.audit.Record(r, audit.Event{Kind: audit.KindGuestVerifyFailed, Subject: email})
		writeProblem(w, http.StatusUnprocessableEntity, "Unprocessable", err.Error())
		return
	}
	if err != nil {
		Error(w, r, err)
		return
	}
	if u.SuspendedAt.Valid {
		writeProblem(w, http.StatusForbidden, "Forbidden", "this account is suspended")
		return
	}
	if s.personal != nil {
		s.personal.Adopt(r, u, u.Email, true)
	}
	if err := s.auth.IssueGuestSession(r.Context(), w, r, u.ID); err != nil {
		slog.ErrorContext(r.Context(), "guest session", "user", u.Email, "err", err)
		Error(w, r, err)
		return
	}
	s.audit.Record(r, audit.Event{Kind: audit.KindGuestVerified, UserID: u.ID, Subject: u.Email})
	w.WriteHeader(http.StatusNoContent)
}
