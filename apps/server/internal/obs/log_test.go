package obs

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/go-chi/chi/v5/middleware"
)

func decodeLine(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	var rec map[string]any
	if err := json.Unmarshal(buf.Bytes(), &rec); err != nil {
		t.Fatalf("not one JSON record: %q", buf.String())
	}
	buf.Reset()
	return rec
}

// The request id is the join key between a caller's X-Request-Id, the access
// log line and every other line logged while serving the request — including
// through a logger derived with With, which rebuilds the handler.
func TestContextHandlerAddsRequestID(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(NewHandler(&buf, slog.LevelInfo, "json"))
	ctx := context.WithValue(context.Background(), middleware.RequestIDKey, "req-1")

	log.InfoContext(ctx, "x")
	if got := decodeLine(t, &buf)["request_id"]; got != "req-1" {
		t.Errorf("request_id %v, want req-1", got)
	}

	log.InfoContext(context.Background(), "x")
	if rec := decodeLine(t, &buf); rec["request_id"] != nil {
		t.Errorf("request_id %v on a context without one", rec["request_id"])
	}

	log.With("k", "v").InfoContext(ctx, "x")
	if rec := decodeLine(t, &buf); rec["request_id"] != "req-1" || rec["k"] != "v" {
		t.Errorf("derived logger lost the request id: %v", rec)
	}
}

func TestParseLevel(t *testing.T) {
	if l, ok := parseLevel("DEBUG"); !ok || l != slog.LevelDebug {
		t.Errorf("DEBUG → %v %v", l, ok)
	}
	if l, ok := parseLevel(""); !ok || l != slog.LevelInfo {
		t.Errorf("empty → %v %v, want the info default", l, ok)
	}
	if _, ok := parseLevel("verbose"); ok {
		t.Error("an unknown level was accepted")
	}
}
