// Package agenttools is what an AI agent may do in pp-hub (M41): a fixed
// registry of tools, each a few calls to pp-hub's own REST API made as the
// agent's user. The MCP endpoint (internal/httpapi/mcp.go) serves them to
// Claude and ChatGPT; an embedded agent would call the same registry.
//
// Tools never touch the store or a module directly. They go through API,
// which the HTTP edge implements by dispatching into the very handlers the
// web client reaches — so an agent is authorized, audited and notified about
// exactly as its user would be, and no access rule exists twice. See
// docs/architecture/agent-access.md.
package agenttools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
)

// API is pp-hub's REST API under /api/v1, called as the agent's user. path
// is relative to /api/v1 and may carry a query; body, when non-nil, is sent
// as JSON.
type API interface {
	Do(ctx context.Context, method, path string, body any) (Response, error)
}

// Response is the API's answer.
type Response struct {
	Status int
	Body   []byte
}

// Error is an answer outside 2xx, with the problem+json detail the handler
// wrote — the text an agent needs to correct itself ("insufficient
// permission", "the document changed since you read it").
type Error struct {
	Status int
	Detail string
}

func (e *Error) Error() string {
	if e.Detail == "" {
		return fmt.Sprintf("%d %s", e.Status, http.StatusText(e.Status))
	}
	return fmt.Sprintf("%d %s: %s", e.Status, http.StatusText(e.Status), e.Detail)
}

// Tool is one capability offered to an agent.
type Tool struct {
	Name        string
	Title       string
	Description string
	// Schema is the JSON Schema of the arguments (an object).
	Schema json.RawMessage
	// ReadOnly marks a tool that changes nothing; Destructive one whose
	// change a client should confirm with its user first (MCP's
	// destructiveHint). Additive writes are neither.
	ReadOnly    bool
	Destructive bool
	Run         func(ctx context.Context, api API, args json.RawMessage) (any, error)
}

// Options are the per-request facts that change the registry.
type Options struct {
	// WebURL is the deployment's public origin (PP_EXTERNAL_URL), for the
	// links a tool hands back to the person.
	WebURL string
	// PublicLinks offers create_public_link (sysconfig mcp_public_links).
	PublicLinks bool
	// Publishing offers publish_artifact and unpublish_artifact: the
	// instance offers publishing (artifacts_enabled) and
	// mcp_public_links is on — a publication is a public link with scripts.
	Publishing bool
}

// ErrInvalidArguments is an argument a tool refuses before calling anything:
// the agent's mistake, told back to it, not a server failure.
var ErrInvalidArguments = errors.New("invalid arguments")

func badArgs(format string, a ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidArguments, fmt.Sprintf(format, a...))
}

// decode reads a tool's arguments; an absent body is an empty object.
func decode(args json.RawMessage, v any) error {
	if len(args) == 0 || string(args) == "null" {
		return nil
	}
	if err := json.Unmarshal(args, v); err != nil {
		return badArgs("%v", err)
	}
	return nil
}

// uuidRe is the shape of every id the API takes. Ids are spliced into paths,
// so anything else is refused here rather than escaped and hoped about.
var uuidRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// id checks a required id argument.
func id(name, v string) (string, error) {
	if !uuidRe.MatchString(v) {
		return "", badArgs("%s must be an id (a UUID) as returned by another tool", name)
	}
	return v, nil
}

// optionalID checks an id argument that may be empty.
func optionalID(name, v string) (string, error) {
	if v == "" {
		return "", nil
	}
	return id(name, v)
}

// call runs one request and answers its JSON body, or an *Error.
func call(ctx context.Context, api API, method, path string, body any) (json.RawMessage, error) {
	res, err := api.Do(ctx, method, path, body)
	if err != nil {
		return nil, err
	}
	if res.Status < 200 || res.Status > 299 {
		var p struct {
			Detail string `json:"detail"`
			Title  string `json:"title"`
		}
		_ = json.Unmarshal(res.Body, &p)
		if p.Detail == "" {
			p.Detail = p.Title
		}
		return nil, &Error{Status: res.Status, Detail: p.Detail}
	}
	if len(res.Body) == 0 {
		return json.RawMessage(`{"status":"ok"}`), nil
	}
	return res.Body, nil
}

// into runs a request and decodes its JSON answer into v.
func into(ctx context.Context, api API, method, path string, body, v any) error {
	raw, err := call(ctx, api, method, path, body)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, v); err != nil {
		return fmt.Errorf("decode %s %s: %w", method, path, err)
	}
	return nil
}
