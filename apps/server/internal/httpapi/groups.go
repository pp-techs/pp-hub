package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nhatbui/pp-hub/apps/server/internal/groups"
	"github.com/nhatbui/pp-hub/apps/server/internal/spaces"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

// timeFormat matches the layout every other DTO in this package formats with.
const timeFormat = "2006-01-02T15:04:05Z07:00"

// --- DTOs ---

type groupDTO struct {
	ID          string `json:"id"`
	SpaceID     string `json:"space_id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	// IsEveryone marks the automatic group: the UI must not offer rename,
	// delete, or membership editing for it.
	IsEveryone  bool   `json:"is_everyone"`
	MemberCount int32  `json:"member_count"`
	GrantCount  int32  `json:"grant_count"`
	CreatedAt   string `json:"created_at"`
}

type groupMemberDTO struct {
	UserID      string `json:"user_id"`
	Email       string `json:"email"`
	Name        string `json:"name"`
	DisplayName string `json:"display_name,omitempty"`
	AvatarURL   string `json:"avatar_url,omitempty"`
	AddedAt     string `json:"added_at"`
}

type grantDTO struct {
	ID         string `json:"id"`
	GroupID    string `json:"group_id"`
	GroupName  string `json:"group_name"`
	IsEveryone bool   `json:"is_everyone"`
	Perm       string `json:"perm"`
	CreatedAt  string `json:"created_at"`
}

func groupRowToDTO(g store.ListGroupsRow) groupDTO {
	return groupDTO{
		ID:          uuidString(g.ID),
		SpaceID:     uuidString(g.SpaceID),
		Name:        g.Name,
		Description: g.Description,
		IsEveryone:  g.IsEveryone,
		MemberCount: g.MemberCount,
		GrantCount:  g.GrantCount,
		CreatedAt:   g.CreatedAt.Time.Format(timeFormat),
	}
}

// groupError maps the service's typed errors onto status codes.
func (s *Server) groupError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, groups.ErrNotFound):
		writeProblem(w, http.StatusNotFound, "Not Found", "no such group")
	case errors.Is(err, groups.ErrEveryoneImmutable):
		writeProblem(w, http.StatusConflict, "Conflict", err.Error())
	case errors.Is(err, groups.ErrNameTaken):
		writeProblem(w, http.StatusConflict, "Conflict", err.Error())
	case errors.Is(err, groups.ErrNameEmpty):
		writeProblem(w, http.StatusBadRequest, "Bad Request", err.Error())
	case errors.Is(err, groups.ErrCrossSpace):
		// 404 rather than 403: the caller should not learn that a group exists
		// in a space they were not asking about.
		writeProblem(w, http.StatusNotFound, "Not Found", "no such group")
	default:
		Error(w, r, err)
	}
}

// requireSpaceAdmin resolves {spaceID} and demands the admin role.
func (s *Server) requireSpaceAdmin(w http.ResponseWriter, r *http.Request) (*store.User, pgtype.UUID) {
	u := s.requireUser(w, r)
	if u == nil {
		return nil, pgtype.UUID{}
	}
	spaceID, err := uuidParam(r, "spaceID")
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "invalid space id")
		return nil, pgtype.UUID{}
	}
	if err := s.spaces.RequireRole(r.Context(), spaceID, u.ID, spaces.RoleAdmin); err != nil {
		s.spaceError(w, r, err)
		return nil, pgtype.UUID{}
	}
	return u, spaceID
}

// groupInSpace loads {groupID} and refuses one belonging to another space, so a
// group id cannot be used to reach across the company boundary.
func (s *Server) groupInSpace(w http.ResponseWriter, r *http.Request, spaceID pgtype.UUID) (store.Group, bool) {
	groupID, err := uuidParam(r, "groupID")
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "invalid group id")
		return store.Group{}, false
	}
	g, err := s.groups.Get(r.Context(), groupID)
	if err != nil {
		s.groupError(w, r, err)
		return store.Group{}, false
	}
	if g.SpaceID != spaceID {
		writeProblem(w, http.StatusNotFound, "Not Found", "no such group")
		return store.Group{}, false
	}
	return g, true
}

// --- Group handlers ---

// handleListGroups is readable by any member: you should be able to see which
// groups exist in your company, even if you cannot change them.
func (s *Server) handleListGroups(w http.ResponseWriter, r *http.Request) {
	u := s.requireUser(w, r)
	if u == nil {
		return
	}
	spaceID, err := uuidParam(r, "spaceID")
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "invalid space id")
		return
	}
	if err := s.spaces.RequireRole(r.Context(), spaceID, u.ID, spaces.RoleMember); err != nil {
		s.spaceError(w, r, err)
		return
	}
	list, err := s.groups.List(r.Context(), spaceID)
	if err != nil {
		Error(w, r, err)
		return
	}
	out := make([]groupDTO, 0, len(list))
	for _, g := range list {
		out = append(out, groupRowToDTO(g))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleCreateGroup(w http.ResponseWriter, r *http.Request) {
	u, spaceID := s.requireSpaceAdmin(w, r)
	if u == nil {
		return
	}
	var body struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "invalid body")
		return
	}
	g, err := s.groups.Create(r.Context(), spaceID, body.Name, body.Description, u.ID)
	if err != nil {
		s.groupError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, groupDTO{
		ID:          uuidString(g.ID),
		SpaceID:     uuidString(g.SpaceID),
		Name:        g.Name,
		Description: g.Description,
		CreatedAt:   g.CreatedAt.Time.Format(timeFormat),
	})
}

func (s *Server) handleUpdateGroup(w http.ResponseWriter, r *http.Request) {
	u, spaceID := s.requireSpaceAdmin(w, r)
	if u == nil {
		return
	}
	g, ok := s.groupInSpace(w, r, spaceID)
	if !ok {
		return
	}
	var body struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "invalid body")
		return
	}
	updated, err := s.groups.Update(r.Context(), g.ID, body.Name, body.Description)
	if err != nil {
		s.groupError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, groupDTO{
		ID:          uuidString(updated.ID),
		SpaceID:     uuidString(updated.SpaceID),
		Name:        updated.Name,
		Description: updated.Description,
		CreatedAt:   updated.CreatedAt.Time.Format(timeFormat),
	})
}

// handleDeleteGroup reports how many folders the group opened, because the
// grants cascade away with it and the people in it lose that access silently.
func (s *Server) handleDeleteGroup(w http.ResponseWriter, r *http.Request) {
	u, spaceID := s.requireSpaceAdmin(w, r)
	if u == nil {
		return
	}
	g, ok := s.groupInSpace(w, r, spaceID)
	if !ok {
		return
	}
	revoked, err := s.groups.GrantCount(r.Context(), g.ID)
	if err != nil {
		Error(w, r, err)
		return
	}
	if err := s.groups.Delete(r.Context(), g.ID); err != nil {
		s.groupError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":         "deleted",
		"grants_revoked": revoked,
	})
}

func (s *Server) handleListGroupMembers(w http.ResponseWriter, r *http.Request) {
	u := s.requireUser(w, r)
	if u == nil {
		return
	}
	spaceID, err := uuidParam(r, "spaceID")
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "invalid space id")
		return
	}
	if err := s.spaces.RequireRole(r.Context(), spaceID, u.ID, spaces.RoleMember); err != nil {
		s.spaceError(w, r, err)
		return
	}
	g, ok := s.groupInSpace(w, r, spaceID)
	if !ok {
		return
	}
	list, err := s.groups.Members(r.Context(), g.ID)
	if err != nil {
		Error(w, r, err)
		return
	}
	out := make([]groupMemberDTO, 0, len(list))
	for _, m := range list {
		out = append(out, groupMemberDTO{
			UserID:      uuidString(m.ID),
			Email:       m.Email,
			Name:        m.Name,
			DisplayName: m.DisplayName,
			AvatarURL:   m.AvatarUrl,
			AddedAt:     m.AddedAt.Time.Format(timeFormat),
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleAddGroupMember(w http.ResponseWriter, r *http.Request) {
	u, spaceID := s.requireSpaceAdmin(w, r)
	if u == nil {
		return
	}
	g, ok := s.groupInSpace(w, r, spaceID)
	if !ok {
		return
	}
	var body struct {
		UserID string `json:"user_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "invalid body")
		return
	}
	var userID pgtype.UUID
	if err := userID.Scan(body.UserID); err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "invalid user id")
		return
	}
	// A group belongs to one company, so it may only contain that company's
	// people. Without this a group grant would carry an outsider into the
	// space's files.
	if err := s.spaces.RequireRole(r.Context(), spaceID, userID, spaces.RoleMember); err != nil {
		writeProblem(w, http.StatusUnprocessableEntity, "Unprocessable", "that person is not a member of this space")
		return
	}
	if err := s.groups.AddMember(r.Context(), g.ID, userID); err != nil {
		s.groupError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"status": "added"})
}

func (s *Server) handleRemoveGroupMember(w http.ResponseWriter, r *http.Request) {
	u, spaceID := s.requireSpaceAdmin(w, r)
	if u == nil {
		return
	}
	g, ok := s.groupInSpace(w, r, spaceID)
	if !ok {
		return
	}
	userID, err := uuidParam(r, "userID")
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "invalid user id")
		return
	}
	if err := s.groups.RemoveMember(r.Context(), g.ID, userID); err != nil {
		s.groupError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "removed"})
}

// --- Grant handlers ---

// handleListGrants shows which groups open a folder. Rank 1 is enough: if you
// can see the folder you may see who else can.
func (s *Server) handleListGrants(w http.ResponseWriter, r *http.Request) {
	u, node := s.requireNodeAccess(w, r, 1)
	if u == nil {
		return
	}
	list, err := s.groups.GrantsForNode(r.Context(), node.ID)
	if err != nil {
		Error(w, r, err)
		return
	}
	out := make([]grantDTO, 0, len(list))
	for _, g := range list {
		out = append(out, grantDTO{
			ID:         uuidString(g.ID),
			GroupID:    uuidString(g.GroupID),
			GroupName:  g.GroupName,
			IsEveryone: g.IsEveryone,
			Perm:       string(g.Perm),
			CreatedAt:  g.CreatedAt.Time.Format(timeFormat),
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// handleUpsertGrant gives a group access to a folder.
//
// Gated on the *space admin* role rather than on rank 3 on the node. Someone
// with edit on one folder can still share it directly or by link, but handing a
// whole group standing access is administration of the company, and letting it
// spread from whoever holds edit puts the shape of access outside the admin's
// control.
func (s *Server) handleUpsertGrant(w http.ResponseWriter, r *http.Request) {
	u, node := s.requireNodeAccess(w, r, 1)
	if u == nil {
		return
	}
	if err := s.spaces.RequireRole(r.Context(), node.SpaceID, u.ID, spaces.RoleAdmin); err != nil {
		writeProblem(w, http.StatusForbidden, "Forbidden", "only a space admin can grant group access")
		return
	}
	var body struct {
		GroupID string `json:"group_id"`
		Perm    string `json:"perm"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "invalid body")
		return
	}
	var groupID pgtype.UUID
	if err := groupID.Scan(body.GroupID); err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "invalid group id")
		return
	}
	perm := store.SharePerm(body.Perm)
	switch perm {
	case store.SharePermView, store.SharePermDownload, store.SharePermEdit:
	default:
		writeProblem(w, http.StatusBadRequest, "Bad Request", "perm must be view|download|edit")
		return
	}
	sh, err := s.groups.Grant(r.Context(), node, groupID, perm, u.ID)
	if err != nil {
		s.groupError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"id":       uuidString(sh.ID),
		"group_id": uuidString(groupID),
		"perm":     string(sh.Perm),
	})
}

func (s *Server) handleRevokeGrant(w http.ResponseWriter, r *http.Request) {
	u, node := s.requireNodeAccess(w, r, 1)
	if u == nil {
		return
	}
	if err := s.spaces.RequireRole(r.Context(), node.SpaceID, u.ID, spaces.RoleAdmin); err != nil {
		writeProblem(w, http.StatusForbidden, "Forbidden", "only a space admin can revoke group access")
		return
	}
	groupID, err := uuidParam(r, "groupID")
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "invalid group id")
		return
	}
	if err := s.groups.Revoke(r.Context(), node.ID, groupID); err != nil {
		Error(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "revoked"})
}
