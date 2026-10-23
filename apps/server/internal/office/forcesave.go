package office

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// A Document Server keeps what people type in memory and hands it to the
// WOPI host only when the last editor leaves. Anything that ends the session
// by other means — a redeploy that stops the host first, a crash, an OOM
// kill — loses everything typed since the file was opened, with nothing in
// the version history to say it ever existed.
//
// Forcesaver narrows that window. Every interval it asks the Document Server,
// through its command service, to write each open document back now. The
// server answers with an ordinary PutFile, so the result is a normal version.
// https://api.onlyoffice.com/docs/docs-api/additional-api/command-service/forcesave
//
// The Document Server's own `autoAssembly` setting looks like the same thing
// and is not: with it enabled, 10.0.0.1 never sent a PutFile for a document
// that stayed open.

// Command service error codes that are not failures.
const (
	// errUnknownKey: no open session has this key. The lock outlived the
	// session (the editor closed and its UNLOCK has not landed, or the
	// Document Server restarted). Nothing to save.
	errUnknownKey = 1
	// errNoChanges: nothing was edited since the last save.
	errNoChanges = 4
)

// Forcesaver periodically forces open OnlyOffice documents to be saved.
type Forcesaver struct {
	// URL is the Document Server as reachable from this process.
	URL string
	// Secret is the Document Server's JWT secret. Empty means it runs
	// unsigned and the request carries no token.
	Secret []byte
	// Keys lists the editing-session keys to save — the WOPI lock ids.
	Keys func(ctx context.Context) ([]string, error)
	// Client is used for the command requests; nil means a client with a
	// short timeout.
	Client *http.Client
}

// commandTimeout bounds one command request. The Document Server answers as
// soon as the save is queued, not when it completes.
const commandTimeout = 15 * time.Second

type commandRequest struct {
	C   string `json:"c"`
	Key string `json:"key"`
	// Token is the request above, signed, omitted when the Document Server
	// runs unsigned (same reasoning as the conversion request).
	Token string `json:"token,omitempty"`
}

type commandResponse struct {
	Error int `json:"error"`
}

// Run saves every interval until ctx ends. A failed round is logged and the
// next one still happens.
func (f *Forcesaver) Run(ctx context.Context, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			saved, err := f.Once(ctx)
			if err != nil {
				slog.WarnContext(ctx, "office forcesave", "err", err)
			} else if saved > 0 {
				slog.DebugContext(ctx, "office forcesave", "requested", saved)
			}
		}
	}
}

// Once asks the Document Server to save every open document and returns how
// many it accepted. It keeps going past a failing document and reports the
// first error after trying all of them, so one bad key cannot hold up the
// rest.
func (f *Forcesaver) Once(ctx context.Context) (int, error) {
	keys, err := f.Keys(ctx)
	if err != nil {
		return 0, fmt.Errorf("list editor locks: %w", err)
	}
	saved := 0
	var first error
	for _, key := range keys {
		ok, err := f.forcesave(ctx, key)
		if err != nil {
			if first == nil {
				first = err
			}
			continue
		}
		if ok {
			saved++
		}
	}
	return saved, first
}

// forcesave sends one command. It reports whether a save was queued.
func (f *Forcesaver) forcesave(ctx context.Context, key string) (bool, error) {
	req := commandRequest{C: "forcesave", Key: key}
	var bearer string
	if len(f.Secret) > 0 {
		signed, err := SignJWT(f.Secret, req)
		if err != nil {
			return false, fmt.Errorf("sign forcesave: %w", err)
		}
		req.Token = signed
		bearer, err = SignJWT(f.Secret, map[string]any{"payload": req})
		if err != nil {
			return false, fmt.Errorf("sign forcesave header: %w", err)
		}
	}
	body, err := json.Marshal(req)
	if err != nil {
		return false, err
	}

	ctx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()
	hr, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(f.URL, "/")+"/command", bytes.NewReader(body))
	if err != nil {
		return false, err
	}
	hr.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		hr.Header.Set("Authorization", "Bearer "+bearer)
	}
	client := f.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(hr)
	if err != nil {
		return false, fmt.Errorf("forcesave %s: %w", key, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("forcesave %s: command service returned %s", key, resp.Status)
	}
	var out commandResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(&out); err != nil {
		return false, fmt.Errorf("forcesave %s: decode response: %w", key, err)
	}
	switch out.Error {
	case 0:
		return true, nil
	case errUnknownKey, errNoChanges:
		return false, nil
	default:
		return false, fmt.Errorf("forcesave %s: command service error %d", key, out.Error)
	}
}
