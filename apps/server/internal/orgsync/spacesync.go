package orgsync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

// A space is a company and a company is a Logto organization, so the
// space-level writes an admin makes here — creating one, renaming it, linking
// one that predates the sync — go to Logto first and are mirrored second,
// exactly like membership. Deletion is the exception: it does not travel in
// either direction (see detached_orgs and orgGone).

// LinkJobKind routes the member push that links an existing space.
const LinkJobKind = "org:link"

var (
	// ErrAlreadyLinked: the space already mirrors an organization.
	ErrAlreadyLinked = errors.New("orgsync: space is already linked to an organization")
	// ErrStrandedMembers: linking would cut off members who have no Logto
	// account, and the caller has not confirmed that.
	ErrStrandedMembers = errors.New("orgsync: some members have no account in this Logto tenant")
)

// CreateSpace creates the organization, seats the creator in it as admin, and
// mirrors it as a space.
//
// Upstream first. If the local half fails after the organization exists, the
// creator's next sign-in finds it in their token and creates the space the
// ordinary way (ensureSpace) — so there is no compensation to write, and no
// organization left behind with nobody in it.
func (s *Service) CreateSpace(ctx context.Context, creator store.User, name string) (store.Space, error) {
	if creator.OidcIss != s.issuer {
		return store.Space{}, ErrNoIdentity
	}
	org, err := s.client.CreateOrganization(ctx, name)
	if err != nil {
		return store.Space{}, err
	}
	if err := s.client.AddMember(ctx, org.ID, creator.OidcSub); err != nil {
		return store.Space{}, err
	}
	if err := s.client.SetAdmin(ctx, org.ID, creator.OidcSub, true); err != nil {
		return store.Space{}, err
	}
	space, _, err := s.ensureSpace(ctx, org.ID, creator)
	if err != nil {
		return store.Space{}, err
	}
	if err := s.applyMembership(ctx, space.ID, creator.ID, true); err != nil {
		return store.Space{}, err
	}
	return space, nil
}

// Rename renames the organization before the caller renames the space, so
// the next reconcile — which mirrors the organization's name — agrees. An
// unlinked space has nothing upstream to rename.
func (s *Service) Rename(ctx context.Context, space store.Space, name string) error {
	if !space.IdpOrgID.Valid {
		return nil
	}
	return s.client.RenameOrganization(ctx, space.IdpOrgID.String, name)
}

// LinkPreview splits a space's members into those who can be pushed into a
// new organization — accounts this Logto tenant issued — and those who
// cannot and would therefore lose access once the space is linked.
func (s *Service) LinkPreview(ctx context.Context, space store.Space) (pushable, stranded []store.User, err error) {
	members, err := s.q.ListSpaceMemberAccounts(ctx, space.ID)
	if err != nil {
		return nil, nil, err
	}
	for _, u := range members {
		if u.OidcIss == s.issuer {
			pushable = append(pushable, u)
		} else {
			stranded = append(stranded, u)
		}
	}
	return pushable, stranded, nil
}

// Link creates an organization for an unlinked space and queues the push of
// its members. It returns once the organization exists; the space shows as
// linked when the job has finished.
//
// The organization is created here, synchronously, and its id travels in the
// job — so a retried job never creates a second one. Members who cannot be
// pushed must be confirmed by the caller (confirmStranded), because linking
// turns each of them into a departure.
func (s *Service) Link(ctx context.Context, space store.Space, confirmStranded bool) (string, error) {
	if space.IdpOrgID.Valid {
		return "", ErrAlreadyLinked
	}
	_, stranded, err := s.LinkPreview(ctx, space)
	if err != nil {
		return "", err
	}
	if len(stranded) > 0 && !confirmStranded {
		return "", ErrStrandedMembers
	}
	org, err := s.client.CreateOrganization(ctx, space.Name)
	if err != nil {
		return "", err
	}
	payload, err := json.Marshal(jobPayload{OrgID: org.ID, SpaceID: space.ID.String()})
	if err != nil {
		return "", err
	}
	if _, err := s.q.EnqueueJob(ctx, store.EnqueueJobParams{Kind: LinkJobKind, Payload: payload}); err != nil {
		return "", err
	}
	return org.ID, nil
}

// runLink is the job body: push every member, link, then turn the members
// who could not be pushed into departures.
//
// The link is written last. Until then no reconcile compares the space
// against the organization, so a Membership.Updated delivery that arrives
// halfway through the push cannot evict the members not yet pushed. Every
// step is safe to repeat: a member already in the organization is not an
// error, and a space already linked to this organization skips to the end.
func (s *Service) runLink(ctx context.Context, spaceID pgtype.UUID, orgID string) error {
	space, err := s.q.GetSpace(ctx, spaceID)
	if errors.Is(err, pgx.ErrNoRows) {
		// Deleted while the job waited. There is nothing left to link, and
		// retrying would fail the same way forever.
		slog.WarnContext(ctx, "link skipped: space no longer exists", "space", spaceID.String(), "org", orgID)
		return nil
	}
	if err != nil {
		return err
	}
	if space.IdpOrgID.Valid && space.IdpOrgID.String != orgID {
		return fmt.Errorf("space %s was linked to %s while linking to %s", spaceID.String(), space.IdpOrgID.String, orgID)
	}
	pushable, stranded, err := s.LinkPreview(ctx, space)
	if err != nil {
		return err
	}
	if !space.IdpOrgID.Valid {
		for _, u := range pushable {
			err := s.client.AddMember(ctx, orgID, u.OidcSub)
			if err != nil && Status(err) != http.StatusUnprocessableEntity {
				return fmt.Errorf("add %s to %s: %w", u.Email, orgID, err)
			}
			if err := s.client.SetAdmin(ctx, orgID, u.OidcSub, s.spaces.IsAdmin(ctx, space.ID, u.ID)); err != nil {
				return fmt.Errorf("role of %s in %s: %w", u.Email, orgID, err)
			}
		}
		if err := s.q.ReattachOrg(ctx, orgID); err != nil {
			return err
		}
		if err := s.q.SetSpaceOrg(ctx, store.SetSpaceOrgParams{
			ID: space.ID, IdpOrgID: pgtype.Text{String: orgID, Valid: true},
		}); err != nil {
			return err
		}
		slog.InfoContext(ctx, "space linked to a new organization",
			"space", space.ID.String(), "org", orgID, "pushed", len(pushable), "stranded", len(stranded))
	}
	for _, u := range stranded {
		if err := s.depart(ctx, space.ID, u.ID, SourceAdmin); err != nil {
			return err
		}
	}
	return s.Enqueue(ctx, orgID)
}
