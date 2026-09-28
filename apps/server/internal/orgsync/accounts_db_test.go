package orgsync_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nhatbui/pp-hub/apps/server/internal/orgsync"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
	"github.com/nhatbui/pp-hub/apps/server/internal/users"
)

// logtoAccount creates a local account issued by the test tenant, signed in
// on one device, and registers its upstream twin with the fake.
func logtoAccount(t *testing.T, q *store.Queries, logto *fakeLogto, issuer, label string) store.User {
	t.Helper()
	ctx := context.Background()
	sub := fmt.Sprintf("sub-%s-%d-%s", label, os.Getpid(), t.Name())
	user, err := q.CreateUser(ctx, store.CreateUserParams{
		OidcSub: sub, OidcIss: issuer, Email: sub + "@test.local", Name: label,
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	if _, err := q.CreateSession(ctx, store.CreateSessionParams{
		UserID: user.ID, TokenHash: []byte("hash-" + sub),
		ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true},
	}); err != nil {
		t.Fatalf("create session: %v", err)
	}
	logto.accounts[sub] = map[string]any{"id": sub, "primaryEmail": user.Email, "name": label, "isSuspended": false}
	return user
}

func reload(t *testing.T, q *store.Queries, id pgtype.UUID) store.User {
	t.Helper()
	u, err := q.GetUser(context.Background(), id)
	if err != nil {
		t.Fatalf("reload user: %v", err)
	}
	return u
}

func sessionCount(t *testing.T, q *store.Queries, id pgtype.UUID) int {
	t.Helper()
	rows, err := q.ListUserSessions(context.Background(), id)
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	return len(rows)
}

// Logto refuses a suspended account's next sign-in, but a pp-hub session is a
// row that outlives that by up to 90 days. The suspension has to land here
// and end the sessions, and a reactivation has to lift it — Logto decides both.
func TestSuspensionUpstreamEndsSessionsAndReactivationLiftsIt(t *testing.T) {
	q, _, logto, svc := testEnv(t)
	ctx := context.Background()
	user := logtoAccount(t, q, logto, testIssuer, "suspendee")

	logto.accounts[user.OidcSub]["isSuspended"] = true
	if err := svc.ReconcileAccount(ctx, user.OidcSub); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if !reload(t, q, user.ID).SuspendedAt.Valid {
		t.Fatal("suspension upstream did not suspend the local account")
	}
	if n := sessionCount(t, q, user.ID); n != 0 {
		t.Errorf("%d sessions survived the suspension", n)
	}

	logto.accounts[user.OidcSub]["isSuspended"] = false
	if err := svc.ReconcileAccount(ctx, user.OidcSub); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if reload(t, q, user.ID).SuspendedAt.Valid {
		t.Error("reactivation upstream left the account suspended")
	}
}

// An account removed from the tenant is locked out at once. Its row stays:
// it owns files and history, and the departure handover needs it.
func TestAccountDeletedUpstreamIsSuspendedNotRemoved(t *testing.T) {
	q, _, logto, svc := testEnv(t)
	ctx := context.Background()
	user := logtoAccount(t, q, logto, testIssuer, "leaver")

	delete(logto.accounts, user.OidcSub)
	if err := svc.ReconcileAccount(ctx, user.OidcSub); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if !reload(t, q, user.ID).SuspendedAt.Valid {
		t.Error("an account Logto no longer has is still usable")
	}
	if n := sessionCount(t, q, user.ID); n != 0 {
		t.Errorf("%d sessions survived the deletion", n)
	}
}

// The address and name are Logto's. A changed address is mirrored; one that
// already belongs to another account is not, because overwriting it would be
// a merge nobody asked for.
func TestReconcileMirrorsTheUpstreamAddressUnlessAnotherAccountHoldsIt(t *testing.T) {
	q, _, logto, svc := testEnv(t)
	ctx := context.Background()
	user := logtoAccount(t, q, logto, testIssuer, "renamed")
	other := logtoAccount(t, q, logto, testIssuer, "bystander")

	moved := "moved-" + user.Email
	logto.accounts[user.OidcSub]["primaryEmail"] = moved
	logto.accounts[user.OidcSub]["name"] = "New Name"
	if err := svc.ReconcileAccount(ctx, user.OidcSub); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	got := reload(t, q, user.ID)
	if got.Email != moved || got.Name != "New Name" {
		t.Errorf("identity = (%q, %q), want (%q, %q)", got.Email, got.Name, moved, "New Name")
	}

	logto.accounts[user.OidcSub]["primaryEmail"] = other.Email
	if err := svc.ReconcileAccount(ctx, user.OidcSub); err != nil {
		t.Fatalf("reconcile onto a taken address: %v", err)
	}
	if got := reload(t, q, user.ID).Email; got != moved {
		t.Errorf("email = %q, want it kept at %q", got, moved)
	}
}

// The sweep lists the tenant once. An account missing from that listing must
// be confirmed gone on its own before it is locked out, or a short page
// becomes a mass suspension.
func TestAccountSweepConfirmsAbsenceBeforeActingOnIt(t *testing.T) {
	q, sp, logto, _ := testEnv(t)
	ctx := context.Background()
	// The sweep covers every account of its issuer, so this one gets an
	// issuer of its own rather than sweeping every other test's fixtures.
	issuer := fmt.Sprintf("https://sweep-%d.logto.test/oidc", time.Now().UnixNano())
	svc := orgsync.NewService(q, sp, users.NewService(q, nil), orgsync.NewClient(logto.settings()), issuer, "https://hub.test")
	hidden := logtoAccount(t, q, logto, issuer, "hidden")
	gone := logtoAccount(t, q, logto, issuer, "gone")

	logto.unlisted[hidden.OidcSub] = true
	delete(logto.accounts, gone.OidcSub)
	if err := svc.SweepAccounts(ctx); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if reload(t, q, hidden.ID).SuspendedAt.Valid {
		t.Error("an account the listing merely omitted was suspended")
	}
	if !reload(t, q, gone.ID).SuspendedAt.Valid {
		t.Error("an account Logto no longer has survived the sweep")
	}
}

// An organization's member list may not carry isSuspended. Absent must read
// as "unknown", never as "reactivate": otherwise every membership reconcile
// would undo a suspension.
func TestOrganizationReconcileWithoutSuspensionFieldKeepsTheSuspension(t *testing.T) {
	q, sp, logto, svc := testEnv(t)
	ctx := context.Background()
	_, orgID := linkedSpace(t, q, sp)
	user := logtoAccount(t, q, logto, testIssuer, "suspended-member")
	if err := q.SetUserSuspended(ctx, store.SetUserSuspendedParams{
		ID: user.ID, SuspendedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true},
	}); err != nil {
		t.Fatalf("suspend: %v", err)
	}

	logto.roster = []map[string]any{member(user.OidcSub, user.Email, false)}
	if err := svc.ReconcileOrg(ctx, orgID); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if !reload(t, q, user.ID).SuspendedAt.Valid {
		t.Error("a roster without isSuspended reactivated the account")
	}
}
