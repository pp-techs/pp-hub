package orgsync

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"
)

// SweepAccounts exposes the hourly account sweep to the external test package.
func (s *Service) SweepAccounts(ctx context.Context) error { return s.sweepAccounts(ctx) }

// RunLink exposes the org:link job body to the external test package.
func (s *Service) RunLink(ctx context.Context, spaceID pgtype.UUID, orgID string) error {
	return s.runLink(ctx, spaceID, orgID)
}
