package httpapi

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nhatbui/pp-hub/apps/server/internal/audit"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

// adminStatsDTO is the row of tiles at the top of the admin console.
type adminStatsDTO struct {
	Users          int64 `json:"users"`
	SuspendedUsers int64 `json:"suspended_users"`
	Spaces         int64 `json:"spaces"`
	Files          int64 `json:"files"`
	Trashed        int64 `json:"trashed"`
	StorageBytes   int64 `json:"storage_bytes"`
	JobsPending    int64 `json:"jobs_pending"`
	JobsFailed     int64 `json:"jobs_failed"`
}

// requireAdmin resolves the caller and demands the system admin flag.
//
// Distinct from space ownership: a space owner administers their own space, an
// admin administers the server. Neither implies the other.
func (s *Server) requireAdmin(w http.ResponseWriter, r *http.Request) *store.User {
	u := caller(r)
	if !u.IsAdmin {
		writeProblem(w, http.StatusForbidden, "Forbidden", "admin access required")
		return nil
	}
	return u
}

func (s *Server) handleAdminStats(w http.ResponseWriter, r *http.Request) {
	if s.requireAdmin(w, r) == nil {
		return
	}
	row, err := s.store.AdminStats(r.Context())
	if err != nil {
		Error(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, adminStatsDTO{
		Users:          row.Users,
		SuspendedUsers: row.SuspendedUsers,
		Spaces:         row.Spaces,
		Files:          row.Files,
		Trashed:        row.Trashed,
		StorageBytes:   row.StorageBytes,
		JobsPending:    row.JobsPending,
		JobsFailed:     row.JobsFailed,
	})
}

// handleAdminUpdateUser toggles the admin flag or the suspension.
//
// Both guard against self-inflicted lockout: an admin cannot remove their own
// admin rights or suspend themselves, because doing so would leave the server
// potentially unadministrable with no way back through the UI.
func (s *Server) handleAdminUpdateUser(w http.ResponseWriter, r *http.Request) {
	admin := s.requireAdmin(w, r)
	if admin == nil {
		return
	}
	userID, err := uuidParam(r, "userID")
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "invalid user id")
		return
	}

	var body struct {
		IsAdmin   *bool `json:"is_admin"`
		Suspended *bool `json:"suspended"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "invalid body")
		return
	}
	isSelf := userID == admin.ID

	if body.IsAdmin != nil {
		if isSelf && !*body.IsAdmin {
			writeProblem(w, http.StatusBadRequest, "Bad Request",
				"you cannot remove your own admin access")
			return
		}
		if err := s.store.SetUserAdmin(r.Context(), store.SetUserAdminParams{
			ID: userID, IsAdmin: *body.IsAdmin,
		}); err != nil {
			Error(w, r, err)
			return
		}
		kind := audit.KindAdminRevoked
		if *body.IsAdmin {
			kind = audit.KindAdminGranted
		}
		s.audit.Record(r, audit.Event{
			Kind:    kind,
			UserID:  userID,
			Subject: admin.Email,
			Detail:  map[string]any{"by": admin.ID.String()},
		})
	}

	if body.Suspended != nil {
		if isSelf && *body.Suspended {
			writeProblem(w, http.StatusBadRequest, "Bad Request", "you cannot suspend yourself")
			return
		}
		var at pgtype.Timestamptz
		if *body.Suspended {
			at = pgtype.Timestamptz{Time: time.Now(), Valid: true}
		}
		if err := s.store.SetUserSuspended(r.Context(), store.SetUserSuspendedParams{
			ID: userID, SuspendedAt: at,
		}); err != nil {
			Error(w, r, err)
			return
		}
		kind := audit.KindUnsuspended
		if *body.Suspended {
			kind = audit.KindSuspended
			// The per-request suspension check already locks them out, but
			// leaving the rows behind means the account's Sessions tab keeps
			// listing devices that can no longer do anything, and an
			// un-suspension silently restores every one of them. Ending them
			// is what "suspended" should mean.
			if n, err := s.store.DeleteUserSessions(r.Context(), userID); err != nil {
				slog.Error("revoke sessions on suspend", "user", userID.String(), "err", err)
			} else if n > 0 {
				s.audit.Record(r, audit.Event{
					Kind:    audit.KindSessionsRevoked,
					UserID:  userID,
					Subject: admin.Email,
					Detail:  map[string]any{"revoked": n, "reason": "suspended"},
				})
			}
		}
		s.audit.Record(r, audit.Event{
			Kind:    kind,
			UserID:  userID,
			Subject: admin.Email,
			Detail:  map[string]any{"by": admin.ID.String()},
		})
	}

	user, err := s.store.GetUser(r.Context(), userID)
	if err != nil {
		writeProblem(w, http.StatusNotFound, "Not Found", "user not found")
		return
	}
	spaceCount, _ := s.store.CountUserSpaces(r.Context(), userID)
	writeJSON(w, http.StatusOK, adminUserToDTO(user, spaceCount))
}

// adminUserDTO adds the server-administration facts to the profile shape.
type adminUserDTO struct {
	ID          string `json:"id"`
	Email       string `json:"email"`
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	AvatarURL   string `json:"avatar_url"`
	IsAdmin     bool   `json:"is_admin"`
	Suspended   bool   `json:"suspended"`
	Spaces      int64  `json:"spaces"`
	LastSeen    string `json:"last_seen,omitempty"`
	CreatedAt   string `json:"created_at"`
}

func adminUserToDTO(u store.User, spaceCount int64) adminUserDTO {
	return adminUserDTO{
		ID:          uuidString(u.ID),
		Email:       u.Email,
		Name:        u.Name,
		DisplayName: u.DisplayName,
		AvatarURL:   u.AvatarUrl,
		IsAdmin:     u.IsAdmin,
		Suspended:   u.SuspendedAt.Valid,
		Spaces:      spaceCount,
		LastSeen:    optionalTime(u.LastSeen),
		CreatedAt:   u.CreatedAt.Time.Format(timeLayout),
	}
}
