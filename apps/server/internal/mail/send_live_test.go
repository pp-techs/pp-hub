package mail

import (
	"net"
	"os"
	"strconv"
	"testing"
)

// TestSendLive drives Send against a real SMTP catcher (mailhog, mailpit).
//
// The mocked unit tests would only prove this package's own bytes are
// well-formed; this proves an actual SMTP server accepts them. It needs a
// local catcher with no TLS and no auth:
//
//	docker run --rm -d -p 1025:1025 -p 8025:8025 mailhog/mailhog
//	PP_TEST_SMTP_ADDR=127.0.0.1:1025 go test ./internal/mail/ -run Live -v
func TestSendLive(t *testing.T) {
	addr := os.Getenv("PP_TEST_SMTP_ADDR")
	if addr == "" {
		t.Skip("PP_TEST_SMTP_ADDR not set; skipping live SMTP send")
	}
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("PP_TEST_SMTP_ADDR %q: %v", addr, err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("PP_TEST_SMTP_ADDR port %q: %v", portStr, err)
	}

	sender := New(Settings{
		Host: host,
		Port: port,
		From: "pp-hub@example.com",
		TLS:  "none",
	})
	if err := sender.Send(t.Context(), "invitee@example.com",
		"pp-hub live SMTP test", "This message proves Send reaches a real catcher.\n"); err != nil {
		t.Fatalf("send: %v", err)
	}
}
