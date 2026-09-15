# Documentation Summary

pp-hub — a minimal, self-hosted Google Workspace alternative: Drive, collaborative office (OnlyOffice/WOPI), permissions, and multiple spaces.
Monorepo: bun workspaces + Vite+ frontend (`apps/web`), Go modular-monolith backend (`apps/server`), shared UI package (`packages/ui`), Postgres + content-addressed disk storage, compose variants in `deploy/compose/`.

## Agent Context Guide

Before planning or implementing, read this `docs/SUMMARY.md` file first. Load only the detail docs relevant to the current task, and prioritize `Code Standard` docs for implementation conventions. If docs conflict with code or user intent, use the available question tool before making broad changes.

Status: M1–M5 done — full MVP: OIDC auth (Logto; any OIDC provider works), drive (spaces/nodes/CAS blob/upload/download/versioning), sharing (direct + link), OnlyOffice WOPI editing with locks, previews (image/PDF/video/text) via background jobs — PDFs and video render from the original
bytes at `GET /api/v1/nodes/{id}/content` (inline, allowlisted MIME), since their generated preview blobs are placeholders. Compose stacks in `deploy/compose/`. UI primitives extracted to `packages/ui` (shadcn/ui preset). Drive rail exposes Trash and Shared with me as panels (`?panel=trash|shared`); Trash supports restore, delete forever and empty trash. Uploads run through a queue with drag-and-drop, folder upload and a progress tray. ⌘K opens a command palette backed by `GET /api/v1/search` (trigram index, scoped to your space membership). Share links browse shared folders anonymously (containment-checked, zip download); stars, activity and a Home dashboard span every space; space settings cover slug/description/default role/quota and deletion, with members and pending invitations in one filterable table (role, joined, last active, files owned, CSV export); the admin console reports server stats and can suspend accounts. Blob GC reclaims disk from unreferenced content, quotas are enforced at upload, and per-space trash retention purges on a schedule (off by default).

## Architecture

System design, component interactions, data flows, deployment, and external integrations.

| File                                                        | Description                                                                                      |
| ----------------------------------------------------------- | ------------------------------------------------------------------------------------------------ |
| [system-design.md](architecture/system-design.md)           | Modular monolith rationale, component map, upload/WOPI/share data flows, tenancy & scale posture |
| [data-model.md](architecture/data-model.md)                 | Postgres schema, invariants, query patterns, quota handling                                      |
| [storage.md](architecture/storage.md)                       | Content-addressed blob store, blake3, dedup/versioning, GC, sync-readiness                       |
| [office-integration.md](architecture/office-integration.md) | WOPI host endpoints, access tokens, OnlyOffice co-editing, locking                               |
| [permissions.md](architecture/permissions.md)               | Space roles + per-node shares, resolution order, share-link security                             |
| [preview-pipeline.md](architecture/preview-pipeline.md)     | Preview job flow, the MIME→kind contract, why `preview not ready` happens                        |
| [milestones.md](architecture/milestones.md)                 | M1–M5 delivery plan, testing strategy, rollout/ops                                               |

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

| File                                                    | Description                                     |
| ------------------------------------------------------- | ----------------------------------------------- |
| [deploy/compose/README.md](../deploy/compose/README.md) | Dev workflows, compose file variants, env setup |
