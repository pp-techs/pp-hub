package auth

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

// Sessions are rows, and everything that makes them safe — the idle deadline
// in the lookup predicate, the user id in the revoke predicate, the cascade
// from users — lives in SQL. None of it can be asserted against a fake. Same
// contract as internal/spaces/permissions_db_test.go: a real Postgres or a
// loud skip.
//
//	PP_TEST_DATABASE_URL=postgres://pphub:pphub@localhost:5433/pphub go test ./internal/auth/
func testQueries(t *testing.T) *store.Queries {
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
	return store.New(pool)
}

func testUser(t *testing.T, q *store.Queries, label string) store.User {
	t.Helper()
	sub := fmt.Sprintf("session-test-%s-%d-%d", label, os.Getpid(), time.Now().UnixNano())
	u, err := q.CreateUser(context.Background(), store.CreateUserParams{
		OidcSub: sub,
		OidcIss: "test",
		Email:   sub + "@test.local",
		Name:    label,
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	return u
}

func newSession(t *testing.T, q *store.Queries, userID pgtype.UUID, expiresIn time.Duration) (store.Session, []byte) {
	t.Helper()
	token := fmt.Sprintf("token-%d-%d", os.Getpid(), time.Now().UnixNano())
	sum := sha256.Sum256([]byte(token))
	s, err := q.CreateSession(context.Background(), store.CreateSessionParams{
		UserID:    userID,
		TokenHash: sum[:],
		UserAgent: "test-agent",
		Ip:        "127.0.0.1",
		ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(expiresIn), Valid: true},
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	return s, sum[:]
}

// The lookup returns the owner alongside the session, which is what lets one
// indexed read replace the cookie-verify-then-fetch-user pair.
func TestSessionLookupReturnsOwner(t *testing.T) {
	q := testQueries(t)
	u := testUser(t, q, "owner")
	sess, hash := newSession(t, q, u.ID, time.Hour)

	row, err := q.GetSessionByHash(context.Background(), hash)
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if row.Session.ID != sess.ID {
		t.Errorf("session id = %v, want %v", row.Session.ID, sess.ID)
	}
	if row.User.ID != u.ID {
		t.Errorf("user id = %v, want %v", row.User.ID, u.ID)
	}
}

// An expired session must be unreachable through the lookup, not merely
// scheduled for cleanup — the sweep runs every five minutes and enforcement
// cannot wait for it.
func TestExpiredSessionDoesNotResolve(t *testing.T) {
	q := testQueries(t)
	u := testUser(t, q, "expired")
	_, hash := newSession(t, q, u.ID, -time.Minute)

	if _, err := q.GetSessionByHash(context.Background(), hash); err != pgx.ErrNoRows {
		t.Fatalf("expired session resolved (err = %v)", err)
	}
}

// Sliding the deadline has to bring an almost-dead session back into the
// lookup, and it has to move users.last_seen with it — that column is what the
// People table shows, and writing it only at login made it a lie.
func TestTouchSessionSlidesDeadlineAndLastSeen(t *testing.T) {
	q := testQueries(t)
	ctx := context.Background()
	u := testUser(t, q, "touched")
	sess, hash := newSession(t, q, u.ID, time.Minute)

	if u.LastSeen.Valid {
		t.Fatal("a freshly created user should have no last_seen")
	}

	if err := q.TouchSession(ctx, store.TouchSessionParams{
		ID:        sess.ID,
		ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(72 * time.Hour), Valid: true},
	}); err != nil {
		t.Fatalf("touch: %v", err)
	}

	row, err := q.GetSessionByHash(ctx, hash)
	if err != nil {
		t.Fatalf("lookup after touch: %v", err)
	}
	if time.Until(row.Session.ExpiresAt.Time) < 48*time.Hour {
		t.Errorf("deadline not slid: expires at %v", row.Session.ExpiresAt.Time)
	}
	if !row.User.LastSeen.Valid {
		t.Error("touching a session did not set users.last_seen")
	}
}

// The user id is part of the revoke predicate, so knowing another person's
// session id is not enough to end it.
func TestRevokeIsScopedToOwner(t *testing.T) {
	q := testQueries(t)
	ctx := context.Background()
	victim := testUser(t, q, "victim")
	attacker := testUser(t, q, "attacker")
	sess, hash := newSession(t, q, victim.ID, time.Hour)

	n, err := q.DeleteUserSession(ctx, store.DeleteUserSessionParams{ID: sess.ID, UserID: attacker.ID})
	if err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if n != 0 {
		t.Fatalf("attacker revoked %d of someone else's sessions", n)
	}
	if _, err := q.GetSessionByHash(ctx, hash); err != nil {
		t.Fatalf("victim's session died anyway: %v", err)
	}

	n, err = q.DeleteUserSession(ctx, store.DeleteUserSessionParams{ID: sess.ID, UserID: victim.ID})
	if err != nil {
		t.Fatalf("owner revoke: %v", err)
	}
	if n != 1 {
		t.Fatalf("owner revoked %d rows, want 1", n)
	}
}

// "Sign out everywhere else" must keep exactly the session asking for it.
func TestRevokeOthersKeepsCurrent(t *testing.T) {
	q := testQueries(t)
	ctx := context.Background()
	u := testUser(t, q, "multi")
	current, currentHash := newSession(t, q, u.ID, time.Hour)
	_, otherHash := newSession(t, q, u.ID, time.Hour)
	_, thirdHash := newSession(t, q, u.ID, time.Hour)

	n, err := q.DeleteOtherUserSessions(ctx, store.DeleteOtherUserSessionsParams{UserID: u.ID, ID: current.ID})
	if err != nil {
		t.Fatalf("revoke others: %v", err)
	}
	if n != 2 {
		t.Fatalf("revoked %d sessions, want 2", n)
	}
	if _, err := q.GetSessionByHash(ctx, currentHash); err != nil {
		t.Fatalf("current session was revoked: %v", err)
	}
	for _, h := range [][]byte{otherHash, thirdHash} {
		if _, err := q.GetSessionByHash(ctx, h); err != pgx.ErrNoRows {
			t.Errorf("other session survived (err = %v)", err)
		}
	}
}

// Sessions listed for the account page exclude the expired ones, or the
// Sessions tab shows devices that cannot actually get in.
func TestListExcludesExpired(t *testing.T) {
	q := testQueries(t)
	ctx := context.Background()
	u := testUser(t, q, "listed")
	live, _ := newSession(t, q, u.ID, time.Hour)
	dead, _ := newSession(t, q, u.ID, -time.Hour)

	rows, err := q.ListUserSessions(ctx, u.ID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var sawLive, sawDead bool
	for _, r := range rows {
		sawLive = sawLive || r.ID == live.ID
		sawDead = sawDead || r.ID == dead.ID
	}
	if !sawLive {
		t.Error("live session missing from the list")
	}
	if sawDead {
		t.Error("expired session listed")
	}
}
