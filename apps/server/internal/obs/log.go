// Package obs is process-wide observability: the slog handler every binary
// installs and the Prometheus metrics the API server exposes.
package obs

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"

	"github.com/go-chi/chi/v5/middleware"
)

// InitLogging installs the process-wide slog handler from PP_LOG_LEVEL and
// PP_LOG_FORMAT. It reads the environment directly because it must run before
// config.Load, which already logs. An unusable value falls back to the default
// with a warning rather than refusing startup: a log knob typo must not take
// file serving down.
func InitLogging() {
	rawLevel, rawFormat := os.Getenv("PP_LOG_LEVEL"), os.Getenv("PP_LOG_FORMAT")
	level, levelOK := parseLevel(rawLevel)
	format, formatOK := parseFormat(rawFormat)
	slog.SetDefault(slog.New(NewHandler(os.Stderr, level, format)))
	if !levelOK {
		slog.Warn("ignoring unusable value", "key", "PP_LOG_LEVEL", "value", rawLevel, "using", "info")
	}
	if !formatOK {
		slog.Warn("ignoring unusable value", "key", "PP_LOG_FORMAT", "value", rawFormat, "using", "text")
	}
}

// NewHandler builds the handler InitLogging installs: text or JSON at the
// given level, with the chi request id of the record's context attached.
func NewHandler(w io.Writer, level slog.Level, format string) slog.Handler {
	opts := &slog.HandlerOptions{Level: level}
	if format == "json" {
		return contextHandler{slog.NewJSONHandler(w, opts)}
	}
	return contextHandler{slog.NewTextHandler(w, opts)}
}

// parseLevel maps PP_LOG_LEVEL to a slog level; "" is the default, info.
func parseLevel(s string) (slog.Level, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug, true
	case "", "info":
		return slog.LevelInfo, true
	case "warn":
		return slog.LevelWarn, true
	case "error":
		return slog.LevelError, true
	}
	return slog.LevelInfo, false
}

// parseFormat maps PP_LOG_FORMAT to "text" or "json"; "" is the default, text.
func parseFormat(s string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "text":
		return "text", true
	case "json":
		return "json", true
	}
	return "text", false
}

// contextHandler adds the chi request id carried by the record's context, so
// every *Context log call made while serving a request can be joined to its
// access log line, its X-Request-Id header and its security_events rows.
type contextHandler struct{ slog.Handler }

func (h contextHandler) Handle(ctx context.Context, r slog.Record) error {
	if ctx != nil {
		if id := middleware.GetReqID(ctx); id != "" {
			r.AddAttrs(slog.String("request_id", id))
		}
	}
	return h.Handler.Handle(ctx, r)
}

func (h contextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return contextHandler{h.Handler.WithAttrs(attrs)}
}

func (h contextHandler) WithGroup(name string) slog.Handler {
	return contextHandler{h.Handler.WithGroup(name)}
}
