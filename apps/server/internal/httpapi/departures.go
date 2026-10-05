package httpapi

import (
	"net/http"

	"github.com/nhatbui/pp-hub/apps/server/internal/audit"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

// A departure is somebody the organization no longer lists. Their access is
// already gone — the sync path dropped the membership and group rows the
// moment it learned — but what they created is untouched until an admin says
// so. This is that queue.
type departureDTO struct {
	UserID      string `json:"user_id"`
	Email       string `json:"email"`
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	AvatarURL   string `json:"avatar_url"`
	DetectedAt  string `json:"detected_at"`
	// Source is how pp-hub found out: signin, webhook, reconcile or admin.
	// It is on the wire because "removed by an admin here" and "vanished
	// from the directory" read very differently to the person confirming.
	Source string `json:"source"`
	// FilesOwned is how much is actually at stake. Zero is the common case
	// and the client says so rather than warning about nothing.
	FilesOwned int64 `json:"files_owned"`
}

func departureToDTO(row store.ListPendingDeparturesRow) departureDTO {
	return departureDTO{
		UserID:      uuidString(row.UserID),
		Email:       row.Email,
		Name:        row.UserName,
		DisplayName: row.DisplayName,
		AvatarURL:   row.AvatarUrl,
		DetectedAt:  row.DetectedAt.Time.Format(timeLayout),
		Source:      row.Source,
		FilesOwned:  row.FilesOwned,
	}
}

func (s *Server) handleListDepartures(w http.ResponseWriter, r *http.Request) {
	u := caller(r)
	spaceID, err := uuidParam(r, "spaceID")
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Error", "invalid space id")
		return
	}
	if !s.requireOrgSpace(w, r, spaceID) {
		return
	}
	if err := s.spaces.RequireAdmin(r.Context(), spaceID, u.ID); err != nil {
		s.spaceError(w, r, err)
		return
	}
	rows, err := s.store.ListPendingDepartures(r.Context(), spaceID)
	if err != nil {
		Error(w, r, err)
		return
	}
	out := make([]departureDTO, 0, len(rows))
	for _, row := range rows {
		out = append(out, departureToDTO(row))
	}
	writeJSON(w, http.StatusOK, out)
}

// handleCompleteDeparture runs the destructive half of a departure.
//
// Grants go, nested ownership climbs to the surrounding folder's creator, and
// top-level subtrees the departed member owned move into the space's Archive.
// None of it is reversible, which is exactly why no webhook may trigger it
// and why the route is a POST an administrator has to make on purpose.
func (s *Server) handleCompleteDeparture(w http.ResponseWriter, r *http.Request) {
	u := caller(r)
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
	if !s.requireOrgSpace(w, r, spaceID) {
		return
	}
	if err := s.spaces.RequireAdmin(r.Context(), spaceID, u.ID); err != nil {
		s.spaceError(w, r, err)
		return
	}
	// The pending row is the authorization for this, not decoration: without
	// the check, the endpoint would archive the content of somebody who is
	// still a member.
	if _, err := s.store.GetPendingDeparture(r.Context(), store.GetPendingDepartureParams{
		SpaceID: spaceID, UserID: userID,
	}); err != nil {
		writeProblem(w, http.StatusNotFound, "Not Found", "no pending departure for that user in this space")
		return
	}
	if err := s.spaces.CompleteDeparture(r.Context(), spaceID, userID, u.ID); err != nil {
		s.spaceError(w, r, err)
		return
	}
	if err := s.store.ResolveDeparture(r.Context(), store.ResolveDepartureParams{
		SpaceID: spaceID, UserID: userID, ResolvedBy: u.ID,
	}); err != nil {
		Error(w, r, err)
		return
	}
	s.audit.Record(r, audit.Event{
		Kind:    audit.KindMemberRemoved,
		UserID:  userID,
		Subject: u.Email,
		Detail:  map[string]any{"space": spaceID.String(), "handover": "completed"},
	})
	w.WriteHeader(http.StatusNoContent)
}
