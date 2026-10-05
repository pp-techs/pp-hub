package httpapi

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nhatbui/pp-hub/apps/server/internal/audit"
	"github.com/nhatbui/pp-hub/apps/server/internal/groups"
	"github.com/nhatbui/pp-hub/apps/server/internal/notify"
	"github.com/nhatbui/pp-hub/apps/server/internal/share"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

type groupDTO struct {
	ID          string `json:"id"`
	SpaceID     string `json:"space_id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	IsAdmin     bool   `json:"is_admin,omitempty"`
	IsEveryone  bool   `json:"is_everyone,omitempty"`
	MemberCount int32  `json:"member_count"`
	GrantCount  int32  `json:"grant_count"`
	CreatedAt   string `json:"created_at"`
}

type groupMemberDTO struct {
	UserID      string `json:"user_id"`
	Email       string `json:"email"`
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	AvatarURL   string `json:"avatar_url"`
	AddedAt     string `json:"added_at"`
}

// grantDTO is a group's standing access to one node, on the M15 capability
// model: a level plus the two flags. `perm` is not emitted — it no longer
// exists in the database.
type grantDTO struct {
	ID          string `json:"id"`
	GroupID     string `json:"group_id"`
	GroupName   string `json:"group_name"`
	IsAdmin     bool   `json:"is_admin,omitempty"`
	IsEveryone  bool   `json:"is_everyone,omitempty"`
	Level       string `json:"level"`
	CanDownload bool   `json:"can_download"`
	CanShare    bool   `json:"can_share"`
}

func groupRowToDTO(g store.ListGroupsRow) groupDTO {
	return groupDTO{
		ID:          uuidString(g.ID),
		SpaceID:     uuidString(g.SpaceID),
		Name:        g.Name,
		Description: g.Description,
		IsAdmin:     g.IsAdmin,
		IsEveryone:  g.IsEveryone,
		MemberCount: g.MemberCount,
		GrantCount:  g.GrantCount,
		CreatedAt:   g.CreatedAt.Time.Format(timeLayout),
	}
}

func groupToDTO(g store.Group) groupDTO {
	return groupDTO{
		ID:          uuidString(g.ID),
		SpaceID:     uuidString(g.SpaceID),
		Name:        g.Name,
		Description: g.Description,
		IsAdmin:     g.IsAdmin,
		IsEveryone:  g.IsEveryone,
		CreatedAt:   g.CreatedAt.Time.Format(timeLayout),
	}
}

func grantRowToDTO(g store.ListGrantsForNodeRow) grantDTO {
	return grantDTO{
		ID:          uuidString(g.ID),
		GroupID:     uuidString(g.GroupID),
		GroupName:   g.GroupName,
		IsAdmin:     g.IsAdmin,
		IsEveryone:  g.IsEveryone,
		Level:       string(g.Level),
		CanDownload: g.CanDownload,
		CanShare:    g.CanShare,
	}
}

// groupError maps the group service's refusals. The automatic groups answer
// 403 wherever they are touched — rename, delete, hand-edited membership —
// so the rule lives here instead of being re-checked at each handler.
func (s *Server) groupError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, groups.ErrNotFound):
		writeProblem(w, http.StatusNotFound, "Not Found", err.Error())
	case errors.Is(err, groups.ErrSystemGroupImmutable):
		writeProblem(w, http.StatusForbidden, "Forbidden", err.Error())
	case errors.Is(err, groups.ErrNameTaken):
		writeProblem(w, http.StatusConflict, "Conflict", err.Error())
	case errors.Is(err, groups.ErrNameEmpty):
		writeProblem(w, http.StatusBadRequest, "Bad Request", err.Error())
	case errors.Is(err, groups.ErrCrossSpace):
		writeProblem(w, http.StatusUnprocessableEntity, "Unprocessable", err.Error())
	default:
		Error(w, r, err)
	}
}

// requireSpaceAdmin resolves {spaceID} and demands Admin-group membership.
//
// Callers MUST honour the ok flag: continuing after a refusal would write a
// second response body over the 403 this already sent.
func (s *Server) requireSpaceAdmin(w http.ResponseWriter, r *http.Request) (*store.User, store.Space, bool) {
	u := caller(r)
	spaceID, err := uuidParam(r, "spaceID")
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "invalid space id")
		return nil, store.Space{}, false
	}
	if err := s.spaces.RequireAdmin(r.Context(), spaceID, u.ID); err != nil {
		s.spaceError(w, r, err)
		return nil, store.Space{}, false
	}
	space, err := s.spaces.Get(r.Context(), spaceID)
	if err != nil {
		s.spaceError(w, r, err)
		return nil, store.Space{}, false
	}
	return u, space, true
}

// groupInSpace loads {groupID} and refuses one belonging to another space, so
// a group id cannot be used to reach across the company boundary.
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

// recordGroup writes a group change to the security log. Group membership is
// access by proxy — every grant the group holds follows it — so it is audited
// like a grant is.
func (s *Server) recordGroup(r *http.Request, u *store.User, kind audit.Kind, g store.Group, detail map[string]any) {
	d := map[string]any{"space": g.SpaceID.String(), "group": g.ID.String(), "group_name": g.Name}
	for k, v := range detail {
		d[k] = v
	}
	s.audit.Record(r, audit.Event{Kind: kind, UserID: u.ID, Subject: u.Email, Detail: d})
}

// --- Group handlers ---

// handleListGroups is readable by any member: granting a group access to a
// folder you own needs the group's name and id, and the members panel already
// shows who is in the company. Creating or editing a group stays admin-only.
func (s *Server) handleListGroups(w http.ResponseWriter, r *http.Request) {
	u := caller(r)
	spaceID, err := uuidParam(r, "spaceID")
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "invalid space id")
		return
	}
	if err := s.spaces.RequireMember(r.Context(), spaceID, u.ID); err != nil {
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
	u, space, ok := s.requireOrgSpaceAdmin(w, r)
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
	g, err := s.groups.Create(r.Context(), space.ID, body.Name, body.Description, u.ID)
	if err != nil {
		s.groupError(w, r, err)
		return
	}
	s.recordGroup(r, u, audit.KindGroupCreated, g, nil)
	writeJSON(w, http.StatusCreated, groupToDTO(g))
}

func (s *Server) handleUpdateGroup(w http.ResponseWriter, r *http.Request) {
	u, space, ok := s.requireOrgSpaceAdmin(w, r)
	if !ok {
		return
	}
	g, ok := s.groupInSpace(w, r, space.ID)
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
	// The automatic groups are refused in groups.Service and mapped by
	// groupError; no pre-check here.
	updated, err := s.groups.Update(r.Context(), g.ID, body.Name, body.Description)
	if err != nil {
		s.groupError(w, r, err)
		return
	}
	s.recordGroup(r, u, audit.KindGroupUpdated, updated, map[string]any{"from": g.Name})
	writeJSON(w, http.StatusOK, groupToDTO(updated))
}

// handleDeleteGroup reports how many folders the group opened, because the
// grants cascade away with it and the people in it lose that access silently.
func (s *Server) handleDeleteGroup(w http.ResponseWriter, r *http.Request) {
	u, space, ok := s.requireOrgSpaceAdmin(w, r)
	if !ok {
		return
	}
	g, ok := s.groupInSpace(w, r, space.ID)
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
	s.recordGroup(r, u, audit.KindGroupDeleted, g, map[string]any{"grants_revoked": revoked})
	writeJSON(w, http.StatusOK, map[string]any{
		"status":         "deleted",
		"grants_revoked": revoked,
	})
}

func (s *Server) handleListGroupMembers(w http.ResponseWriter, r *http.Request) {
	_, space, ok := s.requireSpaceAdmin(w, r)
	if !ok {
		return
	}
	g, ok := s.groupInSpace(w, r, space.ID)
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
			AddedAt:     m.AddedAt.Time.Format(timeLayout),
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// handleAddGroupMember puts a space member in a user-created group. Admin
// membership is not editable here — it is the admin role, and it changes
// through POST /members/{userID}/set-admin.
//
// An admin may not add THEMSELVES to a group that already opens files. Group
// membership is admin-only, so without this rule it was the one path by which
// being an admin became reading a colleague's work — the thing the model says
// admin never buys. Another admin can still do it, and every change is in the
// security log.
func (s *Server) handleAddGroupMember(w http.ResponseWriter, r *http.Request) {
	u, space, ok := s.requireOrgSpaceAdmin(w, r)
	if !ok {
		return
	}
	g, ok := s.groupInSpace(w, r, space.ID)
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
	if err := s.spaces.RequireMember(r.Context(), space.ID, userID); err != nil {
		writeProblem(w, http.StatusUnprocessableEntity, "Unprocessable", "that person is not a member of this space")
		return
	}
	if userID == u.ID {
		grants, err := s.groups.GrantCount(r.Context(), g.ID)
		if err != nil {
			Error(w, r, err)
			return
		}
		if grants > 0 {
			writeProblem(w, http.StatusForbidden, "Forbidden",
				"this group opens files, and being an admin never grants file access on its own — another admin has to add you")
			return
		}
	}
	if err := s.groups.AddMember(r.Context(), g.ID, userID); err != nil {
		s.groupError(w, r, err)
		return
	}
	s.recordGroup(r, u, audit.KindGroupMemberAdded, g, map[string]any{"member": userID.String()})
	writeJSON(w, http.StatusCreated, map[string]string{"status": "added"})
}

func (s *Server) handleRemoveGroupMember(w http.ResponseWriter, r *http.Request) {
	u, space, ok := s.requireOrgSpaceAdmin(w, r)
	if !ok {
		return
	}
	g, ok := s.groupInSpace(w, r, space.ID)
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
	s.recordGroup(r, u, audit.KindGroupMemberRemoved, g, map[string]any{"member": userID.String()})
	writeJSON(w, http.StatusOK, map[string]string{"status": "removed"})
}

// --- Node grant handlers ---

// grantCapsFromBody validates the wire's capability triple. Level is the only
// enumerated field; the flags are independent booleans, and Normalized closes
// the gap the database CHECKs would otherwise reject (an editor can always
// download what it edits).
func grantCapsFromBody(level string, canDownload, canShare bool) (share.GrantCaps, bool) {
	var lvl store.ShareLevel
	switch level {
	case "view":
		lvl = share.LevelView
	case "edit":
		lvl = share.LevelEdit
	default:
		return share.GrantCaps{}, false
	}
	return share.GrantCaps{Level: lvl, CanDownload: canDownload, CanShare: canShare}.Normalized(), true
}

func (s *Server) handleListGrants(w http.ResponseWriter, r *http.Request) {
	u, node, _ := s.requireNodeCaps(w, r, share.CapShare)
	if u == nil {
		return
	}
	rows, err := s.groups.GrantsForNode(r.Context(), node.ID)
	if err != nil {
		Error(w, r, err)
		return
	}
	out := make([]grantDTO, 0, len(rows))
	for _, g := range rows {
		out = append(out, grantRowToDTO(g))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleUpsertGrant(w http.ResponseWriter, r *http.Request) {
	u, node, caps := s.requireNodeCaps(w, r, share.CapShare)
	if u == nil {
		return
	}
	var body struct {
		GroupID     string `json:"group_id"`
		Level       string `json:"level"`
		CanDownload bool   `json:"can_download"`
		CanShare    bool   `json:"can_share"`
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
	want, ok := grantCapsFromBody(body.Level, body.CanDownload, body.CanShare)
	if !ok {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "level must be view|edit")
		return
	}
	// Monotonicity: you can only hand out what you hold yourself, or a
	// download-only grantee could mint an editor.
	if !caps.Covers(want) {
		writeProblem(w, http.StatusForbidden, "Forbidden", share.ErrEscalation.Error())
		return
	}
	existing, found, err := s.groupGrant(r, node.ID, groupID)
	if err != nil {
		Error(w, r, err)
		return
	}
	isNew := !found
	if found {
		// Replacing a grant is changing someone else's access: it takes the
		// same bound as revoking it.
		if !caps.Covers(grantRowCaps(existing)) {
			writeProblem(w, http.StatusForbidden, "Forbidden", errGrantWider)
			return
		}
		if !s.keepsTeamManager(w, r, node, grantRowIsManager(existing), want.CanShare) {
			return
		}
	}
	if _, err := s.groups.Grant(r.Context(), node, groupID, want, u.ID); err != nil {
		s.groupError(w, r, err)
		return
	}
	s.audit.Record(r, audit.Event{
		Kind:    audit.KindGrantCreated,
		UserID:  u.ID,
		Subject: u.Email,
		NodeID:  node.ID,
		Detail: map[string]any{
			"node":         node.ID.String(),
			"name":         node.Name,
			"group":        groupID.String(),
			"level":        string(want.Level),
			"can_download": want.CanDownload,
			"can_share":    want.CanShare,
			"updated":      found,
		},
	})
	if isNew {
		ctx := r.Context()
		g, err := s.groups.Get(ctx, groupID)
		if err == nil && !g.IsEveryone {
			rows, err := s.store.ListGroupEmailRecipients(ctx, store.ListGroupEmailRecipientsParams{
				GroupID:       groupID,
				ExcludeUserID: u.ID,
			})
			if err != nil {
				slog.WarnContext(ctx, "failed to list group email recipients", "err", err)
			} else {
				recips := make([]notify.Recipient, len(rows))
				for i, row := range rows {
					recips[i] = notify.Recipient{
						ID:       row.ID,
						Email:    row.Email,
						Settings: row.Settings,
					}
				}
				s.notify.Shared(ctx, notify.Share{
					Granter:     *u,
					Node:        node,
					Level:       want.Level,
					CanDownload: want.CanDownload,
					Group:       g.Name,
					Recipients:  recips,
				})
			}
		} else if err != nil {
			slog.WarnContext(ctx, "failed to get group for notification", "err", err)
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleRevokeGrant(w http.ResponseWriter, r *http.Request) {
	u, node, caps := s.requireNodeCaps(w, r, share.CapShare)
	if u == nil {
		return
	}
	groupID, err := uuidParam(r, "groupID")
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "invalid group id")
		return
	}
	existing, found, err := s.groupGrant(r, node.ID, groupID)
	if err != nil {
		Error(w, r, err)
		return
	}
	if !found {
		writeProblem(w, http.StatusNotFound, "Not Found", "that group has no grant on this node")
		return
	}
	if !caps.Covers(grantRowCaps(existing)) {
		writeProblem(w, http.StatusForbidden, "Forbidden", errGrantWider)
		return
	}
	if !s.keepsTeamManager(w, r, node, grantRowIsManager(existing), false) {
		return
	}
	revoked, err := s.groups.Revoke(r.Context(), node.ID, groupID)
	if err != nil {
		s.groupError(w, r, err)
		return
	}
	if !revoked {
		// Gone between the read and the delete — somebody else revoked it.
		writeProblem(w, http.StatusNotFound, "Not Found", "that group has no grant on this node")
		return
	}
	s.audit.Record(r, audit.Event{
		Kind:    audit.KindGrantRevoked,
		UserID:  u.ID,
		Subject: u.Email,
		NodeID:  node.ID,
		Detail: map[string]any{
			"node":  node.ID.String(),
			"name":  node.Name,
			"group": groupID.String(),
		},
	})
	w.WriteHeader(http.StatusNoContent)
}

// groupGrant finds the group's grant on exactly this node, if any.
func (s *Server) groupGrant(r *http.Request, nodeID, groupID pgtype.UUID) (store.ListGrantsForNodeRow, bool, error) {
	rows, err := s.groups.GrantsForNode(r.Context(), nodeID)
	if err != nil {
		return store.ListGrantsForNodeRow{}, false, err
	}
	for _, g := range rows {
		if g.GroupID == groupID {
			return g, true, nil
		}
	}
	return store.ListGrantsForNodeRow{}, false, nil
}

func grantRowCaps(g store.ListGrantsForNodeRow) share.GrantCaps {
	return share.GrantCaps{Level: g.Level, CanDownload: g.CanDownload, CanShare: g.CanShare}
}

// grantRowIsManager: a live can_share grant — one CountTeamManagers counts.
func grantRowIsManager(g store.ListGrantsForNodeRow) bool {
	return g.CanShare && (!g.ExpiresAt.Valid || g.ExpiresAt.Time.After(time.Now()))
}
