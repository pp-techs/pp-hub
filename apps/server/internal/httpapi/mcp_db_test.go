package httpapi

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/nhatbui/pp-hub/apps/server/internal/auth"
	"github.com/nhatbui/pp-hub/apps/server/internal/config"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
	"github.com/nhatbui/pp-hub/apps/server/internal/sysconfig"
	"github.com/nhatbui/pp-hub/apps/server/internal/users"
)

// Agent access (M41) end to end: an access token from an issuer with real
// keys, the MCP endpoint served by the production router, the official SDK
// as the client, and the tools reaching real grants, rooms and versions. Like
// the rest of this package's database tests it skips without
// PP_TEST_DATABASE_URL.

// fakeIssuer is an OIDC issuer that signs access tokens with its own key.
type fakeIssuer struct {
	srv *httptest.Server
	key *rsa.PrivateKey
}

func newFakeIssuer(t *testing.T) *fakeIssuer {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	iss := &fakeIssuer{key: key}
	iss.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/jwks" {
			_ = json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{
				{Key: &key.PublicKey, KeyID: "k1", Algorithm: "RS256", Use: "sig"},
			}})
			return
		}
		u := iss.srv.URL
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer": u, "authorization_endpoint": u + "/authorize", "token_endpoint": u + "/token",
			"jwks_uri": u + "/jwks", "end_session_endpoint": u + "/logout",
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	}))
	t.Cleanup(iss.srv.Close)
	return iss
}

// mint is an access token for sub, addressed to aud.
func (i *fakeIssuer) mint(t *testing.T, sub, aud string, ttl time.Duration) string {
	t.Helper()
	sig, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: i.key},
		(&jose.SignerOptions{}).WithType("at+jwt").WithHeader("kid", "k1"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	raw, err := jwt.Signed(sig).Claims(map[string]any{
		"iss": i.srv.URL, "sub": sub, "aud": aud, "client_id": "https://claude.ai/oauth/mcp-oauth-client-metadata",
		"iat": now.Unix(), "exp": now.Add(ttl).Unix(), "scope": "",
	}).Serialize()
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// mcpRig is a socialRig whose IdP signs access tokens, with agents enabled.
type mcpRig struct {
	socialRig
	iss *fakeIssuer
}

func newMCPRig(t *testing.T) mcpRig {
	t.Helper()
	rig := newSocialRig(t)
	ctx := context.Background()
	iss := newFakeIssuer(t)
	authSvc, err := auth.NewService(ctx, &config.Config{
		OIDCIssuer: iss.srv.URL, OIDCIssuerExt: iss.srv.URL, OIDCClientID: "pphub-test", ExternalURL: rig.url,
	}, rig.q, nil)
	if err != nil {
		t.Fatalf("auth service: %v", err)
	}
	// The router reads s.auth per request, so swapping it after Router()
	// is enough for both the cookie and the bearer path.
	rig.srv.auth = authSvc
	rig.srv.users = users.NewService(rig.q, rig.srv.blobs)
	rig.srv.sysconfig = sysconfig.NewService(rig.q, nil)
	rig.setSwitch(t, sysconfig.KeyMCPEnabled, true)
	return mcpRig{socialRig: rig, iss: iss}
}

func (rig socialRig) setSwitch(t *testing.T, key string, on bool) {
	t.Helper()
	ctx := context.Background()
	v, _ := json.Marshal(on)
	if _, err := rig.srv.sysconfig.UpdateMany(ctx, sysconfig.DefaultTenantID, map[string]json.RawMessage{key: v}, pgtype.UUID{}); err != nil {
		t.Fatalf("set %s: %v", key, err)
	}
	t.Cleanup(func() { _ = rig.srv.sysconfig.Delete(context.Background(), sysconfig.DefaultTenantID, key) })
}

// agent makes u an identity of the rig's issuer and connects an MCP client
// as them.
func (rig mcpRig) agent(t *testing.T, u store.User) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	if _, err := rig.q.AdoptUserIdentity(ctx, store.AdoptUserIdentityParams{
		ID: u.ID, OidcIss: rig.iss.srv.URL, OidcSub: u.OidcSub, Email: u.Email, Name: u.Name,
	}); err != nil {
		t.Fatalf("adopt identity: %v", err)
	}
	return rig.connect(t, rig.iss.mint(t, u.OidcSub, rig.url+mcpPath, time.Hour))
}

// connect opens an MCP client session presenting token.
func (rig mcpRig) connect(t *testing.T, token string) *mcp.ClientSession {
	t.Helper()
	client := mcp.NewClient(&mcp.Implementation{Name: "test-agent", Version: "1"}, nil)
	cs, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{
		Endpoint:             rig.url + mcpPath,
		HTTPClient:           &http.Client{Transport: bearer{token: token}},
		DisableStandaloneSSE: true,
		MaxRetries:           -1,
	}, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

// A personal token is minted by its person over the cookie API, acts as them
// on /mcp/, is never reachable through an agent, and stops working when it is
// revoked or tokens are turned off.
func TestMcpPersonalTokens(t *testing.T) {
	rig := newMCPRig(t)
	f := rig.collabRig.fixture(t, "mcppat", "# Doc\n")
	ctx := context.Background()

	if res := rig.do(t, f.owner, http.MethodPost, "/api/v1/users/me/agent-tokens",
		map[string]any{"name": "ci", "expires_in_days": 91}); res.code != http.StatusBadRequest {
		t.Fatalf("a 91-day token: %d %s", res.code, res.body)
	}
	res := rig.do(t, f.owner, http.MethodPost, "/api/v1/users/me/agent-tokens",
		map[string]any{"name": "ci", "expires_in_days": 7})
	var made agentTokenDTO
	if res.code != http.StatusCreated || json.Unmarshal(res.body, &made) != nil || !strings.HasPrefix(made.Token, "pph_") {
		t.Fatalf("create token: %d %s", res.code, res.body)
	}
	cs := rig.connect(t, made.Token)
	var me struct {
		ID string `json:"id"`
	}
	mustTool(t, cs, "whoami", nil, &me)
	if me.ID != uuidString(f.owner.ID) {
		t.Fatalf("token acts as %s, want the owner", me.ID)
	}
	mustTool(t, cs, "read_document", map[string]any{"node_id": uuidString(f.doc.ID)}, nil)

	// The dispatcher an agent drives does not mount the token routes, so a
	// tool can never mint the credential that would outlive its switches.
	outer := httptest.NewRequest(http.MethodPost, mcpPath, nil)
	got, err := agentAPI{router: rig.srv.agentRouter, outer: outer, sess: &auth.Session{User: f.owner}}.
		Do(ctx, http.MethodPost, "/users/me/agent-tokens", map[string]any{"name": "x", "expires_in_days": 1})
	if err != nil || (got.Status != http.StatusNotFound && got.Status != http.StatusMethodNotAllowed) {
		t.Fatalf("agent router minted a token: %d %v", got.Status, err)
	}

	rig.setSwitch(t, sysconfig.KeyMCPTokens, false)
	if _, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "whoami"}); err == nil {
		t.Fatal("token still worked with personal tokens turned off")
	}
	if res := rig.do(t, f.owner, http.MethodPost, "/api/v1/users/me/agent-tokens",
		map[string]any{"name": "ci2"}); res.code != http.StatusForbidden {
		t.Fatalf("minted a token while turned off: %d", res.code)
	}
	rig.setSwitch(t, sysconfig.KeyMCPTokens, true)

	if res := rig.do(t, f.owner, http.MethodDelete, "/api/v1/users/me/agent-tokens/"+made.ID, nil); res.code != http.StatusNoContent {
		t.Fatalf("revoke: %d %s", res.code, res.body)
	}
	if _, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "whoami"}); err == nil {
		t.Fatal("token still worked after it was revoked")
	}
}

type bearer struct{ token string }

func (b bearer) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+b.token)
	return http.DefaultTransport.RoundTrip(r)
}

// callTool runs a tool and answers its text, failing on a protocol error.
func callTool(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) (string, bool) {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String(), res.IsError
}

func mustTool(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any, v any) {
	t.Helper()
	text, isErr := callTool(t, cs, name, args)
	if isErr {
		t.Fatalf("%s failed: %s", name, text)
	}
	if v != nil {
		if err := json.Unmarshal([]byte(text), v); err != nil {
			t.Fatalf("%s: decode %q: %v", name, text, err)
		}
	}
}

// The endpoint takes a bearer token and nothing else: no token and a
// session cookie alone both get the 401 that starts sign-in, pointing at the
// metadata; a token for another audience is refused the same way.
func TestMcpIsBearerOnly(t *testing.T) {
	rig := newMCPRig(t)
	f := rig.collabRig.fixture(t, "mcpauth", "# Doc\n")

	post := func(hdr map[string]string) *http.Response {
		t.Helper()
		req, _ := http.NewRequest(http.MethodPost, rig.url+mcpPath,
			strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		for k, v := range hdr {
			req.Header.Set(k, v)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res
	}
	wantChallenge := func(what string, res *http.Response) {
		t.Helper()
		if res.StatusCode != http.StatusUnauthorized {
			t.Fatalf("%s: %d, want 401", what, res.StatusCode)
		}
		if h := res.Header.Get("WWW-Authenticate"); !strings.Contains(h, `resource_metadata="`+rig.url+mcpMetadataPath+`"`) {
			t.Fatalf("%s: WWW-Authenticate %q", what, h)
		}
	}
	wantChallenge("no credentials", post(nil))
	wantChallenge("session cookie", post(map[string]string{"Cookie": rig.cookie(f.owner), "Origin": rig.url}))
	wantChallenge("wrong audience", post(map[string]string{
		"Authorization": "Bearer " + rig.iss.mint(t, f.owner.OidcSub, "pphub-test", time.Hour),
	}))

	res, err := http.Get(rig.url + mcpMetadataPath)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var meta struct {
		Resource             string   `json:"resource"`
		AuthorizationServers []string `json:"authorization_servers"`
	}
	_ = json.NewDecoder(res.Body).Decode(&meta)
	if meta.Resource != rig.url+mcpPath || len(meta.AuthorizationServers) != 1 || meta.AuthorizationServers[0] != rig.iss.srv.URL {
		t.Fatalf("metadata: %+v", meta)
	}
}

// Both switches close the door at once: the instance's and the person's.
func TestMcpSwitches(t *testing.T) {
	rig := newMCPRig(t)
	f := rig.collabRig.fixture(t, "mcpswitch", "# Doc\n")
	cs := rig.agent(t, f.owner)
	mustTool(t, cs, "whoami", nil, nil)

	off := false
	if _, err := rig.srv.users.UpdatePreferences(context.Background(), f.owner.ID, users.PreferencesPatch{AgentsEnabled: &off}); err != nil {
		t.Fatal(err)
	}
	if _, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "whoami"}); err == nil {
		t.Fatal("agent still answered after its person turned agents off")
	}

	other := rig.agent(t, f.editor)
	rig.setSwitch(t, sysconfig.KeyMCPEnabled, false)
	if _, err := other.CallTool(context.Background(), &mcp.CallToolParams{Name: "whoami"}); err == nil {
		t.Fatal("agent still answered after the instance turned agents off")
	}
}

// An agent reads and writes with its person's access, a write is refused once
// the document moved on, and people in the room see an accepted write live.
func TestMcpDocumentsFollowAccessAndConcurrency(t *testing.T) {
	rig := newMCPRig(t)
	f := rig.collabRig.fixture(t, "mcpdoc", "# Plan\n\nBudget is final.\n")
	editor := rig.agent(t, f.editor)
	viewer := rig.agent(t, f.viewer)
	stranger := rig.agent(t, f.stranger)
	doc := uuidString(f.doc.ID)

	if _, isErr := callTool(t, stranger, "read_document", map[string]any{"node_id": doc}); !isErr {
		t.Fatal("a member without a grant read the document")
	}
	var read struct {
		ETag string `json:"etag"`
		Text string `json:"text"`
	}
	mustTool(t, viewer, "read_document", map[string]any{"node_id": doc}, &read)
	if text, isErr := callTool(t, viewer, "update_document", map[string]any{
		"node_id": doc, "markdown": "# Mine\n", "base_etag": read.ETag,
	}); !isErr || !strings.Contains(text, "403") {
		t.Fatalf("a viewer wrote the document: %s", text)
	}

	// Someone edits in the room after the agent read: the write is stale.
	room := rig.mustSession(t, f.owner, f.doc)
	peer := rig.mustDial(t, f.owner, room.Room)
	if !peer.edit(t, "# Plan\n\nBudget is final. Mostly.\n") {
		t.Fatal("room refused the owner's edit")
	}
	if text, isErr := callTool(t, editor, "update_document", map[string]any{
		"node_id": doc, "markdown": "# Plan\n\nBudget is draft.\n", "base_etag": read.ETag,
	}); !isErr || !strings.Contains(text, "changed since you read it") {
		t.Fatalf("a stale write went through: %s", text)
	}

	// Read again — the room is flushed first — and the write lands, live.
	mustTool(t, editor, "read_document", map[string]any{"node_id": doc}, &read)
	if !strings.Contains(read.Text, "Mostly.") {
		t.Fatalf("read missed the room's unsaved edit: %q", read.Text)
	}
	mustTool(t, editor, "update_document", map[string]any{
		"node_id": doc, "markdown": "# Plan\n\nBudget is draft.\n", "base_etag": read.ETag,
	}, nil)
	waitUntil(t, "the room to show the agent's write", func() bool {
		return strings.Contains(peer.markdown(), "Budget is draft.")
	})
	if v := rig.versions(t, f.doc)[0]; v.CreatedBy != f.editor.ID {
		t.Fatalf("new version authored by %v, want the editor", v.CreatedBy)
	}

	// The agent's reads are in the security log as the agent's.
	pool, err := pgxpool.New(context.Background(), os.Getenv("PP_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var n int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM security_events WHERE node_id = $1 AND user_id = $2 AND detail->>'via' = 'mcp'`,
		f.doc.ID, f.editor.ID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n == 0 {
		t.Fatal("no security event marked as the agent's")
	}
}

// A comment with a quote is a thread anchored at that text in the file.
func TestMcpCommentAnchorsInDocument(t *testing.T) {
	rig := newMCPRig(t)
	f := rig.collabRig.fixture(t, "mcpcomment", "# Plan\n\nBudget is final.\n")
	editor := rig.agent(t, f.editor)
	doc := uuidString(f.doc.ID)

	if text, isErr := callTool(t, editor, "add_comment", map[string]any{
		"node_id": doc, "quote": "nowhere", "body": "?",
	}); !isErr || !strings.Contains(text, "not found") {
		t.Fatalf("an unanchorable quote: %s", text)
	}
	if got := rig.threads(t, f.owner, f.doc); len(got) != 0 {
		t.Fatalf("a refused anchor left %d threads behind", len(got))
	}

	var thread struct {
		ID string `json:"id"`
	}
	mustTool(t, editor, "add_comment", map[string]any{"node_id": doc, "quote": "final", "body": "Really?"}, &thread)
	var read struct {
		Text string `json:"text"`
	}
	mustTool(t, editor, "read_document", map[string]any{"node_id": doc}, &read)
	if !strings.Contains(read.Text, `"threadId":"`+thread.ID+`"`) {
		t.Fatalf("thread %s not anchored in:\n%s", thread.ID, read.Text)
	}
}

// Public links are not offered until the instance allows agents to make them.
func TestMcpPublicLinksAreOptIn(t *testing.T) {
	rig := newMCPRig(t)
	f := rig.collabRig.fixture(t, "mcplink", "# Doc\n")
	has := func() bool {
		t.Helper()
		res, err := rig.agent(t, f.owner).ListTools(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, tool := range res.Tools {
			if tool.Name == "create_public_link" {
				return true
			}
		}
		return false
	}
	if has() {
		t.Fatal("create_public_link offered by default")
	}
	rig.setSwitch(t, sysconfig.KeyMCPPublicLinks, true)
	if !has() {
		t.Fatal("create_public_link missing once allowed")
	}
}

// M43: a document is a folder for its sub-pages to an agent too —
// create_document makes one under it, list_folder lists them and marks the
// document in its own listing — and read_document shows the frontmatter
// that holds the page's icon.
func TestMcpSubPages(t *testing.T) {
	rig := newMCPRig(t)
	f := rig.collabRig.fixture(t, "mcppages", "---\nicon: 🚀\nstatus: draft\n---\n# Plan\n")
	editor := rig.agent(t, f.editor)
	owner := rig.agent(t, f.owner)
	space, doc := uuidString(f.space.ID), uuidString(f.doc.ID)

	var read struct {
		Text string `json:"text"`
	}
	mustTool(t, editor, "read_document", map[string]any{"node_id": doc}, &read)
	if !strings.HasPrefix(read.Text, "---\n") || !strings.Contains(read.Text, "icon: 🚀") || !strings.Contains(read.Text, "status: draft") {
		t.Fatalf("frontmatter missing from read_document:\n%s", read.Text)
	}

	var made struct {
		ID       string `json:"id"`
		ParentID string `json:"parent_id"`
	}
	mustTool(t, editor, "create_document", map[string]any{
		"space_id": space, "name": "Budget", "folder_id": doc,
	}, &made)
	if made.ID == "" || made.ParentID != doc {
		t.Fatalf("create_document under the document = %+v, want a sub-page of %s", made, doc)
	}
	var listed []struct {
		ID             string `json:"id"`
		Name           string `json:"name"`
		Kind           string `json:"kind"`
		ParentID       string `json:"parent_id"`
		ChildPageCount int64  `json:"child_page_count"`
	}
	mustTool(t, editor, "list_folder", map[string]any{"space_id": space, "folder_id": doc}, &listed)
	if len(listed) != 1 || listed[0].ID != made.ID || listed[0].Name != "Budget.md" || listed[0].Kind != "file" {
		t.Fatalf("list_folder on the document = %+v, want the one sub-page", listed)
	}

	mustTool(t, owner, "list_folder", map[string]any{"space_id": space}, &listed)
	found := false
	for _, n := range listed {
		if n.ID == doc {
			found = true
			if n.ChildPageCount != 1 {
				t.Fatalf("document listed with child_page_count %d, want 1", n.ChildPageCount)
			}
		}
	}
	if !found {
		t.Fatalf("document missing from the top of the space: %+v", listed)
	}
}
