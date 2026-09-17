package httpapi

import (
	"encoding/json"
	"net/http"

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
	u := s.requireUser(w, r)
	if u == nil {
		return
	}
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

func (s *Server) handleAddMember(w http.ResponseWriter, r *http.Request) {
	u := s.requireUser(w, r)
	if u == nil {
		return
	}
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
	if body.Email == "" {
		writeProblem(w, http.StatusBadRequest, "Error", "email is required")
		return
	}
	user, err := s.store.GetUserByEmail(r.Context(), body.Email)
	if err != nil {
		writeProblem(w, http.StatusNotFound, "Error", "no user with that email")
		return
	}
	if err := s.spaces.AddMember(r.Context(), spaceID, user.ID, body.AsAdmin); err != nil {
		writeProblem(w, http.StatusInternalServerError, "Error", "add failed")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"status": "added"})
}

func (s *Server) handleSetMemberAdmin(w http.ResponseWriter, r *http.Request) {
	u := s.requireUser(w, r)
	if u == nil {
		return
	}
	spaceID, err := uuidParam(r, "spaceID")
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Error", "invalid space id")
		return
	}
	userID, err := uuidParam(r, "userID")
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Error", "invalid user id")
		return
	}
	if err := s.spaces.RequireAdmin(r.Context(), spaceID, u.ID); err != nil {
		s.spaceError(w, r, err)
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
	// SetAdmin owns the last-admin guard: AddMember can only ever add, so
	// demoting through it silently did nothing.
	if err := s.spaces.SetAdmin(r.Context(), spaceID, userID, *body.AsAdmin); err != nil {
		s.spaceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

func (s *Server) handleRemoveMember(w http.ResponseWriter, r *http.Request) {
	u := s.requireUser(w, r)
	if u == nil {
		return
	}
	spaceID, err := uuidParam(r, "spaceID")
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Error", "invalid space id")
		return
	}
	userID, err := uuidParam(r, "userID")
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Error", "invalid user id")
		return
	}
	if err := s.spaces.RequireAdmin(r.Context(), spaceID, u.ID); err != nil {
		s.spaceError(w, r, err)
		return
	}
	// RemoveMember owns the last-admin guard and the archive hand-off; a
	// second copy of the count here could only drift from it.
	if err := s.spaces.RemoveMember(r.Context(), spaceID, userID, u.ID); err != nil {
		s.spaceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "removed"})
}
