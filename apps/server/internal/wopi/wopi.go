// Package wopi implements the WOPI host side for OnlyOffice.
// See docs/architecture/office-integration.md.
package wopi

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

var (
	ErrInvalidToken = errors.New("invalid or expired WOPI token")
	ErrLockConflict = errors.New("file is locked by another session")
	ErrNotLocked    = errors.New("no active lock")
)

// TokenTTL is how long an editor session stays valid.
const TokenTTL = 10 * time.Hour

// SystemSubject is the token subject for a machine-initiated read with no
// user behind it — currently only the preview worker's conversion fetch.
// handleWopiGetFile authorizes on the token and never reads the subject.
const SystemSubject = "00000000-0000-0000-0000-000000000000"

// Service signs WOPI access tokens and manages file locks.
type Service struct {
	q   *store.Queries
	key []byte
}

func NewService(q *store.Queries, key []byte) *Service {
	return &Service{q: q, key: key}
}

// --- Access tokens ---
// Format: "w1.<userID>.<nodeID>.<permRank>.<expiryUnix>.<hmac>" — all URL-safe.

// SignToken issues a token for user+node with the resolved permission rank
// (1=view, 2=download, 3=edit).
func (s *Service) SignToken(userID, nodeID pgtype.UUID, permRank int) (string, error) {
	return SignTokenWithTTL(s.key, userID, nodeID, permRank, TokenTTL)
}

// SignTokenWithTTL mints a token without needing a Service.
//
// cmd/worker uses this to hand OnlyOffice's conversion API a URL it can fetch
// the source document from: the API fetches by URL rather than accepting an
// upload, and the worker has no listener of its own to serve one. It holds the
// session key from config, so it can sign a short-lived, read-only token
// against the API server's existing /wopi/files/{id}/contents route rather
// than needing a new endpoint.
//
// Keep the TTL small. Unlike an editor session, nothing here is waiting on a
// human — the fetch happens within seconds of minting.
func SignTokenWithTTL(key []byte, userID, nodeID pgtype.UUID, permRank int, ttl time.Duration) (string, error) {
	uid, _ := userID.Value()
	nid, _ := nodeID.Value()
	if uid == nil {
		// A conversion fetch acts for no user. The nil UUID is the subject
		// rather than an empty string, because VerifyToken scans this field
		// into a pgtype.UUID and "" fails to parse — the token would mint
		// fine and then be rejected on use.
		uid = SystemSubject
	}
	if nid == nil {
		return "", fmt.Errorf("wopi: node id required")
	}
	exp := time.Now().Add(ttl)
	var expBuf [8]byte
	binary.BigEndian.PutUint64(expBuf[:], uint64(exp.Unix()))
	payload := fmt.Sprintf("%s.%s.%d.%s",
		uid.(string), nid.(string), permRank,
		base64.RawURLEncoding.EncodeToString(expBuf[:]))
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(payload))
	return "w1." + payload + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

// TokenClaims is the parsed payload.
type TokenClaims struct {
	UserID   pgtype.UUID
	NodeID   pgtype.UUID
	PermRank int
}

// VerifyToken parses and validates a token.
func (s *Service) VerifyToken(value string) (TokenClaims, error) {
	var c TokenClaims
	parts := strings.Split(value, ".")
	if len(parts) != 6 || parts[0] != "w1" {
		return c, ErrInvalidToken
	}
	payload := strings.Join(parts[1:5], ".")
	mac := hmac.New(sha256.New, s.key)
	mac.Write([]byte(payload))
	expected, err := base64.RawURLEncoding.DecodeString(parts[5])
	if err != nil || !hmac.Equal(mac.Sum(nil), expected) {
		return c, ErrInvalidToken
	}
	expRaw, err := base64.RawURLEncoding.DecodeString(parts[4])
	if err != nil || len(expRaw) != 8 {
		return c, ErrInvalidToken
	}
	if time.Now().Unix() > int64(binary.BigEndian.Uint64(expRaw)) {
		return c, ErrInvalidToken
	}
	if err := c.UserID.Scan(parts[1]); err != nil {
		return c, ErrInvalidToken
	}
	if err := c.NodeID.Scan(parts[2]); err != nil {
		return c, ErrInvalidToken
	}
	if _, err := fmt.Sscanf(parts[3], "%d", &c.PermRank); err != nil {
		return c, ErrInvalidToken
	}
	return c, nil
}

// --- Locks (WOPI semantics) ---

// Lock acquires or refreshes a lock on nodeID. Conflict → ErrLockConflict.
func (s *Service) Lock(ctx context.Context, nodeID pgtype.UUID, token string) error {
	if token == "" {
		token = randomLock()
	}
	existing, err := s.q.GetLock(ctx, nodeID)
	if errors.Is(err, pgx.ErrNoRows) {
		return s.q.UpsertLock(ctx, store.UpsertLockParams{NodeID: nodeID, LockToken: token})
	}
	if err != nil {
		return err
	}
	if existing.LockToken != token {
		return ErrLockConflict
	}
	// Same token = refresh.
	return s.q.RefreshLock(ctx, store.RefreshLockParams{NodeID: nodeID, LockToken: token})
}

// Unlock releases the lock if the token matches.
func (s *Service) Unlock(ctx context.Context, nodeID pgtype.UUID, token string) error {
	existing, err := s.q.GetLock(ctx, nodeID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotLocked
	}
	if err != nil {
		return err
	}
	if existing.LockToken != token {
		return ErrLockConflict
	}
	return s.q.DeleteLock(ctx, store.DeleteLockParams{NodeID: nodeID, LockToken: token})
}

// CurrentLock returns the active lock token, or empty.
func (s *Service) CurrentLock(ctx context.Context, nodeID pgtype.UUID) (string, error) {
	l, err := s.q.GetLock(ctx, nodeID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return l.LockToken, err
}

// Sweep removes expired locks (called periodically by the server).
func (s *Service) Sweep(ctx context.Context) error {
	return s.q.SweepExpiredLocks(ctx)
}

func randomLock() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}
