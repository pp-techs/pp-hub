package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/nhatbui/pp-hub/apps/server/internal/audit"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

type spaceMemberDTO struct {
	UserID      string `json:"user_id"`
	Email       string `json:"email"`
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	AvatarURL   string `json:"avatar_url"`
	IsAdmin     bool   `json:"is_admin"`
	CreatedAt   string `json:"created_at"`
	FilesOwned  int64  `json:"files_owned"`
	LastSeen    string `json:"last_seen,omitempty"`
}

func memberToDTO(r store.ListSpaceMembersDetailedRow) spaceMemberDTO {
	dto := spaceMemberDTO{
		UserID:      uuidString(r.UserID),
		Email:       r.Email,
		Name:        r.UserName,
		DisplayName: r.DisplayName,
		AvatarURL:   r.AvatarUrl,
		IsAdmin:     r.IsAdmin,
		CreatedAt:   r.CreatedAt.Time.Format(timeLayout),
		FilesOwned:  r.FilesOwned,
	}
	if r.LastSeen.Valid {
		dto.LastSeen = r.LastSeen.Time.Format(timeLayout)
	}
	return dto
}

func (s *Server) handleListMembers(w http.ResponseWriter, r *http.Request) {
	u := caller(r)
	spaceID, err := uuidParam(r, "spaceID")
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Error", "invalid space id")
		return
	}
	if err := s.spaces.RequireMember(r.Context(), spaceID, u.ID); err != nil {
		s.spaceError(w, r, err)
		return
	}
	members, err := s.spaces.Members(r.Context(), spaceID)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "Error", "list failed")
		return
	}
	out := make([]spaceMemberDTO, 0, len(members))
	for _, m := range members {
		out = append(out, memberToDTO(m))
	}
	writeJSON(w, http.StatusOK, out)
}

// handleAddMember puts an address into the space's organization.
//
// It takes an email rather than a user id because the identity provider owns
// the directory: the address may belong to somebody who has never signed in
// here, or to nobody yet. Those are the two answers this endpoint gives —
// 201 added, 202 invited — and the caller renders different sentences for
// them.
func (s *Server) handleAddMember(w http.ResponseWriter, r *http.Request) {
	u := caller(r)
	spaceID, err := uuidParam(r, "spaceID")
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Error", "invalid space id")
		return
	}
	if err := s.spaces.RequireAdmin(r.Context(), spaceID, u.ID); err != nil {
		s.spaceError(w, r, err)
		return
	}
	var body struct {
		Email   string `json:"email"`
		AsAdmin bool   `json:"as_admin"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeProblem(w, http.StatusBadRequest, "Error", "invalid body")
		return
	}
	body.Email = strings.TrimSpace(body.Email)
	if body.Email == "" {
		writeProblem(w, http.StatusBadRequest, "Error", "email is required")
		return
	}
	space, err := s.spaces.Get(r.Context(), spaceID)
	if err != nil {
		s.spaceError(w, r, err)
		return
	}
	if s.refusePersonal(w, space) {
		return
	}
	outcome, err := s.orgs.Add(r.Context(), space, body.Email, body.AsAdmin)
	if err != nil {
		s.spaceError(w, r, err)
		return
	}
	if outcome.Invited {
		s.audit.Record(r, audit.Event{
			Kind:    audit.KindMemberInvited,
			Subject: u.Email,
			Detail:  map[string]any{"space": spaceID.String(), "invitee": outcome.Email, "as_admin": body.AsAdmin},
		})
		writeJSON(w, http.StatusAccepted, map[string]string{"status": "invited", "email": outcome.Email})
		return
	}
	s.audit.Record(r, audit.Event{
		Kind:    audit.KindMemberAdded,
		UserID:  outcome.UserID,
		Subject: u.Email,
		Detail:  map[string]any{"space": spaceID.String(), "as_admin": body.AsAdmin},
	})
	writeJSON(w, http.StatusCreated, map[string]string{
		"status": "added", "user_id": uuidString(outcome.UserID),
	})
}

func (s *Server) handleSetMemberAdmin(w http.ResponseWriter, r *http.Request) {
	u := caller(r)
	space, target, ok := s.memberTarget(w, r)
	if !ok {
		return
	}
	// A pointer, not a bool: `{"is_admin":true}` or any other typo would
	// otherwise decode to false and silently demote the person the caller
	// meant to promote.
	var body struct {
		AsAdmin *bool `json:"as_admin"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.AsAdmin == nil {
		writeProblem(w, http.StatusBadRequest, "Error", "as_admin must be true or false")
		return
	}
	// The last-admin refusal belongs on this path and only this path: a
	// request may be declined, a fact arriving from the identity provider
	// may not.
	if !*body.AsAdmin {
		if err := s.spaces.RequireNotLastAdmin(r.Context(), space.ID, target.ID); err != nil {
			s.spaceError(w, r, err)
			return
		}
	}
	if err := s.orgs.SetAdmin(r.Context(), space, target, *body.AsAdmin); err != nil {
		s.spaceError(w, r, err)
		return
	}
	s.audit.Record(r, audit.Event{
		Kind:    audit.KindAdminGranted,
		UserID:  target.ID,
		Subject: u.Email,
		Detail:  map[string]any{"space": space.ID.String(), "as_admin": *body.AsAdmin},
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

// handleRemoveMember takes somebody out of the organization and revokes their
// access here.
//
// It does not hand over what they left behind. That is destructive and
// irreversible, so it waits for a second, explicit confirmation — see
// handleCompleteDeparture.
func (s *Server) handleRemoveMember(w http.ResponseWriter, r *http.Request) {
	u := caller(r)
	space, target, ok := s.memberTarget(w, r)
	if !ok {
		return
	}
	if err := s.spaces.RequireNotLastAdmin(r.Context(), space.ID, target.ID); err != nil {
		s.spaceError(w, r, err)
		return
	}
	if err := s.orgs.Remove(r.Context(), space, target); err != nil {
		s.spaceError(w, r, err)
		return
	}
	// Membership is what gates every grant and every ownership claim, so
	// losing it is the single largest change to what a person can reach.
	s.audit.Record(r, audit.Event{
		Kind:    audit.KindMemberRemoved,
		UserID:  target.ID,
		Subject: u.Email,
		Detail:  map[string]any{"space": space.ID.String()},
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "removed"})
}

// memberTarget resolves the {spaceID}/{userID} pair every membership mutation
// needs, having first established that the caller administers the space. It
// writes the problem response itself and reports whether the handler may
// continue.
func (s *Server) memberTarget(w http.ResponseWriter, r *http.Request) (store.Space, store.User, bool) {
	u := caller(r)
	spaceID, err := uuidParam(r, "spaceID")
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Error", "invalid space id")
		return store.Space{}, store.User{}, false
	}
	userID, err := uuidParam(r, "userID")
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Error", "invalid user id")
		return store.Space{}, store.User{}, false
	}
	if err := s.spaces.RequireAdmin(r.Context(), spaceID, u.ID); err != nil {
		s.spaceError(w, r, err)
		return store.Space{}, store.User{}, false
	}
	space, err := s.spaces.Get(r.Context(), spaceID)
	if err != nil {
		s.spaceError(w, r, err)
		return store.Space{}, store.User{}, false
	}
	if s.refusePersonal(w, space) {
		return store.Space{}, store.User{}, false
	}
	target, err := s.store.GetUser(r.Context(), userID)
	if err != nil {
		writeProblem(w, http.StatusNotFound, "Not Found", "no such user")
		return store.Space{}, store.User{}, false
	}
	return space, target, true
}
