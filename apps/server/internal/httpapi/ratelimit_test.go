package httpapi

import (
	"testing"
	"time"
)

// The burst is what a legitimate page load spends, and the rate is what
// refills it. Both have to hold, or the limit is either useless or it breaks
// the app.
func TestLimiterSpendsBurstThenRefuses(t *testing.T) {
	l := newLimiter(60, 5) // one per second, five in hand
	now := time.Now()

	for i := 0; i < 5; i++ {
		if ok, _ := l.allow("1.2.3.4", now); !ok {
			t.Fatalf("refused request %d of the burst", i+1)
		}
	}
	ok, retry := l.allow("1.2.3.4", now)
	if ok {
		t.Fatal("sixth request allowed past a burst of five")
	}
	if retry <= 0 {
		t.Error("a refusal must say how long to wait, or the client can only guess")
	}
}

func TestLimiterRefillsOverTime(t *testing.T) {
	l := newLimiter(60, 2)
	now := time.Now()

	l.allow("1.2.3.4", now)
	l.allow("1.2.3.4", now)
	if ok, _ := l.allow("1.2.3.4", now); ok {
		t.Fatal("burst not exhausted")
	}
	if ok, _ := l.allow("1.2.3.4", now.Add(2*time.Second)); !ok {
		t.Error("two seconds at one per second did not refill a single token")
	}
}

// Buckets are per key, so one noisy address must not spend anyone else's
// allowance — the whole point of keying on IP.
func TestLimiterIsolatesKeys(t *testing.T) {
	l := newLimiter(60, 1)
	now := time.Now()

	l.allow("1.2.3.4", now)
	if ok, _ := l.allow("1.2.3.4", now); ok {
		t.Fatal("first key not exhausted")
	}
	if ok, _ := l.allow("5.6.7.8", now); !ok {
		t.Error("a second address was refused because the first was noisy")
	}
}

// The key is attacker-controlled, so the map has to shed entries or it is a
// memory leak with a remote trigger.
func TestLimiterEvictsIdleBuckets(t *testing.T) {
	l := newLimiter(60, 2)
	now := time.Now()

	for i := 0; i < 100; i++ {
		l.allow(string(rune('a'+i%26))+time.Duration(i).String(), now)
	}
	if len(l.buckets) == 0 {
		t.Fatal("nothing recorded")
	}
	// Long enough for every bucket above to have refilled completely, which
	// is the point at which remembering it stops meaning anything.
	l.allow("someone-else", now.Add(time.Hour))
	if len(l.buckets) != 1 {
		t.Errorf("idle buckets retained: %d remain", len(l.buckets))
	}
}
