package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nhatbui/pp-hub/apps/server/internal/activity"
	"github.com/nhatbui/pp-hub/apps/server/internal/audit"
	"github.com/nhatbui/pp-hub/apps/server/internal/exthost"
	"github.com/nhatbui/pp-hub/apps/server/internal/fs"
	"github.com/nhatbui/pp-hub/apps/server/internal/share"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

// extensionDTO is one extension as the settings screens and the New menu see
// it. Enabled is the effective answer for the space asked about (the instance
// switch when none was); Instance and Space are the two switches behind it.
type extensionDTO struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Version     string   `json:"version"`
	Description string   `json:"description"`
	Author      string   `json:"author"`
	License     string   `json:"license"`
	Homepage    string   `json:"homepage"`
	NodeKinds   []string `json:"node_kinds"`
	Instance    bool     `json:"instance"`
	Space       bool     `json:"space"`
	Enabled     bool     `json:"enabled"`
}

// handleListExtensions is GET /extensions[?space_id=]: every extension this
// build carries and whether each is on. It is open to any signed-in person —
// the New menu needs it — and says nothing a space member could not see.
func (s *Server) handleListExtensions(w http.ResponseWriter, r *http.Request) {
	u := caller(r)
	var spaceID pgtype.UUID
	if raw := r.URL.Query().Get("space_id"); raw != "" {
		if err := spaceID.Scan(raw); err != nil {
			writeProblem(w, http.StatusBadRequest, "Bad Request", "invalid space_id")
			return
		}
		if err := s.spaces.RequireMember(r.Context(), spaceID, u.ID); err != nil {
			s.spaceError(w, r, err)
			return
		}
	}
	out := []extensionDTO{}
	if s.ext != nil {
		infos, err := s.ext.List(r.Context(), spaceID)
		if err != nil {
			Error(w, r, err)
			return
		}
		for _, i := range infos {
			out = append(out, extensionDTO{
				ID: i.ID, Name: i.Name, Version: i.Version, Description: i.Description,
				Author: i.Author, License: i.License, Homepage: i.Homepage, NodeKinds: i.NodeKinds,
				Instance: i.Instance, Space: i.Space, Enabled: i.Enabled,
			})
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) extError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, exthost.ErrUnknownExt), errors.Is(err, exthost.ErrUnknownKind):
		writeProblem(w, http.StatusNotFound, "Not Found", err.Error())
	case errors.Is(err, exthost.ErrDisabled):
		writeProblem(w, http.StatusForbidden, "Forbidden", err.Error())
	case errors.Is(err, fs.ErrNoExtensions):
		writeProblem(w, http.StatusNotFound, "Not Found", err.Error())
	default:
		s.nodeError(w, r, err)
	}
}

func (s *Server) readEnabled(w http.ResponseWriter, r *http.Request) (bool, bool) {
	var body struct {
		Enabled *bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Enabled == nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "enabled is required")
		return false, false
	}
	return *body.Enabled, true
}

// handleSetExtensionInstance is PATCH /admin/extensions/{extID}: the instance
// switch. An extension ships off; an instance administrator turns it on.
func (s *Server) handleSetExtensionInstance(w http.ResponseWriter, r *http.Request) {
	admin := s.requireAdmin(w, r)
	if admin == nil {
		return
	}
	if s.ext == nil {
		writeProblem(w, http.StatusNotFound, "Not Found", exthost.ErrUnknownExt.Error())
		return
	}
	enabled, ok := s.readEnabled(w, r)
	if !ok {
		return
	}
	id := chi.URLParam(r, "extID")
	if err := s.ext.SetInstance(r.Context(), id, enabled, admin.ID); err != nil {
		s.extError(w, r, err)
		return
	}
	s.audit.Record(r, audit.Event{
		Kind: audit.KindSettingsUpdated, UserID: admin.ID, Subject: admin.Email,
		Detail: map[string]any{"extension": id, "enabled": enabled},
	})
	s.handleListExtensions(w, r)
}

// handleSetExtensionSpace is PUT /spaces/{spaceID}/extensions/{extID}: a space
// admin switching an instance-enabled extension off for their space, or back
// on. It cannot turn on what the instance has not.
func (s *Server) handleSetExtensionSpace(w http.ResponseWriter, r *http.Request) {
	u, space, ok := s.requireSpaceAdmin(w, r)
	if !ok {
		return
	}
	if s.ext == nil {
		writeProblem(w, http.StatusNotFound, "Not Found", exthost.ErrUnknownExt.Error())
		return
	}
	enabled, ok := s.readEnabled(w, r)
	if !ok {
		return
	}
	id := chi.URLParam(r, "extID")
	if err := s.ext.SetSpace(r.Context(), id, space.ID, enabled, u.ID); err != nil {
		s.extError(w, r, err)
		return
	}
	s.audit.Record(r, audit.Event{
		Kind: audit.KindSettingsUpdated, UserID: u.ID, Subject: u.Email,
		Detail: map[string]any{"extension": id, "space": uuidString(space.ID), "enabled": enabled},
	})
	// The listing reads the space from the query string; this route carries it
	// in the path.
	q := r.URL.Query()
	q.Set("space_id", uuidString(space.ID))
	r.URL.RawQuery = q.Encode()
	s.handleListExtensions(w, r)
}

// handleListExtNodes is GET /spaces/{spaceID}/extensions/{extID}/nodes: the
// nodes of one app that the caller can open, for the sidebar's Apps section.
// They are not listed in the drive: an app's nodes would mix with files and
// folders there. Where a node hangs does not matter — a board in a team folder
// is found here through the folder's grant like any other — so the candidates
// are every live node of the app in the space, narrowed by what the caller may
// open.
func (s *Server) handleListExtNodes(w http.ResponseWriter, r *http.Request) {
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
	extID := chi.URLParam(r, "extID")
	if s.ext == nil || !s.ext.Known(extID) {
		writeProblem(w, http.StatusNotFound, "Not Found", exthost.ErrUnknownExt.Error())
		return
	}
	on, err := s.ext.Enabled(r.Context(), extID, spaceID)
	if err != nil {
		Error(w, r, err)
		return
	}
	if !on {
		writeProblem(w, http.StatusForbidden, "Forbidden", exthost.ErrDisabled.Error())
		return
	}
	all, err := s.fs.ListExtNodes(r.Context(), spaceID, extID)
	if err != nil {
		Error(w, r, err)
		return
	}
	rows, rc, err := reachableRows(r.Context(), s, u.ID, all,
		func(n store.NodeWithMeta) pgtype.UUID { return n.ID },
		func(n store.NodeWithMeta) pgtype.UUID { return n.SpaceID })
	if err != nil {
		Error(w, r, err)
		return
	}
	out := make([]nodeDTO, 0, len(rows))
	for _, n := range rows {
		d := nodeToDTOMeta(n)
		rc.stamp(&d, n.ID)
		out = append(out, d)
	}
	writeJSON(w, http.StatusOK, out)
}

// handleCreateExtNode is POST /spaces/{spaceID}/ext-nodes: a new node of an
// extension's kind — a kanban board — in a folder, under the same write rule
// as any new file. The extension seeds its rows in the transaction that
// creates the node.
func (s *Server) handleCreateExtNode(w http.ResponseWriter, r *http.Request) {
	u := caller(r)
	spaceID, err := uuidParam(r, "spaceID")
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "invalid space id")
		return
	}
	var body struct {
		ExtKind  string `json:"ext_kind"`
		Name     string `json:"name"`
		ParentID string `json:"parent_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "invalid body")
		return
	}
	name := strings.TrimSpace(body.Name)
	if name == "" {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "name is required")
		return
	}
	if strings.ContainsAny(name, `/\`) {
		writeProblem(w, http.StatusBadRequest, "Bad Request", `name cannot contain / or \`)
		return
	}
	if s.ext == nil {
		writeProblem(w, http.StatusNotFound, "Not Found", exthost.ErrUnknownKind.Error())
		return
	}
	extID, known := s.ext.KindOwner(body.ExtKind)
	if !known {
		writeProblem(w, http.StatusNotFound, "Not Found", exthost.ErrUnknownKind.Error())
		return
	}
	var parentID pgtype.UUID
	if body.ParentID != "" {
		if err := parentID.Scan(body.ParentID); err != nil {
			writeProblem(w, http.StatusBadRequest, "Bad Request", "invalid parent_id")
			return
		}
	}
	if err := s.spaces.RequireMember(r.Context(), spaceID, u.ID); err != nil {
		s.spaceError(w, r, err)
		return
	}
	on, err := s.ext.Enabled(r.Context(), extID, spaceID)
	if err != nil {
		Error(w, r, err)
		return
	}
	if !on {
		writeProblem(w, http.StatusForbidden, "Forbidden", exthost.ErrDisabled.Error())
		return
	}
	if !s.requireParentWrite(w, r, spaceID, parentID, u, fs.Child{Kind: store.NodeKindExt}) {
		return
	}
	n, err := s.fs.CreateExt(r.Context(), spaceID, parentID, name, body.ExtKind, u.ID)
	if err != nil {
		s.extError(w, r, err)
		return
	}
	s.activity.Node(r.Context(), u.ID, n, activity.KindCreated, map[string]any{"ext_kind": body.ExtKind})
	writeJSON(w, http.StatusCreated, nodeToDTO(n, nil).withCaps(share.CapsOwner))
}
