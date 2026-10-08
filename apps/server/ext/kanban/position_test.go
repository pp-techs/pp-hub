package kanban

import (
	"math/rand"
	"sort"
	"strings"
	"testing"
)

func TestBetweenOrdersAndNeverEndsInZero(t *testing.T) {
	// Insert at random places into a growing list and check the order the keys
	// give is the order the inserts asked for.
	rng := rand.New(rand.NewSource(1))
	keys := []string{Between("", "")}
	for i := 0; i < 2000; i++ {
		at := rng.Intn(len(keys) + 1) // insert before keys[at]
		var a, b string
		if at > 0 {
			a = keys[at-1]
		}
		if at < len(keys) {
			b = keys[at]
		}
		k := Between(a, b)
		if a != "" && !(a < k) || b != "" && !(k < b) {
			t.Fatalf("Between(%q, %q) = %q is not between", a, b, k)
		}
		if strings.HasSuffix(k, "0") {
			t.Fatalf("key %q ends in the zero digit", k)
		}
		keys = append(keys, "")
		copy(keys[at+1:], keys[at:])
		keys[at] = k
	}
	if !sort.StringsAreSorted(keys) {
		t.Fatal("keys are not in insertion order")
	}
}

func TestAfterGrowsLogarithmically(t *testing.T) {
	last := ""
	prev := ""
	for i := 0; i < 5000; i++ {
		last = After(last)
		if prev != "" && !(prev < last) {
			t.Fatalf("After(%q) = %q does not sort after", prev, last)
		}
		if strings.HasSuffix(last, "0") {
			t.Fatalf("key %q ends in the zero digit", last)
		}
		prev = last
	}
	if len(last) > 5 {
		t.Fatalf("5000 appends made a %d-letter key %q", len(last), last)
	}
}

func TestSpreadIsOrderedAndUsableByBetween(t *testing.T) {
	keys := Spread(1500)
	if !sort.StringsAreSorted(keys) {
		t.Fatal("Spread is not ordered")
	}
	for i, k := range keys {
		if strings.HasSuffix(k, "0") {
			t.Fatalf("key %q ends in the zero digit", k)
		}
		if i > 0 && keys[i-1] == k {
			t.Fatalf("duplicate %q", k)
		}
	}
	mid := Between(keys[10], keys[11])
	if !(keys[10] < mid && mid < keys[11]) {
		t.Fatalf("no room between neighbours: %q", mid)
	}
}
