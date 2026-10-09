# Delivery Milestones

The delivery history and the open roadmap. Each shipped milestone ended in a
working end-to-end state inside the compose stack; its file keeps the decision
that shaped it and the artifacts that carry it, not the forensics.

Numbering is dependency order, not chronology — M25–M26 and M29–M40 landed after
M33–M35. **M12–M14 never existed** (`M12` was only the header of the retired
`0012_personal_folders.sql`; M13 and M14 appear nowhere). Everything shipped
through M15 is consolidated into `0001_init.sql`, and migration numbering
restarted at `0002` with M20's sessions table: today only `0002`–`0019` exist as
files, so M7/M9/M10/M11 below name the tables they added rather than the
migrations that carried them. Work that never got a number is recorded inside
the milestone that owns its area and says so there, so nothing has to be looked
for twice.

Each shipped milestone has its own file under [`milestones/`](milestones/) with the detail — routes, defaults, tests — and this page is the index and the roadmap. To record a new one, add `milestones/M<n>.md` and a row below.

| #   | Milestone                                              | Status     | Detail                      |
| --- | ------------------------------------------------------ | ---------- | --------------------------- |
| M1  | Skeleton                                               | ✅         | [detail](milestones/M1.md)  |
| M2  | Drive core                                             | ✅         | [detail](milestones/M2.md)  |
| M3  | Sharing                                                | ✅         | [detail](milestones/M3.md)  |
| M4  | Office                                                 | ✅         | [detail](milestones/M4.md)  |
| M5  | Previews & jobs                                        | ✅         | [detail](milestones/M5.md)  |
| M6  | Trash, move & upload queue                             | ✅         | [detail](milestones/M6.md)  |
| M7  | Search & command palette                               | ✅         | [detail](milestones/M7.md)  |
| M8  | Public browsing, preview tabs, editor chrome           | ✅         | [detail](milestones/M8.md)  |
| M9  | Stars, activity, recent & home                         | ✅         | [detail](milestones/M9.md)  |
| M10 | Space settings & admin console                         | ✅         | [detail](milestones/M10.md) |
| M11 | A space is a company, and the disk is honest           | ✅         | [detail](milestones/M11.md) |
| M15 | Owner-anchored permissions                             | ✅         | [detail](milestones/M15.md) |
| M16 | Login → account → space, one shape                     | ✅         | [detail](milestones/M16.md) |
| M17 | One dialog owns access                                 | ✅         | [detail](milestones/M17.md) |
| M18 | The shell is the shadcn dashboard shell                | ✅         | [detail](milestones/M18.md) |
| M19 | One owner decides where a write lands                  | ✅         | [detail](milestones/M19.md) |
| M20 | Sessions are rows                                      | ✅         | [detail](milestones/M20.md) |
| M21 | Access re-resolved, and the controls that were missing | ✅         | [detail](milestones/M21.md) |
| M22 | Backup and restore                                     | ✅         | [detail](milestones/M22.md) |
| M23 | Mail transport, invitations only                       | ✅         | [detail](milestones/M23.md) |
| M24 | The REST/WOPI write collision                          | ✅         | [detail](milestones/M24.md) |
| M25 | Observability                                          | ✅         | [detail](milestones/M25.md) |
| M26 | Audit completeness                                     | ✅         | [detail](milestones/M26.md) |
| M27 | Content-indexed search                                 | 🟡 partial | below                       |
| M28 | Directory sync                                         | 🔜 planned | below                       |
| M29 | Comments and notifications                             | ✅         | [detail](milestones/M29.md) |
| M30 | Resumable upload and honest accounting                 | ✅         | [detail](milestones/M30.md) |
| M31 | Document creation, and share notifications             | ✅         | [detail](milestones/M31.md) |
| M32 | The UI speaks more than one language                   | ✅         | [detail](milestones/M32.md) |
| M33 | Membership is a Logto organization                     | ✅         | [detail](milestones/M33.md) |
| M34 | One settings dialog                                    | ✅         | [detail](milestones/M34.md) |
| M35 | Instance settings in the admin console                 | ✅         | [detail](milestones/M35.md) |
| M36 | Sharing hardening                                      | ✅         | [detail](milestones/M36.md) |
| M37 | Blob integrity                                         | ✅         | [detail](milestones/M37.md) |
| M38 | Two-way Logto sync                                     | ✅         | [detail](milestones/M38.md) |
| M39 | Block documents (Markdown + slash-editor)              | ✅         | [detail](milestones/M39.md) |
| M40 | Block documents, completed                             | ✅         | [detail](milestones/M40.md) |
| M41 | Agent access over MCP                                  | ✅         | [detail](milestones/M41.md) |
| M42 | Personal spaces and external sharing                   | ✅         | [detail](milestones/M42.md) |
| M43 | Pages: page links and sub-pages                        | ✅         | [detail](milestones/M43.md) |
| M44 | Extension host                                         | ✅         | [detail](milestones/M44.md) |
| M45 | Kanban core                                            | ✅         | [detail](milestones/M45.md) |
| M46 | Kanban collaboration                                   | ✅         | [detail](milestones/M46.md) |
| M47 | WebDAV access                                          | ✅         | [detail](milestones/M47.md) |

## Upcoming — M27 Onward

Everything above shipped except M27's office half and M28, including
the P0 register in full as of M24. None of the below is scheduled; the numbering is
the order the dependencies suggest, which is also
[production-readiness.md](production-readiness.md)'s phasing. Each entry names
the section of that document holding the evidence rather than restating it, so
there is one place to check whether a gap is still real.

### Phase B — The multi-department layer

Additive to patterns that already exist.

#### M27 — Content-Indexed Search 🟡 markdown and plain text shipped

P1, "Search only matches file names". **Shipped for markdown and plain text**
(migration `0016_content_search.sql`): an `extract:text` job on the preview
queue, enqueued by the version hook (and on restore) for `text/markdown` —
through `mdoc.PlainText`, so snippets carry no markdown syntax — and
`text/plain`, writes one `node_texts` row per file from its current version,
folded for accent-insensitive Vietnamese by `internal/textsearch`; `cmd/server`
backfills existing files at startup. `GET /api/v1/search` merges the two
matches — every name hit above every content-only hit, `ts_rank` within
content — and each item says `match: "name" | "content"` with a `snippet` and
`snippet_highlights` (rune offsets); the palette shows them.
[preview-pipeline.md](preview-pipeline.md#text-extraction) has the detail.

**What remains is office documents** (and any other format — PDF text is not
extracted either). The
registry is ready for them: an extractor needs its own job kind, registered in
`cmd/worker` beside the OnlyOffice converter and added to the MIME map, and
storage, backfill and search need no change.

#### M28 — Directory Sync 🔜 planned

P1, "Identity is managed by hand". **M33 delivered the membership half**:
accounts are pre-provisioned from the organization's member list, and membership
plus admin status arrive from the IdP. What is left is the rest of the directory
— no SCIM, no admin create-user, and **no group sync**, because a Logto
organization template is tenant-wide and cannot express a per-space group, so
department structure below the company line is still built here and kept in step
by hand. Deprovisioning is the harder half: a departure is revoked immediately
and torn down per space on an admin's confirmation, and since M38 a suspension
or deletion in Logto suspends the account and ends its sessions — but there is
still no `DeleteUser`, and no instance-level equivalent of the archive
hand-off.

### Phase C — Collaboration and storage honesty

M29 shipped (comments on block documents with in-app notifications and a
live stream — [detail](milestones/M29.md)). What it deliberately did not
build: comment or mention **email**, and comments on anything but a block
document.

### Phase D — Agent access

M41 shipped (chat-app agents act as the signed-in person over MCP —
[detail](milestones/M41.md), design in [agent-access.md](agent-access.md)).
What it deliberately did not build, and so what comes next on this line: an
**agent embedded in pp-hub**, over the same `internal/agenttools` registry;
binary upload, which a tool call's JSON cannot carry; and file management
(folders, rename, move, trash) as tools.

M47 shipped the other half for agents with a shell: a WebDAV mount at
`${PP_EXTERNAL_URL}/dav/` with its own personal token, so local tools work on
space files as files ([detail](milestones/M47.md), design in
[webdav.md](webdav.md)). What it deliberately did not build: delta sync,
offline work, files-on-demand placeholders, sharing over WebDAV, a change feed.

### Phase E — Personal use

M42 shipped (personal spaces beside organization spaces, sharing by email
across spaces including addresses with no account — [detail](milestones/M42.md),
design in [personal-spaces.md](personal-spaces.md)). What it deliberately did
not build: billing and plans, cross-space move/copy, external grants to
groups, and re-sharing by an outsider.

### Phase F — Pages

M43 shipped (Notion-style pages on block documents, from slash-editor's pages
feature — [detail](milestones/M43.md), design in
[block-documents.md](block-documents.md#pages)): sub-pages held in the node
tree (a `.md` whose parent is a `.md`, one `fs.CanContain` rule), `pageLink`
chips with live titles and backlinks, a page tree, breadcrumb, icon and cover
(frontmatter, live in the room), sub-pages through share links, recursive
export, zip and copy, MCP and search. What it deliberately did not build:
databases or collection views, drag-and-drop re-parenting in the page tree
(Move takes a document as destination instead), realtime push of titles and
tree changes (they refetch), and sub-pages that are not markdown.

### Phase G — Extensions

M44–M46 shipped (small apps compiled into the server and the SPA behind a
public SDK, switched on per instance and per space — the xcaddy model, not
runtime install and not a sandbox — with a kanban board as the first one;
[M44](milestones/M44.md), [M45](milestones/M45.md), [M46](milestones/M46.md),
design in [extensions.md](extensions.md)). What they deliberately did not
build, and so what comes next on this line: share links to boards, a
cross-board "assigned to me" view and due-date reminders, MCP tools from
extensions, a published SDK with a semver promise (after a second extension
has been written against the contract alone), per-extension translation
catalogs.

### Named but Unscheduled

Named in P1's "Smaller, but on the list" and assigned to no milestone. Recorded
so they are found rather than rediscovered:

- **`style-src 'unsafe-inline'`** is the one CSP directive in the nginx template
  that is looser than it should be rather than looser on purpose. Removing it
  takes changes in `apps/web`, not an edit at the edge.
- **No antivirus or content scanning.** Uploaded bytes go straight into the CAS
  with no scan hook, no sniffing (the type is guessed from the extension) and no
  extension blocklist.
- **No encryption at rest.** Local blobs are plaintext, the S3 backend sets no
  SSE headers, and Postgres runs `sslmode=disable` inside the compose network.
  Share and session tokens _are_ stored hashed.

## Testing Strategy

- **Unit**: capability resolver matrix (ownership × grant × ancestor × link),
  blob CAS (put/get/dedup/gc), tree ops (move-cycle rejection, rename conflicts).
- **Database-backed**: the invariants that live in SQL — owner inheritance,
  admin-has-no-access, member lifecycle, organization convergence and the
  departure queue, session lookup and revocation, WOPI access re-resolved against
  live grants, sharing bounds — run against a real Postgres and skip without
  `PP_TEST_DATABASE_URL`.
- **Structural**: three router walks. `TestEveryAPIRouteIsAuthenticated`,
  `TestEveryAPIMutationChecksOrigin` and `TestWopiIsExemptFromOriginChecks`. A
  convention nobody can forget is worth more than one everybody is told about.
- **Integration** (compose test profile): upload→download byte-identical
  roundtrip; WOPI flow against real OnlyOffice (open → PutFile → version
  increments); link share access without auth.
- **Contract**: hand-typed TS interfaces in `apps/web/src/lib/api/types.ts`
  mirror the Go handlers; OpenAPI codegen still deferred.
- **Load sanity**: k6 — 100 concurrent uploads, 5-user co-edit; pass = no errors,
  no performance target yet.

## Rollout / Operations

- Two compose files in `deploy/compose/`: `compose.dev.yml` (services only, app
  runs from source) and `compose.prod.yml` (everything, for a VPS or Dokploy).
- Persistent state: a `data/` volume (blobs, plus in-flight resumable uploads
  under `.uploads`, which backups skip) plus the Postgres volume.
- Backup: `deploy/backup/pp-backup.sh` and `pp-restore.sh` — implemented in M22,
  Postgres dumped before blobs are archived and never the reverse, self-verifying
  and driven through `docker compose run`/`exec`.
- [production-readiness.md](production-readiness.md) holds everything that stood
  between M21 and an internal rollout: the P0 register, now fully closed; the P1
  gaps against Google Drive still open — content search for office documents
  (M27) and IdP group sync (M28) — and the phases both documents follow.
