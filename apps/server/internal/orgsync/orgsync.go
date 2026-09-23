// Package orgsync mirrors Logto organizations onto pp-hub spaces.
//
// A Logto organization is the source of truth for who belongs to a company;
// `space_members` is a projection of it and is written only from here. See
// docs/architecture/logto-org-sync.md.
//
// Three paths feed the same reconciler, and none of them applies a delta:
//
//   - ReconcileUser, from the `organizations` and `organization_roles` claims
//     in an ID token. It makes no outbound call and is the correctness
//     backbone — a lost webhook heals at the next sign-in.
//   - ReconcileOrg, from a webhook delivery or the hourly sweep. It re-reads
//     the organization's whole member list and converges against it, because
//     webhook deliveries carry no ordering guarantee and applying their
//     add/remove arrays incrementally would be a race, not an optimisation.
//   - The write path, where an admin's action goes upstream first and is
//     mirrored locally only once Logto has accepted it.
package orgsync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nhatbui/pp-hub/apps/server/internal/spaces"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
	"github.com/nhatbui/pp-hub/apps/server/internal/users"
)

// JobKind routes a reconcile onto whichever process runs this worker.
const JobKind = "org:reconcile"

// inviteTTL is how long an organization invitation stays redeemable. Logto
// wants an absolute expiry, not a duration.
const inviteTTL = 14 * 24 * time.Hour

// ErrNotLinked: the space mirrors no organization, so there is no upstream to
// change. Distinct from ErrDisabled, which is about this server's credentials
// rather than this space's configuration.
var ErrNotLinked = errors.New("orgsync: space is not linked to an organization")

// Sources recorded on a departure row — how pp-hub learned somebody left.
const (
	SourceSignin    = "signin"
	SourceWebhook   = "webhook"
	SourceReconcile = "reconcile"
	SourceAdmin     = "admin"
)

type Service struct {
	q      *store.Queries
	spaces *spaces.Service
	// profiles keeps the IdP-owned profile fields of every member current,
	// including accounts whose owner has not signed in yet.
	profiles *users.Service
	client   *Client
	// issuer is the `iss` a pre-provisioned account is keyed on. It must be
	// the same string auth.resolveUser writes, or the member's first sign-in
	// creates a second account beside the one reconcile made for them.
	issuer string
}

func NewService(q *store.Queries, sp *spaces.Service, profiles *users.Service, client *Client, issuer string) *Service {
	return &Service{q: q, spaces: sp, profiles: profiles, client: client, issuer: issuer}
}

// Enabled reports whether the Management API is reachable. The claim-driven
// path works without it; every write and every full reconcile does not.
func (s *Service) Enabled() bool { return s.client.Enabled() }

// Configured reports whether organizations own membership on this install,
// which is true as soon as an endpoint is named. It is the question "does
// pp-hub still decide who belongs to a company" — the answer gates space
// creation — as opposed to Enabled, which asks whether it can write upstream.
func (s *Service) Configured() bool { return s.client.Configured() }

// --- sign-in path ---

// Claims are the organization claims Logto adds to an ID token when the
// application requests urn:logto:scope:organizations and
// urn:logto:scope:organization_roles.
type Claims struct {
	// Organizations is the set of organization ids the user belongs to.
	Organizations []string `json:"organizations"`
	// OrganizationRoles is "<organizationId>:<roleName>", one entry per role
	// per organization.
	OrganizationRoles []string `json:"organization_roles"`
}

// admins maps organization id to whether the user holds the admin role there.
func (c Claims) admins(adminRole string) map[string]bool {
	out := make(map[string]bool, len(c.Organizations))
	for _, org := range c.Organizations {
		out[org] = false
	}
	for _, entry := range c.OrganizationRoles {
		org, role, ok := strings.Cut(entry, ":")
		if !ok || !strings.EqualFold(role, adminRole) {
			continue
		}
		// Only for organizations the `organizations` claim also listed: a
		// role without a membership is not a membership.
		if _, member := out[org]; member {
			out[org] = true
		}
	}
	return out
}

// ReconcileUser converges one person's membership from their own ID token.
//
// It is deliberately one-sided about what it trusts: the claims describe this
// user and nobody else, so it adds and removes only their rows. The rest of
// each organization arrives through ReconcileOrg.
func (s *Service) ReconcileUser(ctx context.Context, user store.User, claims Claims) error {
	want := claims.admins(s.client.cfg.AdminRole)

	for orgID, isAdmin := range want {
		space, err := s.ensureSpace(ctx, orgID, user)
		if err != nil {
			return fmt.Errorf("space for organization %s: %w", orgID, err)
		}
		if err := s.applyMembership(ctx, space.ID, user.ID, isAdmin); err != nil {
			return err
		}
	}

	// The other direction: a space this account is still a member of, whose
	// organization the token no longer lists, is a departure nobody told us
	// about.
	mine, err := s.q.ListMySpaces(ctx, user.ID)
	if err != nil {
		return err
	}
	for _, space := range mine {
		if !space.IdpOrgID.Valid {
			continue // unlinked: nothing upstream claims to know better
		}
		if _, still := want[space.IdpOrgID.String]; still {
			continue
		}
		if err := s.depart(ctx, space.ID, user.ID, SourceSignin); err != nil {
			return err
		}
	}
	return nil
}

// ensureSpace finds or creates the space mirroring an organization.
//
// Creation happens here, at a sign-in, rather than from the Organization.Created
// webhook, for a structural reason: spaces.owner_id is NOT NULL, and a webhook
// carries no user to own the result. The first member through the door owns
// it; their admin status is then corrected from the organization like
// everybody else's, so owning the row buys nothing on its own.
func (s *Service) ensureSpace(ctx context.Context, orgID string, creator store.User) (store.Space, error) {
	space, err := s.q.GetSpaceByOrgID(ctx, pgtype.Text{String: orgID, Valid: true})
	if err == nil {
		return space, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return store.Space{}, err
	}

	name := orgID
	if s.Enabled() {
		if org, err := s.client.Organization(ctx, orgID); err != nil {
			slog.WarnContext(ctx, "organization lookup", "org", orgID, "err", err)
		} else if org.Name != "" {
			name = org.Name
		}
	}
	space, err = s.spaces.Create(ctx, name, creator.ID)
	if err != nil {
		return store.Space{}, err
	}
	if err := s.q.SetSpaceOrg(ctx, store.SetSpaceOrgParams{
		ID: space.ID, IdpOrgID: pgtype.Text{String: orgID, Valid: true},
	}); err != nil {
		return store.Space{}, err
	}
	space.IdpOrgID = pgtype.Text{String: orgID, Valid: true}
	slog.InfoContext(ctx, "space created for organization", "org", orgID, "space", space.ID, "name", name)
	// The creator is one member of possibly many. Pull the rest now rather
	// than making each colleague wait for their own first sign-in.
	if s.Enabled() {
		if err := s.Enqueue(ctx, orgID); err != nil {
			slog.WarnContext(ctx, "enqueue reconcile after space creation", "org", orgID, "err", err)
		}
	}
	return space, nil
}

// applyMembership makes one person's local rows agree with one organization.
func (s *Service) applyMembership(ctx context.Context, spaceID, userID pgtype.UUID, isAdmin bool) error {
	err := s.spaces.RequireMember(ctx, spaceID, userID)
	switch {
	case errors.Is(err, spaces.ErrNotMember):
		if err := s.spaces.AddMember(ctx, spaceID, userID, isAdmin); err != nil {
			return err
		}
	case err != nil:
		return err
	default:
		if err := s.spaces.ApplyAdmin(ctx, spaceID, userID, isAdmin); err != nil {
			return err
		}
	}
	// Rejoining cancels a handover that has not run. Nothing moved, so there
	// is nothing to undo — only a queue entry that stopped being true.
	return s.q.ClearDeparture(ctx, store.ClearDepartureParams{SpaceID: spaceID, UserID: userID})
}

// depart revokes access now and queues the handover for an admin.
func (s *Service) depart(ctx context.Context, spaceID, userID pgtype.UUID, source string) error {
	if err := s.spaces.RevokeMembership(ctx, spaceID, userID); err != nil {
		return err
	}
	return s.q.RecordDeparture(ctx, store.RecordDepartureParams{
		SpaceID: spaceID, UserID: userID, Source: source,
	})
}

// --- full reconcile ---

// ReconcileOrg converges a space against its organization's whole member list.
//
// An organization with no space is not an error and not a reason to create
// one: nobody has signed in from it yet, and creating a space needs an owner.
func (s *Service) ReconcileOrg(ctx context.Context, orgID string) error {
	if !s.Enabled() {
		return ErrDisabled
	}
	space, err := s.q.GetSpaceByOrgID(ctx, pgtype.Text{String: orgID, Valid: true})
	if errors.Is(err, pgx.ErrNoRows) {
		slog.DebugContext(ctx, "reconcile skipped: no space mirrors this organization", "org", orgID)
		return nil
	}
	if err != nil {
		return err
	}

	members, err := s.client.OrgMembers(ctx, orgID)
	if err != nil {
		return err
	}
	want := make(map[pgtype.UUID]bool, len(members))
	for _, m := range members {
		user, err := s.ensureUser(ctx, m)
		if err != nil {
			// One unresolvable account must not stall the other ninety-nine.
			slog.ErrorContext(ctx, "could not resolve organization member", "org", orgID, "logto_user", m.ID, "err", err)
			continue
		}
		want[user.ID] = m.IsAdmin
	}

	have, err := s.q.ListSpaceMemberIDs(ctx, space.ID)
	if err != nil {
		return err
	}
	current := make(map[pgtype.UUID]struct{}, len(have))
	for _, id := range have {
		current[id] = struct{}{}
	}

	for userID, isAdmin := range want {
		if err := s.applyMembership(ctx, space.ID, userID, isAdmin); err != nil {
			return err
		}
	}
	for userID := range current {
		if _, still := want[userID]; still {
			continue
		}
		if err := s.depart(ctx, space.ID, userID, SourceReconcile); err != nil {
			return err
		}
	}
	return nil
}

// ensureUser resolves an organization member to a local account, creating one
// when they have never signed in, and mirrors the profile fields the IdP owns.
//
// Pre-creating accounts is a deliberate reversal of the rule that nothing
// creates an account ahead of its owner's first login. That rule was right
// while pp-hub owned identity; with the IdP authoritative, the identity key is
// exact and comes from the system that issued it, and refusing to pre-create
// would make the People page show a subset of the company — which is the
// disagreement this whole design exists to remove.
//
// A profile sync failure is logged, not returned: the account resolved, and
// membership is what the caller is converging.
func (s *Service) ensureUser(ctx context.Context, m Member) (store.User, error) {
	user, err := s.resolveMember(ctx, m)
	if err != nil {
		return store.User{}, err
	}
	synced, err := s.profiles.SyncIdPProfile(ctx, user, m.Profile)
	if err != nil {
		slog.WarnContext(ctx, "idp profile sync", "user", user.ID.String(), "logto_user", m.ID, "err", err)
	}
	return synced, nil
}

func (s *Service) resolveMember(ctx context.Context, m Member) (store.User, error) {
	user, err := s.q.GetUserByIdentity(ctx, store.GetUserByIdentityParams{
		OidcIss: s.issuer, OidcSub: m.ID,
	})
	if err == nil {
		return user, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return store.User{}, err
	}
	user, err = s.q.CreateUser(ctx, store.CreateUserParams{
		OidcSub: m.ID, OidcIss: s.issuer, Email: m.Email, Name: m.Name,
	})
	if err == nil {
		return user, nil
	}
	// The address already belongs to an account under a different (iss, sub)
	// — an issuer that was renamed, or a row this server made before the
	// organization existed. Re-point it rather than leaving a duplicate that
	// the owner's first sign-in would then collide with.
	if !isUniqueViolation(err) || m.Email == "" {
		return store.User{}, err
	}
	existing, lookupErr := s.q.GetUserByEmail(ctx, m.Email)
	if lookupErr != nil {
		return store.User{}, err
	}
	return s.q.AdoptUserIdentity(ctx, store.AdoptUserIdentityParams{
		ID: existing.ID, OidcIss: s.issuer, OidcSub: m.ID, Email: m.Email, Name: m.Name,
	})
}

func isUniqueViolation(err error) bool {
	var pgErr interface{ SQLState() string }
	if errors.As(err, &pgErr) {
		return pgErr.SQLState() == "23505"
	}
	return false
}

// --- write path ---

// Outcome of adding somebody to a space. Two shapes, because an address with
// no account upstream cannot be made a member — only invited to become one.
type Outcome struct {
	Invited bool
	UserID  pgtype.UUID
	Email   string
}

// Add puts an address into the space's organization, or invites it.
//
// Upstream first, local second. The reverse order would produce a member who
// exists only in pp-hub, which the next reconcile would delete — silently
// undoing what the admin just did.
func (s *Service) Add(ctx context.Context, space store.Space, email string, asAdmin bool) (Outcome, error) {
	orgID, err := s.orgOf(space)
	if err != nil {
		return Outcome{}, err
	}
	email = strings.TrimSpace(email)
	member, found, err := s.client.FindUserByEmail(ctx, email)
	if err != nil {
		return Outcome{}, err
	}
	if !found {
		if err := s.client.Invite(ctx, orgID, email, asAdmin, inviteTTL); err != nil {
			return Outcome{}, err
		}
		return Outcome{Invited: true, Email: email}, nil
	}
	if err := s.client.AddMember(ctx, orgID, member.ID); err != nil {
		return Outcome{}, err
	}
	if err := s.client.SetAdmin(ctx, orgID, member.ID, asAdmin); err != nil {
		return Outcome{}, err
	}
	user, err := s.ensureUser(ctx, member)
	if err != nil {
		return Outcome{}, err
	}
	if err := s.applyMembership(ctx, space.ID, user.ID, asAdmin); err != nil {
		return Outcome{}, err
	}
	return Outcome{UserID: user.ID, Email: email}, nil
}

// Remove takes somebody out of the organization, then revokes locally and
// queues the handover.
func (s *Service) Remove(ctx context.Context, space store.Space, user store.User) error {
	orgID, err := s.orgOf(space)
	if err != nil {
		return err
	}
	// A 404 upstream means they are already gone from the organization —
	// which is the state being asked for, so the local half still runs.
	if err := s.client.RemoveMember(ctx, orgID, user.OidcSub); err != nil && Status(err) != 404 {
		return err
	}
	return s.depart(ctx, space.ID, user.ID, SourceAdmin)
}

// SetAdmin moves the organization role, then mirrors it onto the Admin group.
func (s *Service) SetAdmin(ctx context.Context, space store.Space, user store.User, asAdmin bool) error {
	orgID, err := s.orgOf(space)
	if err != nil {
		return err
	}
	if err := s.client.SetAdmin(ctx, orgID, user.OidcSub, asAdmin); err != nil {
		return err
	}
	return s.spaces.ApplyAdmin(ctx, space.ID, user.ID, asAdmin)
}

func (s *Service) orgOf(space store.Space) (string, error) {
	if !s.Enabled() {
		return "", ErrDisabled
	}
	if !space.IdpOrgID.Valid || space.IdpOrgID.String == "" {
		return "", ErrNotLinked
	}
	return space.IdpOrgID.String, nil
}

// --- worker ---

type jobPayload struct {
	OrgID string `json:"org_id"`
}

// Enqueue schedules a reconcile. The webhook endpoint does nothing else: a
// delivery must be acknowledged in milliseconds, and the work it describes
// takes a round trip to Logto.
func (s *Service) Enqueue(ctx context.Context, orgID string) error {
	payload, err := json.Marshal(jobPayload{OrgID: orgID})
	if err != nil {
		return err
	}
	_, err = s.q.EnqueueJob(ctx, store.EnqueueJobParams{Kind: JobKind, Payload: payload})
	return err
}

// Run drains reconcile jobs and sweeps every linked space hourly.
//
// The sweep is not redundant with the webhook. A delivery can be lost, a
// signature can be rotated, and the endpoint can be unreachable during a
// deploy; without a floor on staleness, any of those leaves a former employee
// in a space until they happen to sign in — which, having left, they will not.
func (s *Service) Run(ctx context.Context) {
	if !s.Enabled() {
		// Two very different states, and an operator debugging "why did
		// nothing happen when I removed them in Logto" needs to be able to
		// tell them apart from one line.
		if s.Configured() {
			slog.InfoContext(ctx, "orgsync: sign-in claims only",
				"reason", "no M2M credentials",
				"effect", "no webhook is acted on and no hourly sweep runs; membership converges when each person signs in")
		} else {
			slog.InfoContext(ctx, "orgsync idle: no Logto tenant configured")
		}
		return
	}
	slog.InfoContext(ctx, "orgsync started", "kind", JobKind)
	jobs := time.NewTicker(3 * time.Second)
	defer jobs.Stop()
	sweep := time.NewTicker(time.Hour)
	defer sweep.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-jobs.C:
			s.drain(ctx)
		case <-sweep.C:
			s.sweep(ctx)
		}
	}
}

func (s *Service) drain(ctx context.Context) {
	claimed, err := s.q.ClaimJobs(ctx, store.ClaimJobsParams{Kinds: []string{JobKind}, Lim: 5})
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) && ctx.Err() == nil {
			slog.WarnContext(ctx, "claim reconcile jobs", "err", err)
		}
		return
	}
	for _, job := range claimed {
		var payload jobPayload
		if err := json.Unmarshal(job.Payload, &payload); err != nil || payload.OrgID == "" {
			// Unparseable payload will never parse. Retrying burns attempts
			// and hides the real one behind it.
			_ = s.q.FailJob(ctx, store.FailJobParams{ID: job.ID, LastError: pgtype.Text{
				String: "reconcile payload carries no org_id", Valid: true,
			}})
			continue
		}
		if err := s.ReconcileOrg(ctx, payload.OrgID); err != nil {
			slog.WarnContext(ctx, "reconcile organization", "org", payload.OrgID, "err", err)
			_ = s.q.RequeueJob(ctx, store.RequeueJobParams{ID: job.ID, LastError: pgtype.Text{
				String: err.Error(), Valid: true,
			}})
			continue
		}
		_ = s.q.CompleteJob(ctx, job.ID)
	}
}

func (s *Service) sweep(ctx context.Context) {
	linked, err := s.q.ListLinkedSpaces(ctx)
	if err != nil {
		slog.WarnContext(ctx, "list linked spaces", "err", err)
		return
	}
	for _, space := range linked {
		if err := s.ReconcileOrg(ctx, space.IdpOrgID.String); err != nil {
			slog.WarnContext(ctx, "hourly reconcile", "org", space.IdpOrgID.String, "err", err)
		}
	}
}
