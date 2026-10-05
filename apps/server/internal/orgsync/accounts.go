package orgsync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nhatbui/pp-hub/apps/server/internal/audit"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

// Accounts are Logto's the way organizations are: whether a person may use
// pp-hub at all, and what their address and name are, is decided upstream and
// mirrored here. The signals are the same shape as for organizations — a
// webhook that only means "user X changed", an hourly sweep, and the write
// path going upstream first — and, again, nothing applies a delta.
//
// Suspension matters most. Logto refuses a suspended account's next sign-in,
// but a pp-hub session is a row that lives for up to 90 days; without this a
// person suspended upstream kept every open tab working, and one removed from
// the tenant kept theirs until the sweep noticed they had left their
// organizations.

// UserJobKind routes an account reconcile onto the orgsync worker.
const UserJobKind = "user:reconcile"

// ErrNoIdentity: the account was not issued by this Logto tenant, so there is
// no upstream account to change.
var ErrNoIdentity = errors.New("orgsync: account has no identity in this Logto tenant")

// Sources recorded on an account change that Logto, not an admin in pp-hub,
// caused.
const (
	reasonSuspended = "suspended upstream"
	reasonDeleted   = "deleted upstream"
)

// EnqueueUser schedules an account reconcile for a Logto user id.
func (s *Service) EnqueueUser(ctx context.Context, logtoUserID string) error {
	payload, err := json.Marshal(jobPayload{UserID: logtoUserID})
	if err != nil {
		return err
	}
	_, err = s.q.EnqueueJob(ctx, store.EnqueueJobParams{Kind: UserJobKind, Payload: payload})
	return err
}

// ReconcileAccount converges one local account against its Logto user.
//
// An account nobody here has seen is not an error and not a reason to create
// one: accounts are created at sign-in or when an organization reconcile
// finds a member, and a user outside every organization reaches nothing.
func (s *Service) ReconcileAccount(ctx context.Context, logtoUserID string) error {
	local, err := s.q.GetUserByIdentity(ctx, store.GetUserByIdentityParams{OidcIss: s.issuer, OidcSub: logtoUserID})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	upstream, err := s.client.User(ctx, logtoUserID)
	if Status(err) == http.StatusNotFound {
		return s.accountGone(ctx, local)
	}
	if err != nil {
		return err
	}
	_, err = s.applyAccount(ctx, local, upstream)
	return err
}

// applyAccount makes the local row agree with the upstream account: identity
// fields, profile fields and — when the response carried it — suspension.
func (s *Service) applyAccount(ctx context.Context, local store.User, m Member) (store.User, error) {
	user, err := s.applyIdentity(ctx, local, m)
	if err != nil {
		return local, err
	}
	if synced, err := s.profiles.SyncIdPProfile(ctx, user, m.Profile); err != nil {
		slog.WarnContext(ctx, "idp profile sync", "user", user.ID.String(), "logto_user", m.ID, "err", err)
	} else {
		user = synced
	}
	if m.Suspended != nil && *m.Suspended != user.SuspendedAt.Valid {
		if err := s.applySuspension(ctx, user, *m.Suspended, reasonSuspended); err != nil {
			return user, err
		}
	}
	return user, nil
}

// applyIdentity mirrors the address and name Logto holds.
//
// Logto's primaryEmail is verified by construction — set through a
// verification flow or by an operator — so it is taken as is, unlike an
// unverified ID-token claim. An address another account already holds is left
// alone and logged: overwriting it would need a merge nobody asked for.
func (s *Service) applyIdentity(ctx context.Context, user store.User, m Member) (store.User, error) {
	email, name := user.Email, user.Name
	if m.Email != "" {
		email = m.Email
	}
	if m.Name != "" {
		name = m.Name
	}
	if email == user.Email && name == user.Name && user.EmailVerified == (email != "") {
		return user, nil
	}
	updated, err := s.q.SetUserIdpIdentity(ctx, store.SetUserIdpIdentityParams{ID: user.ID, Email: email, Name: name})
	if isUniqueViolation(err) {
		slog.WarnContext(ctx, "idp email already belongs to another account; keeping the old one",
			"user", user.ID.String(), "logto_user", m.ID, "email", email)
		return user, nil
	}
	if err != nil {
		return user, err
	}
	return updated, nil
}

// applySuspension writes the suspension and, when suspending, ends every
// session: the per-request check already locks the account out, but a
// reactivation must not silently bring old devices back.
func (s *Service) applySuspension(ctx context.Context, user store.User, suspended bool, reason string) error {
	var at pgtype.Timestamptz
	if suspended {
		at = pgtype.Timestamptz{Time: time.Now(), Valid: true}
	}
	if err := s.q.SetUserSuspended(ctx, store.SetUserSuspendedParams{ID: user.ID, SuspendedAt: at}); err != nil {
		return err
	}
	kind := audit.KindUnsuspended
	detail := map[string]any{"via": "logto"}
	if suspended {
		kind = audit.KindSuspended
		detail["reason"] = reason
		n, err := s.q.DeleteUserSessions(ctx, user.ID)
		if err != nil {
			return err
		}
		detail["sessions_revoked"] = n
	}
	s.audit.RecordSystem(ctx, audit.Event{Kind: kind, UserID: user.ID, Subject: user.Email, Detail: detail}, "")
	slog.InfoContext(ctx, "account suspension mirrored from logto",
		"user", user.ID.String(), "suspended", suspended, "reason", reason)
	return nil
}

// accountGone handles an account Logto no longer has.
//
// The local row stays — it owns files, grants and history, and the Archive
// handover needs it — but it is suspended with its sessions ended, and every
// organization it was in is reconciled now rather than at the next sweep, so
// its memberships become departures an admin can hand over. A personal space
// has nobody to hand over to: it is archived, which takes it out of every
// listing and ends the external grants into it, and an instance admin purges
// it.
func (s *Service) accountGone(ctx context.Context, user store.User) error {
	if !user.SuspendedAt.Valid {
		if err := s.applySuspension(ctx, user, true, reasonDeleted); err != nil {
			return err
		}
	}
	if err := s.q.ArchivePersonalSpace(ctx, user.ID); err != nil {
		return err
	}
	mine, err := s.q.ListMySpaces(ctx, user.ID)
	if err != nil {
		return err
	}
	for _, space := range mine {
		if !space.IdpOrgID.Valid {
			continue
		}
		if err := s.Enqueue(ctx, space.IdpOrgID.String); err != nil {
			return err
		}
	}
	return nil
}

// SetSuspended is the write path: an instance admin's suspension goes to
// Logto first, so the next sweep does not undo it, and the caller applies the
// local half.
func (s *Service) SetSuspended(ctx context.Context, user store.User, suspended bool) error {
	if user.OidcIss != s.issuer {
		return ErrNoIdentity
	}
	return s.client.SetSuspended(ctx, user.OidcSub, suspended)
}

// sweepAccounts converges every account this tenant issued.
//
// One listing covers everybody who still exists. An account missing from it is
// confirmed gone with its own lookup before anything happens to it, so a
// truncated page can never read as a mass deletion.
func (s *Service) sweepAccounts(ctx context.Context) error {
	upstream, err := s.client.Users(ctx)
	if err != nil {
		return fmt.Errorf("list logto users: %w", err)
	}
	byID := make(map[string]Member, len(upstream))
	for _, m := range upstream {
		byID[m.ID] = m
	}
	locals, err := s.q.ListUsersByIssuer(ctx, s.issuer)
	if err != nil {
		return err
	}
	for _, local := range locals {
		if m, ok := byID[local.OidcSub]; ok {
			if _, err := s.applyAccount(ctx, local, m); err != nil {
				slog.WarnContext(ctx, "account sweep", "user", local.ID.String(), "err", err)
			}
			continue
		}
		if err := s.ReconcileAccount(ctx, local.OidcSub); err != nil {
			slog.WarnContext(ctx, "account sweep", "user", local.ID.String(), "err", err)
		}
	}
	return nil
}
