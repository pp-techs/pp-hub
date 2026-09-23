package audit

import (
	"testing"
	"time"
)

func TestReadCacheWindow(t *testing.T) {
	c := newReadCache(3)
	t0 := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	if !c.first("a", t0) {
		t.Fatal("first sighting must record")
	}
	if c.first("a", t0.Add(59*time.Minute)) {
		t.Fatal("repeat within the window must be absorbed")
	}
	if !c.first("a", t0.Add(61*time.Minute)) {
		t.Fatal("repeat after the window must record again")
	}
}

func TestReadCacheFullNeverDropsARead(t *testing.T) {
	c := newReadCache(3)
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	for _, k := range []string{"a", "b", "c"} {
		c.first(k, now)
	}
	if !c.first("d", now.Add(time.Minute)) {
		t.Fatal("a new key must record even when the cache is full of fresh keys")
	}
	if len(c.seen) > c.max {
		t.Fatalf("cache grew past its bound: %d > %d", len(c.seen), c.max)
	}
}
