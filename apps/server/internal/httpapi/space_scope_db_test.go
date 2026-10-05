package httpapi

import (
	"context"
	"fmt"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nhatbui/pp-hub/apps/server/internal/activity"
	"github.com/nhatbui/pp-hub/apps/server/internal/fs"
	"github.com/nhatbui/pp-hub/apps/server/internal/share"
	"github.com/nhatbui/pp-hub/apps/server/internal/spaces"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

// The ?space= filter on recent, starred and shared lives in SQL, next to the
// LIMIT it has to run before: a post-filter would hand back a short page
// whenever another space's rows filled the limit first. That cannot be
// asserted against a fake, so this file follows
// internal/spaces/permissions_db_test.go — a real Postgres or a loud skip:
//
//	PP_TEST_DATABASE_URL=postgres://pphub:pphub@localhost:5433/pphub go test ./internal/httpapi/
func feedEnv(t *testing.T) (*Server, *store.Queries) {
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
	// Only the services the three feeds and the reachability filter reach:
	// no blob store is touched by a listing.
	return &Server{
		spaces:   spaces.NewService(q, nil),
		fs:       fs.NewService(q, nil),
		share:    share.NewService(q),
		activity: activity.NewService(q),
		store:    q,
	}, q
}

// feedUser creates a throwaway account, unique per run so a previous run's
// leftovers cannot stand in for this run's fixtures.
func feedUser(t *testing.T, q *store.Queries, label string) store.User {
	t.Helper()
	sub := fmt.Sprintf("test-%s-%d-%d", label, os.Getpid(), time.Now().UnixNano())
	u, err := q.CreateUser(context.Background(), store.CreateUserParams{
		OidcSub: sub,
		OidcIss: "test",
		Email:   sub + "@test.local",
		Name:    label,
		// A sign-in through Logto: the address is verified.
		EmailVerified: true,
	})
	if err != nil {
		t.Fatalf("create user %s: %v", label, err)
	}
	return u
}

// spaceFixture is one space holding everything the three feeds read: a file
// the caller touched and starred, and a folder somebody else shared with them.
type spaceFixture struct {
	space store.Space
	file  store.Node
	dir   store.Node
}

func TestSpaceFilterScopesRecentStarredAndShared(t *testing.T) {
	srv, q := feedEnv(t)
	ctx := context.Background()

	caller := feedUser(t, q, "scope-caller")
	colleague := feedUser(t, q, "scope-colleague")

	build := func(label string) spaceFixture {
		t.Helper()
		sp, err := srv.spaces.Create(ctx, fmt.Sprintf("scope-%s-%d", label, os.Getpid()), caller.ID)
		if err != nil {
			t.Fatalf("create space %s: %v", label, err)
		}
		t.Cleanup(func() { _ = q.DeleteSpace(ctx, sp.ID) })
		if err := srv.spaces.AddMember(ctx, sp.ID, colleague.ID, false); err != nil {
			t.Fatalf("add member: %v", err)
		}

		// Recent reads activity_events joined to the node; starred reads
		// node_stars. The caller created the file, so the reachability filter
		// resolves them as owner and keeps the row.
		file, err := q.CreateNode(ctx, store.CreateNodeParams{
			SpaceID: sp.ID, Kind: store.NodeKindFile, Name: label + ".txt", CreatedBy: caller.ID,
		})
		if err != nil {
			t.Fatalf("create file: %v", err)
		}
		if err := q.RecordActivity(ctx, store.RecordActivityParams{
			SpaceID: sp.ID, ActorID: caller.ID, NodeID: file.ID,
			Kind: "upload", NodeName: file.Name, Detail: []byte("{}"),
		}); err != nil {
			t.Fatalf("record activity: %v", err)
		}
		if err := q.StarNode(ctx, store.StarNodeParams{UserID: caller.ID, NodeID: file.ID}); err != nil {
			t.Fatalf("star: %v", err)
		}

		// Shared lists entry points, which means a grant on something the
		// caller did not create.
		dir, err := q.CreateNode(ctx, store.CreateNodeParams{
			SpaceID: sp.ID, Kind: store.NodeKindDir, Name: label + "-shared", CreatedBy: colleague.ID,
		})
		if err != nil {
			t.Fatalf("create dir: %v", err)
		}
		if _, err := q.CreateShare(ctx, store.CreateShareParams{
			NodeID: dir.ID, Kind: store.ShareKindDirect, GranteeUserID: caller.ID,
			Level: store.ShareLevelView, CanDownload: true, CreatedBy: colleague.ID,
		}); err != nil {
			t.Fatalf("grant: %v", err)
		}
		return spaceFixture{space: sp, file: file, dir: dir}
	}

	alpha := build("alpha")
	beta := build("beta")

	recent := func(spaceID pgtype.UUID) []pgtype.UUID {
		t.Helper()
		rows, err := srv.activity.Recent(ctx, caller.ID, spaceID)
		if err != nil {
			t.Fatalf("recent: %v", err)
		}
		rows, _, err = reachableRows(ctx, srv, caller.ID, rows,
			func(n store.ListRecentNodesRow) pgtype.UUID { return n.ID },
			func(n store.ListRecentNodesRow) pgtype.UUID { return n.SpaceID })
		if err != nil {
			t.Fatalf("recent reachability: %v", err)
		}
		ids := make([]pgtype.UUID, 0, len(rows))
		for _, n := range rows {
			ids = append(ids, n.ID)
		}
		return ids
	}
	starred := func(spaceID pgtype.UUID, limit int32) []pgtype.UUID {
		t.Helper()
		rows, err := srv.fs.Starred(ctx, caller.ID, spaceID, limit)
		if err != nil {
			t.Fatalf("starred: %v", err)
		}
		rows, _, err = reachableRows(ctx, srv, caller.ID, rows,
			func(n store.ListStarredNodesRow) pgtype.UUID { return n.ID },
			func(n store.ListStarredNodesRow) pgtype.UUID { return n.SpaceID })
		if err != nil {
			t.Fatalf("starred reachability: %v", err)
		}
		ids := make([]pgtype.UUID, 0, len(rows))
		for _, n := range rows {
			ids = append(ids, n.ID)
		}
		return ids
	}
	shared := func(spaceID pgtype.UUID) []pgtype.UUID {
		t.Helper()
		rows, err := srv.fs.ListShared(ctx, caller.ID, spaceID)
		if err != nil {
			t.Fatalf("shared: %v", err)
		}
		ids := make([]pgtype.UUID, 0, len(rows))
		for _, n := range rows {
			ids = append(ids, n.ID)
		}
		return ids
	}

	var allSpaces pgtype.UUID // the absent ?space= param
	for _, c := range []struct {
		feed     string
		scoped   []pgtype.UUID
		unscoped []pgtype.UUID
		want     pgtype.UUID
		other    pgtype.UUID
	}{
		{"recent", recent(alpha.space.ID), recent(allSpaces), alpha.file.ID, beta.file.ID},
		{"starred", starred(alpha.space.ID, 25), starred(allSpaces, 25), alpha.file.ID, beta.file.ID},
		{"shared", shared(alpha.space.ID), shared(allSpaces), alpha.dir.ID, beta.dir.ID},
	} {
		if !contains(c.scoped, c.want) {
			t.Errorf("%s scoped to alpha dropped alpha's own row", c.feed)
		}
		if contains(c.scoped, c.other) {
			t.Errorf("%s scoped to alpha still returned beta's row", c.feed)
		}
		if !contains(c.unscoped, c.want) || !contains(c.unscoped, c.other) {
			t.Errorf("%s without ?space= must span both spaces, got %d rows covering alpha=%v beta=%v",
				c.feed, len(c.unscoped), contains(c.unscoped, c.want), contains(c.unscoped, c.other))
		}
	}

	// The reason the predicate has to be in the WHERE clause and not in the
	// handler: beta was starred last, so it fills a one-row page. A filter
	// applied after the LIMIT would return nothing for alpha here.
	if ids := starred(allSpaces, 1); len(ids) != 1 || ids[0] != beta.file.ID {
		t.Fatalf("expected beta's star to fill a one-row page, got %v", ids)
	}
	if ids := starred(alpha.space.ID, 1); len(ids) != 1 || ids[0] != alpha.file.ID {
		t.Errorf("starred scoped to alpha with limit 1 = %v, want alpha's file: the space filter must run before the LIMIT", ids)
	}
}

func contains(ids []pgtype.UUID, want pgtype.UUID) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}

// A malformed ?space= is the client's mistake, and it has to be caught before
// anything reaches the database — the value is interpolated into a uuid
// predicate. An absent one is not a mistake: it means "every space", which the
// queries spell as a null uuid.
func TestOptionalSpaceParam(t *testing.T) {
	srv := &Server{} // rejection happens before any service is consulted

	rec := httptest.NewRecorder()
	got, ok := srv.optionalSpaceParam(rec, httptest.NewRequest("GET", "/api/v1/recent?space=not-a-uuid", nil), uid(7))
	if ok {
		t.Error("a malformed space id must stop the request")
	}
	if rec.Code != 400 {
		t.Errorf("malformed space id = %d, want 400", rec.Code)
	}
	if got.Valid {
		t.Errorf("a rejected space id must not be passed down, got %v", got)
	}

	rec = httptest.NewRecorder()
	got, ok = srv.optionalSpaceParam(rec, httptest.NewRequest("GET", "/api/v1/recent", nil), uid(7))
	if !ok || got.Valid {
		t.Errorf("an absent space param = (%v, %v), want a null uuid and no error", got, ok)
	}
	if rec.Code != 200 {
		t.Errorf("an absent space param must not answer the request, got %d", rec.Code)
	}
}
