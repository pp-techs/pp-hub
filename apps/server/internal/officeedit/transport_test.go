package officeedit

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func socketServer(t *testing.T, binary string) (string, *http.Client) {
	t.Helper()
	// macOS's Unix socket path bound is shorter than testing.T.TempDir paths.
	dir, err := os.MkdirTemp("/tmp", "officeedit-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	socket := filepath.Join(dir, "agent.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: Handler(binary), ReadHeaderTimeout: time.Second}
	done := make(chan struct{})
	go func() { defer close(done); _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close(); <-done })
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}}
	t.Cleanup(transport.CloseIdleConnections)
	return socket, &http.Client{Transport: transport, Timeout: 10 * time.Second}
}

func TestUnixSocketClientPerformsRealOfficeEdit(t *testing.T) {
	e := realRunner(t)
	source := fixture(t, e, "docx")
	socket, _ := socketServer(t, e.binary)
	client := NewClient(socket)
	schema, err := client.Do(context.Background(), Request{Action: "schema", Format: "docx"})
	if err != nil || !bytes.Contains(schema.Data, []byte(`"paragraph"`)) {
		t.Fatalf("runner schema unavailable: %v", err)
	}
	edited, err := client.Do(context.Background(), Request{Action: "edit", Format: "docx", Content: source, Operations: []Operation{{Op: "add", Parent: "/body", Type: "paragraph", Props: map[string]string{"text": "OverUnixSocket", "bold": "true"}}}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(packageText(t, edited.Content), "OverUnixSocket") {
		t.Fatal("socket edit did not persist requested document content")
	}
	view, err := client.Do(context.Background(), Request{Action: "read", Format: "docx", Content: edited.Content, Depth: 8})
	if err != nil || !bytes.Contains(view.Data, []byte("OverUnixSocket")) || len(view.Content) > 0 {
		t.Fatalf("socket read did not return structured edited document: %v", err)
	}
}

func TestUnixProtocolRejectsUnknownFieldsAndRawCommands(t *testing.T) {
	e := realRunner(t)
	_, client := socketServer(t, e.binary)
	for _, body := range []string{`{"action":"schema","format":"docx","command":"raw /etc/passwd"}`, `{"action":"schema","format":"docx"} {}`, `{"action":"schema","format":"docx","operations":[{"op":"raw-set","xml":"<x/>"}]}`} {
		response, err := client.Post("http://office-agent/process", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		data, _ := io.ReadAll(response.Body)
		response.Body.Close()
		if response.StatusCode != http.StatusBadRequest || bytes.Contains(data, []byte("/etc/passwd")) {
			t.Fatalf("invalid protocol accepted or echoed: %d %s", response.StatusCode, data)
		}
	}
	response, err := client.Get("http://office-agent/process")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("GET accepted: %d", response.StatusCode)
	}
}

func TestUnavailableRunnerAndBoundedProcessing(t *testing.T) {
	if _, err := NewClient("").Do(context.Background(), Request{Action: "schema", Format: "docx"}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("unconfigured runner not unavailable: %v", err)
	}
	socket, _ := socketServer(t, "/no-such-officecli-binary")
	if _, err := NewClient(socket).Do(context.Background(), Request{Action: "schema", Format: "docx"}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("missing executable not unavailable: %v", err)
	}
	e := realRunner(t)
	source := fixture(t, e, "docx")
	for range cap(e.slots) {
		e.slots <- struct{}{}
	}
	if _, err := e.Do(context.Background(), Request{Action: "read", Format: "docx", Content: source}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("saturated worker did not refuse immediately: %v", err)
	}
	for range cap(e.slots) {
		<-e.slots
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := e.Do(ctx, Request{Action: "read", Format: "docx", Content: source}); !errors.Is(err, ErrProcessing) {
		t.Fatalf("canceled processing accepted: %v", err)
	}
}
