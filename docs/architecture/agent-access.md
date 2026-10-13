# Agent Access (MCP) — Design

> **Status: implemented, M41** ([milestones/M41.md](milestones/M41.md)). Chat-app
> agents act as the signed-in person through a remote MCP server in
> `apps/server`; Logto is the authorization server. Operating it is in
> [deployment.md](../deployment.md#agent-access-mcp); who may do what is in
> [permissions.md](permissions.md#agents-mcp).

## The decision

Agents reach pp-hub through a **remote MCP server inside `apps/server`**, not a
skill + CLI. The clients that matter are chat apps — Claude.ai, Claude Desktop,
ChatGPT — and, later, an agent embedded in pp-hub itself. None of them has a
shell, so a CLI is unreachable from all of them; a remote MCP server over
Streamable HTTP with OAuth is the one transport they share. A CLI for local
coding agents is not ruled out — it would be a second client of the same tool
registry — but nothing asked for it.

The agent acts **as the signed-in person** (delegated), never as a service
account: it sees and changes exactly what that person could through the web
app, resolved by the same handlers on every call.

**Logto is the authorization server; pp-hub is only a resource server.** pp-hub
does not mint, store or refresh agent tokens.

| Option                                          | Outcome                                                                                                                                                        |
| ----------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| pp-hub as its own OAuth server (DCR + CIMD)     | Rejected: a second token issuer and a new security surface, unnecessary once Logto speaks CIMD                                                                 |
| **Logto as authorization server, CIMD**         | **Chosen.** Logto 1.43.0 — the version `compose.logto.yml` pins — ships CIMD (`packages/core/src/oidc/cimd/`, dev guard removed in logto-io/logto#9475)        |
| Skill + CLI with personal access tokens         | Rejected as the transport for these clients: no shell in a chat app. Personal tokens were added later beside OAuth, for clients configured from a file (below) |
| Static API key header (Claude `static_headers`) | Rejected: one credential for a whole organization cannot act as an individual                                                                                  |

Logto has **no Dynamic Client Registration** endpoint, and none is needed:
Claude ([authentication](https://claude.com/docs/connectors/building/authentication))
and ChatGPT ([developer mode](https://developers.openai.com/api/docs/guides/developer-mode))
both identify themselves with a Client ID Metadata Document as a public client
(`token_endpoint_auth_method: none`), which Logto advertises once CIMD is on.

### What Logto holds

Two objects, converged by `deploy/logto/pp-logto-setup.sh` (steps 7 and 8)
alongside everything else it manages — configuration, not per-user data:

1. An **API resource** `pp-hub MCP` whose indicator is `${PP_EXTERNAL_URL}/mcp/`
   (trailing slash kept: MCP SDK clients append one when requesting the
   resource, and the audience is compared exactly). The script finds it by
   indicator, which Logto never lets change.
2. **CIMD enabled**: `PATCH /api/configs/cimd {"enabled": true}`. Logto refuses
   this with 422 unless SSRF protection is on with no allowlist —
   `SSRF_PROTECTION_DISABLED`, `OIDC_PROVIDER_SSRF_PROTECTION_DISABLED` and
   `SSRF_ALLOWED_ADDRESSES` all unset — because it fetches each client's document
   from a URL the client names. The script warns and continues; agents just
   cannot sign in until it is fixed.

**No Logto scopes, roles or default roles.** A CIMD client's resource scopes are
the intersection of the user's role scopes and a tenant-wide CIMD ceiling
(`filterResourceScopesForTheCimdClient`), so scope-based permissions would mean
a role on every existing and future user. pp-hub decides what an agent may do
itself; the token only proves who the person is and that it was issued for this
resource. This was the one assumption the design rested on, and it was
confirmed against a live Logto before shipping: Claude Code's CIMD document
signed in, consented, and received an `at+jwt` (ES384) with
`aud: "${PP_EXTERNAL_URL}/mcp/"`, `scope: ""` and `client_id` = the document's
URL; the refresh token rotated on use.

## Flow

```mermaid
sequenceDiagram
  participant C as Claude / ChatGPT
  participant S as pp-hub /mcp/
  participant L as Logto (CIMD)
  C->>S: POST /mcp/ (no token)
  S-->>C: 401 WWW-Authenticate: Bearer resource_metadata=".../.well-known/oauth-protected-resource/mcp"
  C->>S: GET /.well-known/oauth-protected-resource/mcp
  C->>L: authorize (client_id = CIMD URL, PKCE S256, resource = <pp-hub>/mcp/)
  L-->>C: JWT access token, aud = <pp-hub>/mcp/
  C->>S: tools/call + Authorization: Bearer
  S->>S: verify JWT → account → switches → tool → in-process API call
```

## Architecture

```mermaid
flowchart LR
  MCP["/mcp/ — mcpBearer + go-sdk Streamable HTTP (stateless)"] --> REG["internal/agenttools registry"]
  REG -->|agenttools.API| DISP["agentAPI (httpapi/agent.go)"]
  DISP --> TABLE["apiRoutes(r, agent=true)"]
  WEB["browser → /api/v1 (rate limit, sameOrigin, cookie)"] --> TABLE2["apiRoutes(r, agent=false)"]
  TABLE --> H["the same handlers"]
  TABLE2 --> H
```

- **`internal/agenttools`** — the registry: name, title, description, JSON
  Schema, `ReadOnly`/`Destructive`, and `Run(ctx, api, args)`. A tool is a few
  calls to pp-hub's REST API through the `agenttools.API` interface; it never
  imports a module. `Tools(Options)` builds the list per request, so a switch
  flipped in Admin › Settings changes the next `tools/list`.
- **`internal/httpapi/agent.go`** — `agentAPI` implements `agenttools.API` by
  serving each call **in process** through `apiRoutes`, the very table the
  network mounts under `/api/v1`, with the person's session where `caller(r)`
  finds it. A tool therefore runs the web client's handlers — capability
  checks, audit rows, notifications, collab locks — and no access rule exists a
  second time for agents. The inner request carries the outer one's address,
  user agent, request id and agent marker, and drops its chi routing state so
  the path is routed afresh. A panic in a handler is answered as a 500: the tool
  runs on the SDK's goroutine, outside the router's recoverer.
- **`internal/httpapi/mcp.go`** — `mcpRoutes` mounts `/mcp` and `/mcp/` (the
  SDK's `StreamableHTTPHandler`, stateless, JSON responses) and the RFC 9728
  metadata at `/.well-known/oauth-protected-resource` and
  `/.well-known/oauth-protected-resource/mcp`. A fresh `mcp.Server` is built per
  request, bound to its caller; nothing is held between requests.

## Authentication, re-resolved on every request

`mcpBearer`, in order:

1. `mcp_enabled` off → **404** for the endpoint and the metadata alike.
2. No `Authorization: Bearer` → **401** with
   `WWW-Authenticate: Bearer resource_metadata="…"` — Claude starts sign-in only
   on a 401. A token anywhere else (a query parameter) is not read.
3. A token starting `pph_` is a [personal access token](#personal-access-tokens):
   refused with 403 while `mcp_tokens` is off, otherwise looked up by hash
   (`auth.Service.AuthenticateAgentToken`); unknown, revoked or expired → 401.
   Anything else goes to `auth.Service.VerifyAccessToken`: go-oidc against
   Logto's JWKS — signature, `iss` equal to the external issuer, `aud`
   containing exactly `${PP_EXTERNAL_URL}/mcp/`, `exp`. Failure → 401 with
   `error="invalid_token"`. The audience check keeps every other token out:
   pp-hub's own ID tokens are addressed to its client id.
4. The account by `(oidc_iss, oidc_sub)`. None → **403** "sign in to pp-hub once
   with this account before connecting an agent"; the MCP path never creates
   one.
5. Suspended → 403. `users.settings.agents_enabled` false → 403.
6. A per-person token bucket (1200/min, burst 200), keyed on the user id rather
   than the address: every Claude user arrives from the same Anthropic range.
7. `audit.WithAgent(ctx, client_id)` and the session on the context.

An OAuth token is stateless until it expires (the API resource's TTL, 3600 s
by default). The switches in steps 1 and 5 and suspension are the immediate
kill switches; removing the connection in Logto or in the chat app is the
other half, and pp-hub has no "connected apps" list for OAuth clients. A
personal token is a row, so revoking it ends it at once.

### Personal access tokens

OAuth needs a browser, and some clients have none or are configured from a
file that should just work: Claude Code or Cursor through `.mcp.json`, a CI
job, a script. For them a person mints a token in Settings › Security › AI
agents and pastes it into an `Authorization: Bearer` header:

```json
{
  "mcpServers": {
    "pp-hub": {
      "type": "http",
      "url": "https://drive.example.com/mcp/",
      "headers": { "Authorization": "Bearer pph_…" }
    }
  }
}
```

- **Stored like a session** (`agent_tokens`, migration `0018`): only the
  SHA-256 of `pph_` + 256 random bits. The plaintext exists once, in the answer
  to `POST /api/v1/users/me/agent-tokens`. The prefix lets the middleware tell
  a token from a JWT without trying both, and lets a secret scanner find one in
  a leaked file.
- **Fixed lifetime**, 1–90 days (default 30) chosen at creation, never slid:
  the 90-day cap is a session's absolute cap. `last_used_at` is written at most
  every five minutes; the five-minute sweep in `cmd/server` drops expired rows.
- **Same everything else**: it acts as its creator with their access, through
  the same switches, suspension check and rate limit; its audit `client_id` is
  `pat:<token id>`. Creation and revocation are security events
  (`agent_token_created`, `agent_token_revoked`).
- **Managed by the person only.** `GET|POST /users/me/agent-tokens` and
  `DELETE /users/me/agent-tokens/{id}` are mounted on the network's `/api/v1`
  (cookie and same-origin) and **not** on the agent router — nor are the
  session routes — so no tool can mint the credential that would outlive the
  switches meant to stop it. `TestMcpPersonalTokens` asserts it.
- **A purpose.** Since M47 a token is minted for one surface: `purpose`
  `"mcp"` (the default) or `"dav"` on `POST /users/me/agent-tokens`
  (`agent_tokens.purpose`, migration `0025`); the list returns both with their
  purpose. The lookup filters on it, so a `dav` token presented here is 401 and
  an `mcp` token is refused at `/dav`. A `dav` token needs `webdav_enabled`
  rather than these switches, is minted in Settings › Security › Mount as a
  drive, and audits as `dav:<token id>` — see [webdav.md](webdav.md).
- **`mcp_tokens`** (`system_config`, default **on** — agent access as a whole
  is already the opt-in) turns minting and presenting tokens off together;
  existing tokens stay listed so they can be revoked.

### Separation from the cookie surface

`/mcp/` and `/.well-known/oauth-protected-resource*` live **outside** the
`/api/v1` group and **never read the session cookie**. Because the credential
is a bearer token, never ambient, they are exempt from `sameOrigin` without
reopening CSRF — the same reasoning as `/hooks/` and `/wopi/`.
`TestMcpIsBearerOnly` holds it: no credentials, a valid session cookie alone,
and a token for another audience all get the 401 challenge. The SDK's
DNS-rebinding guard is off for the same reason (it protects ambient access, and
a reverse proxy on the same host would trip it).

## Tools

| Group         | Tools                                                                                                                                                                                         | Notes                                                                                                                                                                                                                                            |
| ------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Read          | `whoami`, `list_spaces`, `list_folder`, `search`, `get_node`, `list_recent`, `list_starred`, `list_shared_with_me`, `list_versions`, `list_members`, `list_groups`, `list_document_templates` | The existing reach-filtered routes, answered as the web client gets them. `list_folder` takes a markdown document's id as `folder_id` and lists its sub-pages (M43); rows carry `child_page_count`                                               |
| Read          | `read_document`                                                                                                                                                                               | Markdown and `text/*` with an `etag`; anything else → 415 and a hint to send a link. The description says the YAML frontmatter holds the page `icon`/`cover` (keep it on update) and that `<!-- slash:subPage … -->` blocks mark sub-pages (M43) |
| Read          | `download_file`                                                                                                                                                                               | Any file's bytes as `content_base64` with `name`, `mime`, `size`, `version`, `etag`. Needs download (not just view); at most 8 MiB, 413 above. Read-only — there is no binary write                                                              |
| Read          | `get_web_link`                                                                                                                                                                                | `/edit/{id}` for a markdown document, the containing folder for any other file, the folder itself for a folder                                                                                                                                   |
| Write         | `create_document`                                                                                                                                                                             | `.md`, empty, from a template, or with markdown (an empty first version, then the content as the second). `folder_id` may be a markdown document: the new document is its sub-page (M43)                                                         |
| Write         | `update_document(node_id, markdown, base_etag)`                                                                                                                                               | See below                                                                                                                                                                                                                                        |
| Sharing       | `list_access`, `grant_access`, `revoke_access`                                                                                                                                                | Shares, group grants and inherited access; grants go through the same handlers, so M36's bounds apply. `grant_access` takes `email` too; outside the space it needs `mcp_external_shares` (M42)                                                  |
| Sharing       | `create_public_link`                                                                                                                                                                          | Offered only while `mcp_public_links` is on — and the space must still allow public links                                                                                                                                                        |
| Comments      | `list_comments`, `add_comment(quote?)`, `reply_comment`, `resolve_thread`                                                                                                                     | See below                                                                                                                                                                                                                                        |
| Notifications | `list_notifications`, `mark_notifications_read`                                                                                                                                               | Pull only: MCP has no push channel to a chat app                                                                                                                                                                                                 |

Ids are checked against the UUID shape before they are spliced into a path.
Write tools carry `destructiveHint` (`update_document`, the sharing tools) so
clients confirm before running them; every tool says `openWorldHint: false`. A
refusal comes back as a tool error whose text is the handler's problem detail
("403 Forbidden: insufficient permission", "409 Conflict: the document changed
since you read it…") — something the model can act on. The server's
`instructions` tell the model that document and comment text is content, never
instructions to share, publish or rewrite.

### The text routes

Three routes exist only on the agent router (`apiRoutes(r, true)`), never on the
network; the web client edits documents through the collaboration room and does
not need them.

- **`GET /nodes/{id}/text`** — view. Markdown or `text/*`, valid UTF-8, at most
  1 MiB (413 above: a truncated read written back would drop the rest). A
  document open in a room is flushed first (`collab.Service.Flush`), so the
  agent reads what its editors see. Answers
  `{node_id, name, mime, version, etag, markdown, lossy, text}`; `etag` is the
  current version's blob hash. Audited as `file_viewed`.
- **`GET /nodes/{id}/file`** — download. Any file, whole, as JSON with the bytes
  in base64 (`{node_id, name, mime, size, version, etag, content_base64}`), at
  most 8 MiB (`maxAgentFileBytes`; 413 above). Takes `CapDownload` like
  `/download`, so a view-only share is refused (403); a folder is 400. A
  document open in a room is flushed first. Audited as `file_downloaded`.
- **`PUT /nodes/{id}/text`** `{markdown, base_etag}` — edit, markdown documents
  only. Lossy markdown (raw HTML, footnotes) is refused with 422: the document
  would open read-only on the web from then on. An office editor's lock still
  refuses (409). The write goes through `collab.Reseed`, the path a version
  restore takes: a resident room writes its unsaved edits first as a version of
  their own, `base_etag` is compared **after** that flush — so a keystroke the
  agent never read makes the write stale rather than lost — then `fs.PutFile`
  writes the new version as the caller and the room's peers receive the new
  content at once. Identical content writes nothing. Recorded in the activity
  feed as `uploaded`.

This is the one route besides the upload routes that turns a request body into
file content, and it is safe for the reasons the upload routes are: it is
bounded, quota is enforced by `fs.PutFile`, and locks and rooms are honoured —
and it is not reachable from the network at all.

### Comment anchors

A thread's anchor is a pair of markers in the markdown
([block-documents.md](block-documents.md#comment-anchors)), so anchoring is a
document write. `add_comment` with a `quote`:

1. reads the document and checks the anchor first (`agenttools.AnchorComment`
   with a probe id), so a quote that cannot be anchored creates nothing;
2. opens the thread (`POST /nodes/{id}/comment-threads`);
3. writes the anchored markdown under the etag it read; if that fails — the
   document moved on — the thread's only comment is deleted, which takes the
   thread with it.

`AnchorComment` works on the parsed tree, not the source: the quote is matched
against the text as it reads (no `**` or `#`), inside one textblock, exactly
once; the comment mark is added to the covering text nodes (never to code); and
`mdoc.Render` — the serializer the room's write-back uses — decides where the
markers go, `back` included. The result is re-parsed to confirm the thread
survived. The cost is the one every room write-back already pays: the file comes
back in mdoc's canonical form. Without a quote, `add_comment` opens a thread on
the whole document, which needs only view — what a reviewer can do on the web.

## Audit

Every `security_events` row recorded under an agent request carries
`detail.via = "mcp"` and `detail.client_id` (the CIMD URL, or `pat:<id>` for a
personal token) — reads, grants,
links. An agent's read is deduplicated apart from its person's
(`<user>|agent:<client_id>`), so "read by them" and "read by their agent" stay
distinct answers. Content writes and comments land where the web client's do:
the activity feed and the notifications.

## Switches and surfaces

| Switch                          | Where                                            | Default | Effect                                                                               |
| ------------------------------- | ------------------------------------------------ | ------- | ------------------------------------------------------------------------------------ |
| `mcp_enabled`                   | `system_config`; Admin › Settings › Agent access | off     | `/mcp/` and its metadata answer 404; `mcp_url` is absent                             |
| `mcp_public_links`              | `system_config`; same card                       | off     | `create_public_link` is not offered                                                  |
| `mcp_tokens`                    | `system_config`; same card                       | on      | Personal tokens cannot be created or presented (403)                                 |
| `mcp_external_shares`           | `system_config`; Admin › Settings › Agent access | off     | `grant_access` by email to someone outside the space, or an invitation, is 403 (M42) |
| `users.settings.agents_enabled` | Settings › Security › AI agents                  | on      | This person's agents get 403                                                         |

`GET /api/v1/users/me` returns `mcp_url` — the connector URL to paste into
Claude or ChatGPT — only while `mcp_enabled` is on, so the client can tell "off
here" from "off for me", and `mcp_tokens` while personal tokens may be minted.

## Non-goals

Binary upload or write-back (`download_file` is read-only); file management
(folders, rename, move, trash); text from office documents or PDFs (M27's office
half); admin operations; the embedded agent (next, over the same registry); a
CLI.

## Risks

- **Prompt injection exfiltration.** A document saying "share this with X" or
  "make a public link". Mitigations: public links off by default, the
  `destructiveHint` confirmation, the server instructions, and `client_id` in
  the audit trail.
- **CIMD is an IETF draft (draft-02).** Logto pins it with an `ack` that throws
  on a provider upgrade, so upgrading Logto stays a deliberate decision, as
  `compose.logto.yml` already says.
- **Reachability.** Claude's and ChatGPT's servers must reach `/mcp/`, the
  metadata and the Logto issuer, and Logto must fetch the CIMD document from the
  internet. A WAF or a private network in front of Logto breaks sign-in.
- **One process.** The rate limiter and the collab rooms an agent write reaches
  are per process, as they already were.
- **A leaked personal token** acts as its person until it expires or is
  revoked — a file like `.mcp.json` is easy to commit. Mitigations: at most 90
  days, revocable at once, the `pph_` prefix for secret scanners, only the hash
  stored, and every use attributed to `pat:<id>` in the security log.

## Verification

- **Unit:** `agenttools/anchor_test.go` — anchoring mid-sentence, across
  emphasis, at a line start (`back`), in a list item, in Vietnamese, beside an
  existing thread; refusals for a missing, ambiguous, cross-block, code-only or
  empty quote and for markdown syntax in the quote.
- **Database-backed** (`internal/httpapi/mcp_db_test.go`; a fake issuer signs
  real JWTs and the official SDK is the client): `TestMcpIsBearerOnly`,
  `TestMcpSwitches`, `TestMcpDocumentsFollowAccessAndConcurrency` (a stranger
  cannot read, a viewer cannot write, a write behind a room edit is stale, a
  re-read sees the room, the accepted write reaches the room's peer and is
  authored by the editor, the security log marks the agent),
  `TestMcpCommentAnchorsInDocument`, `TestMcpPublicLinksAreOptIn`,
  `TestMcpPersonalTokens` (minted over the cookie API, acts as its person, a
  91-day token refused, the agent router cannot mint one, turned off and
  revoked tokens stop working).
- **Live smoke** (M41): Logto 1.44 dev tenant with the resource and CIMD
  enabled, Claude Code's CIMD document through sign-in and consent, then the
  token against a running server — discovery, the 401 challenge, every tool
  class, a stale write, a lossy write, an ID token refused, and both settings
  surfaces in the browser.
