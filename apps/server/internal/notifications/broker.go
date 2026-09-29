package notifications

import (
	"errors"
	"sync"

	"github.com/jackc/pgx/v5/pgtype"
)

// MaxStreamsPerUser bounds the open streams one person may hold — a tab each,
// with room to spare — so a script cannot pin unbounded memory in the broker.
const MaxStreamsPerUser = 16

// streamBuffer is how many notifications a stream may fall behind by before it
// is dropped. A reader that slow is gone or stuck; closing its channel ends
// the response, and the client's reconnect refetches the list anyway.
const streamBuffer = 32

// ErrTooManyStreams refuses a stream past MaxStreamsPerUser.
var ErrTooManyStreams = errors.New("too many open notification streams")

// ErrShuttingDown refuses a stream once the broker has been shut down.
var ErrShuttingDown = errors.New("server is shutting down")

// Broker fans notifications out to the recipients' open streams. It is
// in-process: every stream and every writer of notifications live in the one
// server instance (the same single-instance assumption as the collaboration
// rooms and the rate limiter). A notification written while its recipient has
// no stream open is simply in the list next time they look.
type Broker struct {
	mu   sync.Mutex
	subs map[pgtype.UUID]map[*Subscription]struct{}
	// down is set by Shutdown; no stream opens after it.
	down bool
}

// NewBroker returns an empty broker.
func NewBroker() *Broker {
	return &Broker{subs: map[pgtype.UUID]map[*Subscription]struct{}{}}
}

// Subscription is one open stream. C is closed when the subscription ends —
// by Close, or by the broker dropping a reader that fell behind.
type Subscription struct {
	C      <-chan Item
	ch     chan Item
	b      *Broker
	user   pgtype.UUID
	closed bool // guarded by b.mu
}

// Subscribe opens a stream for user.
func (b *Broker) Subscribe(user pgtype.UUID) (*Subscription, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.down {
		return nil, ErrShuttingDown
	}
	set := b.subs[user]
	if len(set) >= MaxStreamsPerUser {
		return nil, ErrTooManyStreams
	}
	if set == nil {
		set = map[*Subscription]struct{}{}
		b.subs[user] = set
	}
	ch := make(chan Item, streamBuffer)
	sub := &Subscription{C: ch, ch: ch, b: b, user: user}
	set[sub] = struct{}{}
	return sub, nil
}

// Close ends the subscription. Safe to call more than once.
func (s *Subscription) Close() {
	s.b.mu.Lock()
	defer s.b.mu.Unlock()
	s.b.dropLocked(s)
}

func (b *Broker) dropLocked(s *Subscription) {
	if s.closed {
		return
	}
	s.closed = true
	close(s.ch)
	set := b.subs[s.user]
	delete(set, s)
	if len(set) == 0 {
		delete(b.subs, s.user)
	}
}

// Shutdown closes every open stream and refuses new ones. A stream is a
// request that never finishes on its own, so http.Server.Shutdown would wait
// its whole grace period for them; cmd/server calls this from
// RegisterOnShutdown, and each handler returns as its channel closes.
func (b *Broker) Shutdown() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.down = true
	for _, set := range b.subs {
		for sub := range set {
			b.dropLocked(sub)
		}
	}
}

// Publish hands it to every open stream of its recipient without blocking.
func (b *Broker) Publish(it Item) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for sub := range b.subs[it.UserID] {
		select {
		case sub.ch <- it:
		default:
			b.dropLocked(sub)
		}
	}
}
