package exthost

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/nhatbui/pp-hub/apps/server/extsdk"
)

const (
	// maxStreams bounds open streams across every extension; each holds a
	// goroutine and a connection.
	maxStreams = 2000
	// subBuffer is how many events a slow reader may fall behind by before it
	// is dropped; the client reconnects and refetches.
	subBuffer = 64
	// heartbeat keeps a quiet stream from looking dead to proxies.
	heartbeat = 25 * time.Second
)

// defaultRecheck is how often a stream asks whether its viewer may still
// watch (Config.StreamRecheck overrides it).
const defaultRecheck = time.Minute

type message struct {
	event string
	data  []byte
}

type sub struct {
	ch      chan message
	dropped bool
}

// broker is the process's topic fan-out. Streams are this process's, so it
// holds the same single-instance assumption as collaboration rooms.
type broker struct {
	mu     sync.Mutex
	topics map[string]map[*sub]struct{}
	n      int
	closed bool
}

func newBroker() *broker { return &broker{topics: map[string]map[*sub]struct{}{}} }

func (b *broker) subscribe(topic string) (*sub, func(), int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return nil, nil, http.StatusServiceUnavailable
	}
	if b.n >= maxStreams {
		return nil, nil, http.StatusTooManyRequests
	}
	s := &sub{ch: make(chan message, subBuffer)}
	if b.topics[topic] == nil {
		b.topics[topic] = map[*sub]struct{}{}
	}
	b.topics[topic][s] = struct{}{}
	b.n++
	return s, func() { b.unsubscribe(topic, s) }, 0
}

func (b *broker) unsubscribe(topic string, s *sub) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.topics[topic][s]; !ok {
		return
	}
	delete(b.topics[topic], s)
	if len(b.topics[topic]) == 0 {
		delete(b.topics, topic)
	}
	b.n--
	if !s.dropped {
		close(s.ch)
	}
}

func (b *broker) publish(topic, event string, data any) {
	raw, err := json.Marshal(data)
	if err != nil {
		slog.Error("encode ext event", "topic", topic, "event", event, "err", err)
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for s := range b.topics[topic] {
		select {
		case s.ch <- message{event: event, data: raw}:
		default:
			// Fell behind: end its stream rather than let it stall the rest.
			s.dropped = true
			close(s.ch)
			delete(b.topics[topic], s)
			b.n--
		}
	}
	if len(b.topics[topic]) == 0 {
		delete(b.topics, topic)
	}
}

func (b *broker) close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closed = true
	for topic, set := range b.topics {
		for s := range set {
			if !s.dropped {
				s.dropped = true
				close(s.ch)
			}
			b.n--
		}
		delete(b.topics, topic)
	}
}

type realtime struct {
	b       *broker
	active  func(*http.Request) bool
	recheck time.Duration
}

func (rt realtime) Publish(topic, event string, data any) { rt.b.publish(topic, event, data) }

func (rt realtime) Stream(w http.ResponseWriter, r *http.Request, topic string, stillAllowed func(ctx context.Context) bool) {
	s, cancel, status := rt.b.subscribe(topic)
	if s == nil {
		extsdk.WriteProblem(w, status, "too many open streams")
		return
	}
	defer cancel()

	rc := http.NewResponseController(w)
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	if _, err := fmt.Fprint(w, "retry: 3000\n\n"); err != nil {
		return
	}
	if err := rc.Flush(); err != nil {
		return
	}

	beat := time.NewTicker(heartbeat)
	defer beat.Stop()
	check := time.NewTicker(rt.recheck)
	defer check.Stop()
	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case m, ok := <-s.ch:
			if !ok {
				return
			}
			if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", m.event, m.data); err != nil {
				return
			}
		case <-beat.C:
			if _, err := fmt.Fprint(w, ": heartbeat\n\n"); err != nil {
				return
			}
		case <-check.C:
			if !stillAllowed(ctx) || (rt.active != nil && !rt.active(r)) {
				return
			}
			continue
		}
		if err := rc.Flush(); err != nil {
			return
		}
	}
}
