package mail

import (
	"context"
	"net"
	"os"
	"strconv"
	"testing"
	"time"
)

func TestLiveSend(t *testing.T) {
	addr := os.Getenv("PP_TEST_SMTP_ADDR")
	if addr == "" {
		t.Skip("PP_TEST_SMTP_ADDR is not set")
	}

	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("invalid PP_TEST_SMTP_ADDR %q: %v", addr, err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("invalid port in PP_TEST_SMTP_ADDR %q: %v", addr, err)
	}

	settings := Settings{
		Host: host,
		Port: port,
		From: "pp-hub <test@pp-hub.local>",
		TLS:  "none",
	}

	sender := New(func() (Settings, error) {
		return settings, nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err = sender.Send(ctx, Message{
		To:      "recipient@example.com",
		Subject: "Test live email",
		Body:    "Hello from live test!",
	})
	if err != nil {
		t.Fatalf("failed to send live email: %v", err)
	}
}
