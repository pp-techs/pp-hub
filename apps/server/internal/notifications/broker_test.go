package notifications

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
)

func testUser(b byte) pgtype.UUID { return pgtype.UUID{Bytes: [16]byte{b}, Valid: true} }

// A stream that falls behind is dropped — its channel closes so the client
// reconnects and refetches — without holding up anybody else's; and one
// person's streams are capped.
func TestBrokerDropsOnlyTheStreamThatFellBehind(t *testing.T) {
	b := NewBroker()
	ann, bob := testUser(1), testUser(2)
	slow, err := b.Subscribe(ann)
	if err != nil {
		t.Fatal(err)
	}
	fast, _ := b.Subscribe(ann)
	other, _ := b.Subscribe(bob)

	for i := range streamBuffer + 1 {
		b.Publish(Item{UserID: ann, Excerpt: string(rune('a' + i%26))})
		<-fast.C // the fast reader keeps up
	}
	n := 0
	for range slow.C {
		n++
	}
	if n != streamBuffer {
		t.Fatalf("slow stream got %d before closing, want %d", n, streamBuffer)
	}
	b.Publish(Item{UserID: ann})
	if _, ok := <-fast.C; !ok {
		t.Fatal("the stream that kept up was closed")
	}
	select {
	case it := <-other.C:
		t.Fatalf("bob received ann's notification %+v", it)
	default:
	}
	slow.Close() // already dropped: a no-op, not a double close

	fast.Close()
	other.Close()
	var subs []*Subscription
	for range MaxStreamsPerUser {
		s, err := b.Subscribe(ann)
		if err != nil {
			t.Fatalf("stream %d: %v", len(subs)+1, err)
		}
		subs = append(subs, s)
	}
	if _, err := b.Subscribe(ann); !errors.Is(err, ErrTooManyStreams) {
		t.Fatalf("stream past the cap: %v", err)
	}
	subs[0].Close()
	if _, err := b.Subscribe(ann); err != nil {
		t.Fatalf("a closed stream must free its slot: %v", err)
	}

	// Shutdown ends every open stream, so the HTTP server's graceful
	// shutdown does not wait on them, and refuses new ones.
	b.Shutdown()
	for i, s := range subs[1:] {
		if _, ok := <-s.C; ok {
			t.Fatalf("stream %d still open after Shutdown", i+1)
		}
	}
	if _, err := b.Subscribe(bob); !errors.Is(err, ErrShuttingDown) {
		t.Fatalf("subscribe after Shutdown: %v", err)
	}
}
