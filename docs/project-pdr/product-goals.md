# Product Goals

## Vision

A minimal, self-hosted Google Workspace alternative: Drive, collaborative office editing, permissions, and multiple spaces on drive. Not a Nextcloud clone — deliberately feature-thin at the core, extensible later.

## Target Users

- Small teams / individuals self-hosting their files and documents.
- Later: SMB (50–500 users), multi-tenant SaaS.

## Competitive Landscape (decision inputs)

| Product                        | Why not                                                             |
| ------------------------------ | ------------------------------------------------------------------- |
| Nextcloud                      | PHP, slow, feature-bloated                                          |
| ocis (ownCloud Infinite Scale) | Written in Go but immature/weak; usable as reference, not as a base |
| Google Workspace               | Heavy, expensive, not self-hostable                                 |

## MVP Scope (must-have)

1. **Drive core** — upload/download, folder tree, move/copy/rename/delete, per-space organization.
2. **Multiple spaces** — isolated file areas per team/project with independent membership.
3. **Permissions** — everything is private to whoever created it; access is given per file/per folder to a person, a group, or a public link, and carries `view` or `edit` plus independent download and re-share flags. Space admins run membership, groups and settings; that grants them no access to anyone's files.
4. **Collaborative office** — edit and realtime co-edit docx/xlsx/pptx in browser via OnlyOffice Document Server (WOPI protocol).
5. **Version history** — list and restore previous versions of a file.
6. **Previews** — PDF, image, video, common text formats without download.

## Non-Goals (MVP)

- Desktop/mobile sync clients (must remain architecturally possible — see `docs/architecture/storage.md`).
- Plugin marketplace (module boundaries must not preclude it later).
- Multi-tenant SaaS operation (schema is tenant-ready, product is single-tenant self-host).
- Mail, calendar, chat, contacts, federation.
- Editing via share link (view/download only for link shares in MVP).

## Deployment Model

Self-host all-in-one: a single `docker compose up` brings up the app server, Postgres, OnlyOffice Document Server, and the identity provider (**Logto**; any OIDC provider works — Logto Cloud is the default dev IdP).

## Success Criteria (MVP)

- A new user can log in via OIDC, create a space, upload files, organize folders.
- A user can share a file to a teammate (direct) and to an outsider (link, view/download).
- Two users can co-edit the same .docx in browser with realtime collaboration.
- Every file save/upload produces a restorable previous version.
- The whole stack runs from one compose file on a single machine.
