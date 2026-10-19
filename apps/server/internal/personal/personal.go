// Package personal does what a sign-in owes personal use: it provisions the
// person's personal space when the instance's `personal_spaces` setting admits
// them, and turns invitations waiting for their verified address into grants.
// See docs/architecture/personal-spaces.md.
package personal

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/nhatbui/pp-hub/apps/server/internal/audit"
	"github.com/nhatbui/pp-hub/apps/server/internal/share"
	"github.com/nhatbui/pp-hub/apps/server/internal/spaces"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
	"github.com/nhatbui/pp-hub/apps/server/internal/sysconfig"
)

// Policy is the part of the instance configuration provisioning reads.
type Policy interface {
	PersonalSpaces() string
	DefaultPersonalQuotaBytes() int64
}

type Service struct {
	q      *store.Queries
	spaces *spaces.Service
	share  *share.Service
	policy Policy
	audit  *audit.Service
}

func New(q *store.Queries, sp *spaces.Service, sh *share.Service, policy Policy, au *audit.Service) *Service {
	return &Service{q: q, spaces: sp, share: sh, policy: policy, audit: au}
}

// OnSignIn is the auth.SignInHook. Failures are logged: neither a missing
// personal space nor an unadopted invitation is a reason to refuse a sign-in,
// and the next sign-in tries again.
func (s *Service) OnSignIn(r *http.Request, user store.User, verifiedEmail string) {
	ctx := r.Context()
	// Adopt first: an invitation into somebody's folder is the reason this
	// person signed up, and it must not wait on provisioning.
	if verifiedEmail != "" {
		s.Adopt(r, user, verifiedEmail, false)
	}
	if err := s.Provision(ctx, user); err != nil {
		slog.ErrorContext(ctx, "personal space at sign-in", "module", "personal", "user", user.Email, "err", err)
	}
}

// Provision creates the person's personal space if the instance admits them
// and they have none. Under `members` that takes belonging to at least one
// organization, so the open email sign-up Logto needs for invitees does not
// hand storage to anybody who finds the instance.
func (s *Service) Provision(ctx context.Context, user store.User) error {
	// A guest holds access to what was shared with them and nothing of their
	// own: no space, whatever the instance setting says.
	if user.Kind == "guest" {
		return nil
	}
	switch s.policy.PersonalSpaces() {
	case sysconfig.PersonalSpacesEveryone:
	case sysconfig.PersonalSpacesMembers:
		n, err := s.q.CountOrgSpaces(ctx, user.ID)
		if err != nil {
			return err
		}
		if n == 0 {
			return nil
		}
	default:
		return nil
	}
	sp, created, err := s.spaces.EnsurePersonal(ctx, user, s.policy.DefaultPersonalQuotaBytes())
	if err != nil {
		return err
	}
	if created {
		slog.InfoContext(ctx, "personal space provisioned", "module", "personal", "user", user.Email, "space", sp.ID.String())
	}
	return nil
}

// Adopt turns the invitations waiting for a verified address into grants and
// audits each one. viewOnly is the guest's call: see share.AdoptPending.
func (s *Service) Adopt(r *http.Request, user store.User, email string, viewOnly bool) {
	ctx := r.Context()
	rows, err := s.share.AdoptPending(ctx, user, share.NormalizeEmail(email), viewOnly)
	if err != nil {
		slog.ErrorContext(ctx, "adopt pending shares", "module", "personal", "user", user.Email, "err", err)
		return
	}
	for _, a := range rows {
		s.audit.Record(r, audit.Event{
			Kind:    audit.KindPendingShareAdopted,
			UserID:  user.ID,
			Subject: user.Email,
			NodeID:  a.Share.NodeID,
			Detail: map[string]any{
				"node": a.Share.NodeID.String(), "name": a.NodeName, "share": a.Share.ID.String(),
				"level": string(a.Share.Level), "can_download": a.Share.CanDownload,
				"external": a.Share.IsExternal, "invited_by": a.InvitedBy.String(),
			},
		})
	}
}
