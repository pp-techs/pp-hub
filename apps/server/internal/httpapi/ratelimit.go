package httpapi

import (
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/nhatbui/pp-hub/apps/server/internal/audit"
)

// A per-IP token bucket, for the handful of endpoints where an anonymous
// caller can do unbounded work or guess a secret.
//
// In-process on purpose. The alternative is Redis, which would be the first
// piece of infrastructure this deployment does not already need, for a
// control whose job is to turn "millions of guesses an hour" into "hundreds".
// The honest cost is that N server replicas allow N times the rate — stated
// in docs/deployment.md rather than hidden, because an operator who scales
// out should know the limit scaled with them.
type limiter struct {
	rate  float64 // tokens per second
	burst float64

	mu      sync.Mutex
	buckets map[string]*bucket
}

type bucket struct {
	tokens float64
	seen   time.Time
}

func newLimiter(perMinute, burst int) *limiter {
	return &limiter{
		rate:    float64(perMinute) / 60,
		burst:   float64(burst),
		buckets: make(map[string]*bucket),
	}
}

// allow spends one token for key, refilling first. It also evicts anything
// idle long enough to have refilled completely, which is what keeps the map
// from being a memory leak with an attacker-controlled key.
func (l *limiter) allow(key string, now time.Time) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()

	full := time.Duration(l.burst/l.rate) * time.Second
	for k, b := range l.buckets {
		if now.Sub(b.seen) > full {
			delete(l.buckets, k)
		}
	}

	b, ok := l.buckets[key]
	if !ok {
		b = &bucket{tokens: l.burst, seen: now}
		l.buckets[key] = b
	} else {
		b.tokens += now.Sub(b.seen).Seconds() * l.rate
		if b.tokens > l.burst {
			b.tokens = l.burst
		}
		b.seen = now
	}

	if b.tokens < 1 {
		return false, time.Duration((1-b.tokens)/l.rate*float64(time.Second)) + time.Second
	}
	b.tokens--
	return true, 0
}

// middleware answers 429 with Retry-After once the bucket is empty.
func (l *limiter) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ok, retry := l.allow(limiterKey(r), time.Now())
		if !ok {
			w.Header().Set("Retry-After", strconv.Itoa(int(retry.Seconds())))
			writeProblem(w, http.StatusTooManyRequests, "Too Many Requests",
				"slow down and try again shortly")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// limiterKey is the caller's address, and the reason it goes through
// audit.ClientIP rather than reading r.RemoteAddr is the whole difference
// between this limiter working and not.
//
// Keyed on IP rather than session, because every endpoint this guards is one
// an unauthenticated caller reaches — a per-session key would be a limit the
// attacker can reset by discarding the session. That reasoning only holds if
// the IP is not equally resettable, which it was while a forwarded header was
// believed from any source.
func limiterKey(r *http.Request) string {
	return audit.ClientIP(r)
}
