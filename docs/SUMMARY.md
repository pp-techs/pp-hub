# Documentation Summary

pp-hub — a minimal, self-hosted Google Workspace alternative: Drive, collaborative office (OnlyOffice/WOPI), permissions, and multiple spaces.
Monorepo: bun workspaces + Vite+ frontend (`apps/web`), Go modular-monolith backend (`apps/server`), shared UI package (`packages/ui`), Postgres + content-addressed disk storage, compose variants in `deploy/compose/`.

## Agent Context Guide

Before planning or implementing, read this `docs/SUMMARY.md` file first. Load only the detail docs relevant to the current task, and prioritize `Code Standard` docs for implementation conventions. If docs conflict with code or user intent, use the available question tool before making broad changes.

Status: M1–M5 done — full MVP: OIDC auth (Logto; any OIDC provider works), drive (spaces/nodes/CAS blob/upload/download/versioning), sharing (direct + link), OnlyOffice WOPI editing with locks, previews via background jobs — images, PDFs, video and office documents all get a real 256px thumbnail, text gets a 4KB snippet. PDF/video rasterization needs poppler/ffmpeg, and office documents convert through OnlyOffice first; all three live in an **optional** `cmd/worker` container; without it those jobs stay pending rather than failing, and `worker --backfill` sweeps files that predate it. Full-size viewing still streams the original bytes from `GET /api/v1/nodes/{id}/content` (inline, allowlisted MIME), since a 256px thumbnail is a stamp in a modal. Two compose stacks in `deploy/compose/` — `compose.dev.yml` (services only, app from source) and `compose.prod.yml` (everything, VPS/Dokploy); the web image is nginx serving the SPA and proxying the API on one origin, which the session cookie requires. See [deployment.md](deployment.md). UI primitives extracted to `packages/ui` (shadcn/ui preset). Drive rail exposes Trash and Shared with me as panels (`?panel=trash|shared`); Trash supports restore, delete forever and empty trash. Uploads run through a queue with drag-and-drop, folder upload and a progress tray. ⌘K opens a command palette backed by `GET /api/v1/search` (trigram index, scoped to your space membership). Share links browse shared folders anonymously (containment-checked, zip download); stars, activity and a Home dashboard span every space; space settings cover slug/description/quota, whether new members arrive as admins, and deletion, with members and pending invitations in one filterable table (admin, joined, last active, files owned, CSV export); the admin console reports server stats and can suspend accounts. Blobs live on local disk or in any S3-compatible bucket (`PP_BLOB_BACKEND`), both behind one interface and one conformance suite (SeaweedFS ships in the compose files). Blob GC reclaims disk from unreferenced content, quotas are enforced at upload, and per-space trash retention purges on a schedule (off by default).

**M11 — a space is a company.** One instance hosts several companies: space membership grants **no file access at all**, and access comes from `groups` granted on folders (`shares.kind = 'group'`), inherited down the subtree. Resolution is one batched recursive query that also walks through trashed ancestors, so the trash needs no weaker rule of its own. Listings, search, starred, recent, activity and trash all narrow to what the caller can reach, in one batched query per page rather than a walk per row, and `Shared with me` is an actual list of grants rather than a slice of the filing cabinet. Groups are managed from a Groups tab in space settings, and a folder's `Who can open this…` dialog is the one place access is granted or revoked; behind them sit `/spaces/{id}/groups` and `/nodes/{id}/grants`. A direct share may not name someone outside the space — that is what link shares are for.

**M15 — permissions are owner-anchored.** Access comes from exactly two things, both gated on **current membership of the node's space**: **ownership** (`nodes.created_by`, inherited down the subtree and resolved in the same recursive query as the grants, so it is not a revocable row and nobody can be locked out of their own work) and **grants**. Everything a person creates is private to them until they share it — and leaving the space ends their reach, so a departure files their work in the Archive instead of moving a folder they can still read. The role scale is gone — no `space_members.role`, no `default_role`, no `roleRank`, no `RequireRole` — and **admin no longer means access**: admin is membership in the space's automatic `Admin` group, it gates space administration via `spaces.RequireAdmin`, and it gates no file at all, because the old model handed every admin an edit floor over the whole company tree. `shares.perm` became `level` (`view` | `edit`) plus independent `can_download` and `can_share` flags — the wire carries that triple directly, since no single ranked word can say "view and pass on, but do not download" — with DB CHECKs keeping an editor able to download and links view-only and unchainable, and `Caps.Covers` refusing to hand out more than the sharer holds (403). `can_download` gates taking a copy, not seeing bytes: `/content` streams inline under view, `/download` needs the flag, for members and anonymous links alike. Departure has an answer instead of a disclaimer: every space owns an `Archive` folder whose only grant is edit+download+share to the `Admin` group, and `RemoveMember` revokes the leaver's grants both ways, climbs nested ownership to the nearest ancestor owner, moves their remaining roots into the Archive and drops their group rows, refusing to lose the last admin (409). Personal folders retired — `personal_folders`, `spaces.personal_root_id` and the pinned "My files" are gone, a member's top level is `ListEntryPoints` (what they own or were granted), and a member can finally create a folder at their own top level. Node DTOs carry `caps`, so the UI offers exactly what the caller holds: sharing and the `Who can open this…` dialog follow `caps.share` rather than admin status, and any member may read the space's group list in order to grant one access. The fifteen development migrations were squashed into one baseline, `0001_init.sql`. See [permissions.md](architecture/permissions.md).

## Architecture

System design, component interactions, data flows, deployment, and external integrations.

| File                                                        | Description                                                                                      |
| ----------------------------------------------------------- | ------------------------------------------------------------------------------------------------ |
| [system-design.md](architecture/system-design.md)           | Modular monolith rationale, component map, upload/WOPI/share data flows, tenancy & scale posture |
| [data-model.md](architecture/data-model.md)                 | Postgres schema, invariants, query patterns, quota handling                                      |
| [storage.md](architecture/storage.md)                       | Content-addressed blob store, blake3, local + S3 backends, dedup/versioning, GC                  |
| [office-integration.md](architecture/office-integration.md) | WOPI host endpoints, access tokens, OnlyOffice co-editing, locking                               |
| [permissions.md](architecture/permissions.md)               | Ownership vs grants, Admin/Everyone groups, capabilities, archive on departure                   |
| [preview-pipeline.md](architecture/preview-pipeline.md)     | Preview job flow, job vs preview kinds, the optional worker, caching, backfill                   |
| [milestones.md](architecture/milestones.md)                 | M1–M15 delivery history, testing strategy, rollout/ops                                           |

## Codebase

Directory structure, entry points, API patterns, and key modules.

| File                                                      | Description                                                             |
| --------------------------------------------------------- | ----------------------------------------------------------------------- |
| [directory-structure.md](codebase/directory-structure.md) | Monorepo layout, `apps/server` module map, web data layer, entry points |

## Code Standard

Conventions, naming rules, tech stack versions, and development workflows.

| File                                                             | Description                                                                     |
| ---------------------------------------------------------------- | ------------------------------------------------------------------------------- |
| [backend-conventions.md](code-standard/backend-conventions.md)   | Go stack (chi, sqlc/pgx, go-oidc), module boundaries, error taxonomy, job rules |
| [frontend-conventions.md](code-standard/frontend-conventions.md) | Vite+/bun workflow, api/queries/ui-store layering, catalog versions, style      |

## Project PDR

Product goals, use cases, business rules, and constraints.

| File                                             | Description                                                                             |
| ------------------------------------------------ | --------------------------------------------------------------------------------------- |
| [product-goals.md](project-pdr/product-goals.md) | Vision, competitive landscape, MVP scope, non-goals, deployment model, success criteria |
| [use-cases.md](project-pdr/use-cases.md)         | UC-1..UC-10: drive, sharing, office co-edit, permissions, previews                      |

## Other

Repository-specific docs outside the standard topic folders.

| File                           | Description                                                                                        |
| ------------------------------ | -------------------------------------------------------------------------------------------------- |
| [deployment.md](deployment.md) | Prod + dev compose, env convention, the same-origin constraint, Dokploy, `/s/` routing, known gaps |
