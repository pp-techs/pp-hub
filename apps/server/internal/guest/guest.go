// Package guest lets somebody with no account open what was shared with their
// email address: they ask for a one-time code, receive it at that address,
// and the code is the proof. The result is a `users` row of kind 'guest' that
// the HTTP edge confines to reading and commenting. See
// docs/architecture/guest-access.md.
package guest

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nhatbui/pp-hub/apps/server/internal/share"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

const (
	// CodeTTL is how long a code can be presented.
	CodeTTL = 10 * time.Minute
	// MaxAttempts is how many wrong codes burn the current one.
	MaxAttempts = 5
	// SendsPerHour caps the codes mailed to one address in an hour,
	// whoever asked: the form must not be a way to flood somebody's inbox.
	SendsPerHour = 3
)

// ErrInvalidCode answers every way a code can fail — unknown address, no code
// outstanding, expired, burnt, wrong — so the verify route cannot be used to
// tell them apart.
var ErrInvalidCode = errors.New("that code is not valid")

// Policy is what the instance configuration says about guests.
type Policy interface {
	GuestAccess() bool
	ExternalSharingEnabled() bool
}

// Mailer delivers a code.
type Mailer interface {
	GuestCode(ctx context.Context, to, code string, ttl time.Duration) error
}

type Service struct {
	q      *store.Queries
	policy Policy
	mailer Mailer
}

func New(q *store.Queries, policy Policy, mailer Mailer) *Service {
	return &Service{q: q, policy: policy, mailer: mailer}
}

// Enabled reports whether guests are admitted at all: the guest switch, and
// external sharing beneath it, since a guest only ever holds external grants.
func (s *Service) Enabled() bool {
	return s != nil && s.policy.GuestAccess() && s.policy.ExternalSharingEnabled()
}

// RequestCode mails a code to an address when there is a reason to: it is
// named by a view invitation that has not expired, or by a guest who still
// holds a grant, and no real account already uses it. In every other case —
// guests off, an unknown address, an address over its hourly allowance —
// nothing is sent and nothing is reported, so the caller can answer the same
// way each time and the form tells nobody who was shared with.
func (s *Service) RequestCode(ctx context.Context, rawEmail string) (sent bool) {
	email := share.NormalizeEmail(rawEmail)
	if email == "" || !s.Enabled() {
		return false
	}
	ok, err := s.eligible(ctx, email)
	if err != nil {
		slog.ErrorContext(ctx, "guest code eligibility", "module", "guest", "err", err)
		return
	}
	if !ok {
		return
	}
	code, err := newCode()
	if err != nil {
		slog.ErrorContext(ctx, "guest code", "module", "guest", "err", err)
		return
	}
	var send bool
	err = s.q.InTx(ctx, func(q *store.Queries) error {
		row, err := q.LockGuestOTP(ctx, email)
		switch {
		case err == nil:
			if time.Since(row.WindowStart.Time) < time.Hour && row.Sends >= SendsPerHour {
				return nil
			}
		case errors.Is(err, pgx.ErrNoRows):
		default:
			return err
		}
		send = true
		return q.PutGuestOTP(ctx, store.PutGuestOTPParams{
			EmailNorm: email,
			CodeHash:  hashCode(email, code),
			ExpiresAt: tstz(time.Now().Add(CodeTTL)),
		})
	})
	if err != nil {
		slog.ErrorContext(ctx, "guest code store", "module", "guest", "err", err)
		return
	}
	if !send {
		return
	}
	if err := s.mailer.GuestCode(ctx, email, code, CodeTTL); err != nil {
		slog.WarnContext(ctx, "guest code mail", "module", "guest", "err", err)
		return false
	}
	return true
}

// eligible is the reason to send a code.
func (s *Service) eligible(ctx context.Context, email string) (bool, error) {
	taken, err := s.q.AccountExistsForEmailNorm(ctx, email)
	if err != nil || taken {
		return false, err
	}
	pending, err := s.q.HasLivePendingShareFor(ctx, email)
	if err != nil || pending {
		return pending, err
	}
	return s.q.GuestHasLiveShare(ctx, email)
}

// Verify checks a code and, when it is right, returns the guest it opens,
// creating the guest on first use. A code works once; five wrong ones burn it.
func (s *Service) Verify(ctx context.Context, rawEmail, code string) (store.User, error) {
	email := share.NormalizeEmail(rawEmail)
	if email == "" || code == "" || !s.Enabled() {
		return store.User{}, ErrInvalidCode
	}
	var user store.User
	err := s.q.InTx(ctx, func(q *store.Queries) error {
		row, err := q.LockGuestOTP(ctx, email)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrInvalidCode
		}
		if err != nil {
			return err
		}
		if !row.ExpiresAt.Time.After(time.Now()) {
			return ErrInvalidCode
		}
		want := hashCode(email, code)
		if subtle.ConstantTimeCompare(row.CodeHash, want) != 1 {
			attempts, err := q.BumpGuestOTPAttempts(ctx, email)
			if err != nil {
				return err
			}
			if attempts >= MaxAttempts {
				if err := q.ExpireGuestOTP(ctx, email); err != nil {
					return err
				}
			}
			// The wrong guess has to be remembered, so this is not an
			// error that rolls the transaction back: the caller maps the
			// sentinel after commit.
			return nil
		}
		if err := q.ExpireGuestOTP(ctx, email); err != nil {
			return err
		}
		user, err = s.guestFor(ctx, q, email)
		return err
	})
	if err != nil {
		return store.User{}, err
	}
	if !user.ID.Valid {
		return store.User{}, ErrInvalidCode
	}
	return user, nil
}

// guestFor is the guest holding an address, made if there is none. An
// address a real account already uses makes none: it would collide with that
// account, or collect what was meant for its owner.
func (s *Service) guestFor(ctx context.Context, q *store.Queries, email string) (store.User, error) {
	u, err := q.GetGuestByEmailNorm(ctx, email)
	if err == nil {
		return u, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return store.User{}, err
	}
	taken, err := q.AccountExistsForEmailNorm(ctx, email)
	if err != nil {
		return store.User{}, err
	}
	if taken {
		return store.User{}, ErrInvalidCode
	}
	u, err = q.CreateGuestUser(ctx, email)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		// Two first verifications raced; the other one made the row.
		return q.GetGuestByEmailNorm(ctx, email)
	}
	return u, err
}

func newCode() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", n.Int64()), nil
}

// hashCode binds the code to the address, so one stolen row proves nothing
// about another address's code.
func hashCode(email, code string) []byte {
	sum := sha256.Sum256([]byte(email + ":" + code))
	return sum[:]
}

func tstz(t time.Time) pgtype.Timestamptz { return pgtype.Timestamptz{Time: t, Valid: true} }
