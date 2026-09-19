package users_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nhatbui/pp-hub/apps/server/internal/store"
	"github.com/nhatbui/pp-hub/apps/server/internal/users"
)

// The autocomplete's company boundary lives in SQL — a self-join over
// space_members inside SearchUsers — so it cannot be asserted against a fake.
// This file follows internal/spaces/permissions_db_test.go: a real Postgres or
// a loud skip.
//
//	PP_TEST_DATABASE_URL=postgres://pphub:pphub@localhost:5433/pphub go test ./internal/users/
func searchEnv(t *testing.T) (*users.Service, *store.Queries) {
	t.Helper()
	url := os.Getenv("PP_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("PP_TEST_DATABASE_URL not set; skipping database-backed test")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := store.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	q := store.New(pool)
	return users.NewService(q), q
}

// searchUser creates a throwaway account whose display name carries the run's
// unique token, so the query below matches this run's fixtures and nothing a
// previous run left behind.
func searchUser(t *testing.T, q *store.Queries, token, label string) store.User {
	t.Helper()
	sub := fmt.Sprintf("test-%s-%s", token, label)
	u, err := q.CreateUser(context.Background(), store.CreateUserParams{
		OidcSub: sub,
		OidcIss: "test",
		Email:   sub + "@test.local",
		Name:    label,
	})
	if err != nil {
		t.Fatalf("create user %s: %v", label, err)
	}
	return u
}

func searchSpace(t *testing.T, q *store.Queries, name string, members ...pgtype.UUID) {
	t.Helper()
	ctx := context.Background()
	sp, err := q.CreateSpace(ctx, store.CreateSpaceParams{Name: name, OwnerID: members[0]})
	if err != nil {
		t.Fatalf("create space %s: %v", name, err)
	}
	t.Cleanup(func() { _ = q.DeleteSpace(ctx, sp.ID) })
	for _, m := range members {
		if err := q.AddSpaceMember(ctx, store.AddSpaceMemberParams{SpaceID: sp.ID, UserID: m}); err != nil {
			t.Fatalf("add member: %v", err)
		}
	}
}

// A space is a company: the picker must show colleagues and nobody else, or it
// doubles as a tenant-wide address book that leaks one customer's staff list
// to another's.
func TestSearchIsLimitedToSharedSpaces(t *testing.T) {
	svc, q := searchEnv(t)
	ctx := context.Background()

	token := fmt.Sprintf("srch%d%d", os.Getpid(), time.Now().UnixNano())
	caller := searchUser(t, q, token, "caller")
	colleague := searchUser(t, q, token, "colleague")
	outsider := searchUser(t, q, token, "outsider")

	searchSpace(t, q, "search-ours-"+token, caller.ID, colleague.ID)
	searchSpace(t, q, "search-theirs-"+token, outsider.ID)

	found := func(callerID pgtype.UUID, unrestricted bool) map[string]bool {
		t.Helper()
		rows, err := svc.Search(ctx, "default", callerID, unrestricted, token, 50)
		if err != nil {
			t.Fatalf("search: %v", err)
		}
		out := make(map[string]bool, len(rows))
		for _, r := range rows {
			out[r.Email] = true
		}
		return out
	}

	got := found(caller.ID, false)
	if !got[colleague.Email] {
		t.Error("a colleague in the same space should be searchable")
	}
	if got[outsider.Email] {
		t.Error("a user sharing no space with the caller leaked into autocomplete")
	}
	if !got[caller.Email] {
		t.Error("the caller should find their own account")
	}

	// The admin console already lists every account, so narrowing its search
	// would only make the console lie about who exists.
	if admin := found(caller.ID, true); !admin[outsider.Email] {
		t.Error("an instance admin should still see every account")
	}

	// Symmetric: the outsider must not see the other company either.
	if theirs := found(outsider.ID, false); theirs[colleague.Email] {
		t.Error("search is not symmetric — the outsider saw another space's member")
	}
}
