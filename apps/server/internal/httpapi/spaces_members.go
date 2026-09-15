package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nhatbui/pp-hub/apps/server/internal/spaces"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

// --- Space Member handlers ---

type spaceMemberDTO struct {
	UserID      string `json:"user_id"`
	Email       string `json:"email"`
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	AvatarURL   string `json:"avatar_url"`
	Role        string `json:"role"`
	CreatedAt   string `json:"created_at"`
	// Live files this member created in this space.
	FilesOwned int64 `json:"files_owned"`
	// Last sign-in, empty for a member who has never signed in.
	LastSeen string `json:"last_seen,omitempty"`
}

type addMemberRequest struct {
	UserID string `json:"user_id"`
	Role   string `json:"role"`
}

type updateMemberRequest struct {
	Role string `json:"role"`
}

func memberToDTO(r store.ListSpaceMembersDetailedRow) spaceMemberDTO {
	dto := spaceMemberDTO{
		UserID:      uuidString(r.UserID),
		Email:       r.Email,
		Name:        r.UserName,
		DisplayName: r.DisplayName,
		AvatarURL:   r.AvatarUrl,
		Role:        string(r.Role),
		CreatedAt:   r.CreatedAt.Time.Format(timeLayout),
		FilesOwned:  r.FilesOwned,
	}
	if r.LastSeen.Valid {
		dto.LastSeen = r.LastSeen.Time.Format(timeLayout)
	}
	return dto
}

// handleListMembers returns all members of a space.
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
	// Check access: any member can list members
	if err := s.spaces.RequireRole(r.Context(), spaceID, u.ID, spaces.RoleMember); err != nil {
		writeProblem(w, http.StatusForbidden, "Error", "not a space member")
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

// handleAddMember adds a user to a space.
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
	// Admin only
	if err := s.spaces.RequireRole(r.Context(), spaceID, u.ID, spaces.RoleAdmin); err != nil {
		writeProblem(w, http.StatusForbidden, "Error", "admin required")
		return
	}
	var req addMemberRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeProblem(w, http.StatusBadRequest, "Error", "invalid body")
		return
	}
	var userID pgtype.UUID
	if err := userID.Scan(req.UserID); err != nil {
		writeProblem(w, http.StatusBadRequest, "Error", "invalid user id")
		return
	}
	role := spaces.Role(req.Role)
	if role != spaces.RoleMember && role != spaces.RoleAdmin {
		writeProblem(w, http.StatusBadRequest, "Error", "invalid role")
		return
	}
	if err := s.spaces.AddMember(r.Context(), spaceID, userID, role); err != nil {
		writeProblem(w, http.StatusInternalServerError, "Error", "add failed")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"status": "added"})
}

// handleUpdateMemberRole changes a member's role.
func (s *Server) handleUpdateMemberRole(w http.ResponseWriter, r *http.Request) {
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
	// Admin only
	if err := s.spaces.RequireRole(r.Context(), spaceID, u.ID, spaces.RoleAdmin); err != nil {
		writeProblem(w, http.StatusForbidden, "Error", "admin required")
		return
	}
	var req updateMemberRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeProblem(w, http.StatusBadRequest, "Error", "invalid body")
		return
	}
	role := spaces.Role(req.Role)
	if role != spaces.RoleMember && role != spaces.RoleAdmin {
		writeProblem(w, http.StatusBadRequest, "Error", "invalid role")
		return
	}
	// Prevent removing the last admin
	if role != spaces.RoleAdmin {
		members, err := s.spaces.Members(r.Context(), spaceID)
		if err == nil {
			adminCount := 0
			for _, m := range members {
				if m.Role == spaces.RoleAdmin {
					adminCount++
				}
			}
			targetRole, _ := s.spaces.RoleOf(r.Context(), spaceID, userID)
			if adminCount <= 1 && targetRole == spaces.RoleAdmin {
				writeProblem(w, http.StatusConflict, "Error", "cannot remove the last admin")
				return
			}
		}
	}
	if err := s.spaces.AddMember(r.Context(), spaceID, userID, role); err != nil {
		writeProblem(w, http.StatusInternalServerError, "Error", "update failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

// handleRemoveMember removes a member from a space.
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
	// Admin only
	if err := s.spaces.RequireRole(r.Context(), spaceID, u.ID, spaces.RoleAdmin); err != nil {
		writeProblem(w, http.StatusForbidden, "Error", "admin required")
		return
	}
	// Prevent removing the last admin
	members, err := s.spaces.Members(r.Context(), spaceID)
	if err == nil {
		adminCount := 0
		for _, m := range members {
			if m.Role == spaces.RoleAdmin {
				adminCount++
			}
		}
		targetRole, _ := s.spaces.RoleOf(r.Context(), spaceID, userID)
		if adminCount <= 1 && targetRole == spaces.RoleAdmin {
			writeProblem(w, http.StatusConflict, "Error", "cannot remove the last admin")
			return
		}
	}
	if err := s.spaces.RemoveMember(r.Context(), spaceID, userID); err != nil {
		writeProblem(w, http.StatusInternalServerError, "Error", "remove failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "removed"})
}
