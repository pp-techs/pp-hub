# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Versions up to and including 0.14.1 predate this changelog; see the
[GitHub Releases](https://github.com/pp-techs/pp-hub/releases) and
[docs/SUMMARY.md](docs/SUMMARY.md) for what shipped in them.

<!-- `bun run version-packages` adds each release's section from its changesets; edit it in the Version Packages PR. -->

## [1.0.0] — 2026-10-22

Kanban and Checklist are no longer built in: they are now ordinary uploadable apps, and pp-hub ships with no apps installed. **Operator action:** after upgrading, an admin uploads `kanban-2.0.0.zip` and `checklist-2.0.0.zip` (build them with `bun run apps:build`; they land in `examples/apps/kanban/dist-pack/` and `examples/apps/checklist/dist-pack/`) in Admin › Apps. Until then, existing boards and checklists stay in the drive but open as "not part of this installation"; nothing is lost. The on/off switches are kept, so an app that was on comes back on with its upload; a fresh install starts with both off, like every app.

Migration `0032` moves every board (columns, labels, cards with their assignees, labels, links and due dates) and every checklist into the app record store under the same ids, keeps card comment threads and notification links, and drops the old `ext_kanban` and `ext_checklist` schemas. It is applied automatically.

Apps can now be uploaded. In Admin › Apps, **Upload app** installs an app from a `.zip` or a folder — no rebuild, no restart — and **Upload new version…**, **Backend…** and **Remove…** manage it afterwards. An uploaded app runs in a sandboxed frame with no access to the session: it reads and writes only the item it is opened on, with the access of whoever opens it, through the record store, live updates, comments and notifications pp-hub provides. An app that needs its own server names a backend; pp-hub forwards its calls there with a short-lived signed token (generate the secret under **Backend…**). Like every app, an uploaded one arrives switched off.

Write one with `bun run apps init <dir>`, package it with `bun run apps pack <dir>`, and see `examples/apps/tasks` and `packages/app-sdk`. Migration `0031` adds the package and record tables and is applied automatically. App files are served under `/x/` — from `PP_ARTIFACT_URL`'s host when it is set — and an app's backend calls back under `/api/apps/`; a proxy in front of pp-hub must pass both through, and treat `/api/v1/apps/*/events` like the notification stream (no buffering).

## [0.30.1] — 2026-10-21

Remove the in-app file tab strip and keep a file's Share, Versions and Download controls in the single header. Files open in the current browser tab by default, with a per-browser preference to open each in a new browser tab.

## [0.30.0] — 2026-10-21

Office files (Word, Excel, PowerPoint, PDF) now open in a new browser tab by default, on a page that holds only the OnlyOffice editor — no sidebar, header or tab strip — so it gets the full window height. A new preference, Account → Preferences → Opening files, switches back to opening them inside the drive. It is stored in the browser, not the account.

Office documents no longer show Vietnamese letters in a mismatched font. The OnlyOffice image's 2006 Calibri and Tahoma lack most Vietnamese letters, so they were drawn in another face mid-word, in the editor and in thumbnails; they are now removed and the editor substitutes Carlito and Open Sans, which cover them with the same metrics. The `onlyoffice` service also installs a set of open-licence fonts that support Vietnamese (Roboto, Noto, Lato, Inter, Montserrat, IBM Plex, Be Vietnam Pro, Fira Sans and more) into a new `onlyoffice_fonts` volume on its first start, which needs outbound access to the Ubuntu archive and `raw.githubusercontent.com` and takes a few minutes; fonts of your own can be copied into the same volume. **If you run your own compose (for example a Dokploy copy)** add the `fonts` step from the `entrypoint` in `compose.prod.yml`, the `onlyoffice_fonts` volume and its mount at `/usr/share/fonts/truetype/custom`. See [docs/deployment.md](docs/deployment.md#onlyoffice-fonts).

Small interface fixes: the drive icon in the Move dialog now turns light when its row is selected, the keyboard hint on "Move to trash" is drawn in the destructive colour like the rest of the item, the tabs on Admin → Instance settings use the standard tab text size, and the header and the open-file tab bar now share the page's side padding (`px-4`) instead of widening to `px-6` on large screens, on the drive and in administration alike.

## [0.29.0] — 2026-10-20

Publish an `.html` file or a folder as a public mini web site, like an AI chat's artifact link: `Publish…` takes a snapshot, gives it a stable `/p/<link>` URL, and Republish updates it in place. Scripts run sandboxed with no network access; styles, scripts and fonts may come from an admin-chosen CDN allowlist. A publication can carry a password and an expiry, and agents can publish over MCP (`publish_artifact`). Operators enable it by switching on Admin › Settings › Sharing › Publishing; it is off by default. No second domain is needed: published pages are served under `/a/` on the app's own address, guarded by the browser's CSP sandbox alone. Before letting untrusted people publish, set `PP_ARTIFACT_URL` to a separate domain routed to `server` so pages run on an origin with no session; the admin page warns until it is set.

## [0.28.1] — 2026-10-20

The open-file bar is more compact: the version and size line is gone, and Share, Versions and Download are icon buttons with tooltips. A long folder breadcrumb now keeps only its last two entries and folds the rest into a `…` dropdown.

## [0.28.0] — 2026-10-20

People with no account can now open what was shared with their email address. Turn on **Admin › Settings › Sharing › Let people without an account open what was shared** (`guest_access`, off by default; it also needs external sharing on and outgoing mail configured). A view-only share to an address with no account mails an invitation that says to enter the address; the sign-in page then emails a six-digit code, and a correct code opens the share for seven days of activity, with view and comment but no spaces, search, settings or history. A later sign-in through Logto with the same address turns the guest into an ordinary account. Migration `0029` adds `users.kind` and `guest_otps`.

## [0.27.0] — 2026-10-19

Each release now attaches a desktop app (macOS arm64 and x64 `.dmg`, Windows x64 `.msi`) built with Pake around your deployment's URL. Set the repository variable `DESKTOP_APP_URL` to the address users open pp-hub at; the build fails until it is set. The apps are unsigned, so macOS and Windows warn on first launch.

## [0.26.0] — 2026-10-19

Search now looks inside PDFs and office documents (`.docx`, `.xlsx`, `.pptx`, `.doc`, `.xls`, `.ppt` and the OpenDocument formats), accents ignored, as it already did for markdown and plain text. The text is read by the worker, so it needs `cmd/worker` running with OnlyOffice reachable and `PP_SESSION_KEY` matching the server's, as for office thumbnails; the server queues every existing file at its next start and the worker drains them, so no operator step is needed beyond having a worker. Without one those files stay matched by name only. Scanned PDFs are not OCR'd.

The Share dialog of a file or folder now has a **Copy link** button. The link is the same `/open/<id>` address the share mails carry: it opens the item for anyone who already has access (in the editor, or in the folder's listing) and shows "no access" to anyone else — it grants nothing on its own, unlike a public link.

## [0.25.0] — 2026-10-16

**Breaking for operators:** the container images are renamed so they are recognisable outside this repository: `ghcr.io/pp-techs/pp-hub/server` is now `ghcr.io/pp-techs/pp-hub-server`, and `ghcr.io/pp-techs/pp-hub/worker` is now `ghcr.io/pp-techs/pp-hub-worker`. `deploy/compose/compose.prod.yml` already uses the new names, so a stack deployed from it needs nothing beyond pulling the new release. A hand-written compose file or platform application that names the old images must be edited, and `PP_IMAGE_TAG` has to be a release from this one on; the old packages are no longer updated.

## [0.24.1] — 2026-10-16

A file or folder shared with you no longer appears at the root of your own drive; it is listed under "Shared with me". Team folders you have access to stay at the root, and WebDAV listings are unchanged.

## [0.24.0] — 2026-10-16

OnlyOffice's AI plugin is not in the 10.0.0.1 image's default plugin list or in its in-editor Plugin Manager, so the editor had no AI tab and users could not add one. The `onlyoffice` service now installs it on start (needs outbound access to `onlyoffice.github.io` on first start); nothing is sent to an AI provider until a user sets one up with their own API key in the AI tab. A hand-written compose needs the `grep … sed …` step from the `entrypoint` in `compose.prod.yml`.

Files now open inside the drive instead of in a new browser tab. A document, spreadsheet, presentation, board, PDF, image, video or text file opens under the same sidebar, and the header's breadcrumb shows its folder path. The header keeps only navigation: the sidebar toggle, the breadcrumb, search and notifications. A document bar under it shows every open file as a tab, alongside the open file's own controls: outline, comments, Share, Versions and Download. The list of open files is kept per space across reloads. Switching tabs brings a recently opened file back without reloading it. Ctrl/⌘-click on a tab still opens it in a separate browser tab, and a middle click closes it. Up to five recently shown files are kept loaded, at most two of them office files. An office file that has been out of sight for ten minutes is unloaded so the file is no longer locked. Old `/edit/…` links keep working.

PDFs now open in OnlyOffice, the same way documents, spreadsheets and presentations do: as a tab inside the drive. In the editor you can comment on a PDF and fill its forms, and with edit access you can also edit it; saving creates a new version. While a PDF is open in the editor it is locked, the same as an office file. The quick preview in the drive still uses the browser's own PDF viewer. A share link opens a PDF in OnlyOffice too, read-only; without download access, printing and export are turned off.

## [0.23.0] — 2026-10-13

OnlyOffice Document Server updated from 9.4.0 to 10.0.0.1. 10.0 refuses to call a WOPI host that no `ipfilter` rule names (`allow: *` no longer counts), so without one the editor shows only "Access forbidden". `compose.prod.yml` now writes that rule itself; nothing to do if you use it as shipped. **If you run your own compose (for example a Dokploy copy)** you must (a) bump the image to `onlyoffice/documentserver:10.0.0.1` and (b) give the `onlyoffice` service this entrypoint, whose rule names the host of `PP_WOPI_SERVER_URL` (`server` here; change it if you renamed the service):

```yaml
entrypoint:
  - sh
  - -c
  - |
    cat > /etc/onlyoffice/documentserver/local-production-linux.json <<'EOF'
    {"services":{"CoAuthoring":{"ipfilter":{"rules":[
      {"address":"server","allowed":true},
      {"address":"*","allowed":false}
    ]}}}}
    EOF
    exec /app/ds/run-document-server.sh
```

The Document Server can now only be asked to fetch from the named host. After an unclean disconnect an edit is still saved only 5–10 minutes later, as before.

Developers: `host.docker.internal` is refused by OnlyOffice 10 on OrbStack, so the dev stack gains a `wopi-proxy` service. Re-run `docker compose -f deploy/compose/compose.dev.yml --profile worker up -d`, and set `PP_WOPI_SERVER_URL=http://wopi-proxy:8081` in `apps/server/.env` and `deploy/compose/.env`. If you set `PP_SESSION_KEY` in `apps/server/.env`, set the same value in `deploy/compose/.env`.

**Breaking for operators:** the nginx `web` container and the `ghcr.io/pp-techs/pp-hub/web` image are gone. The `server` image now serves the built SPA itself on the same listener as the API (new `PP_WEB_DIR`, preset to `/srv/web` in the image; unset keeps the API-only development mode), with the security headers, CSP, gzip and cache headers nginx used to add. Re-point your Dokploy/Traefik/Caddy domain from `web:80` to `server:8080`, and use `server` instead of `web` in `docker compose logs`/`restart`. WOPI moved to its own listener, `PP_WOPI_ADDR` (default `:8081`), which is not on the public port: set `PP_WOPI_SERVER_URL` to `http://server:8081` in any hand-written environment (`compose.prod.yml` already does). `PP_ONLYOFFICE_URL` is now read only by `server`. Docker builds of `server` and `worker` use the repository root as build context (`docker build -f apps/server/Dockerfile --target server .`).

## [0.22.0] — 2026-10-13

Mermaid diagrams are no longer all grey: nodes, actors, notes and loop boxes now take a tinted fill and a coloured border from the brand colour, and pie charts, timelines and other multi-series diagrams use a full colour palette, in both light and dark mode.

Text and code files now preview in full: plain text, logs, JSON, YAML, config and source files (Go, TypeScript, Python, and many more) open in the preview with syntax colouring and line numbers, instead of showing nothing or only the first 4 KB. Large files show their first 256 KB with a note.

## [0.21.1] — 2026-10-13

Trashing a folder no longer drops the items you put inside it onto your top level; they stay in the trash with the folder and come back when you restore it. Work a colleague added to your folder still appears on their own top level.

## [0.21.0] — 2026-10-12

Folders can now be downloaded from the drive: **Download as .zip** appears in a folder's menu, in the selection bar, and beside folders in "Shared with me", for anyone who holds download on the folder. The archive is the one the server already streamed for `GET /nodes/{id}/download` on a folder.

## [0.20.0] — 2026-10-12

Markdown documents now show files beside them by relative path, as a README in a repository does: `![](images/a.png)`, `[spec](../spec.pdf)`, and relative video or file blocks render from the drive, and relative links open the page or file they name. This covers documents written by agents through a WebDAV mount, `docs/` folders uploaded from a repository, and unpacked exports. The markdown keeps the path. A writer can **Embed in document** to keep the document's own copy of the file instead.

Access change: readers of a document, share-link visitors included, now see the files it names inside the folder it sits in, when the author of the version being read can download those files. A document at the top of a space reaches nothing this way, and someone who can edit only that one document cannot use it to read the files around it.

Document export brings those files into `assets/`.

## [0.19.1] — 2026-10-10

Large drives stay fast. Folders with thousands of items render only the rows on screen, folder listings resolve access once per folder instead of once per item, and Home, Recent and the space list no longer slow down with history. Storage usage is now kept current by the database instead of being re-summed on every save, and it now drops when files are purged from Trash; the first start after upgrading recomputes every space's figure, which may lower it. Finished preview jobs older than 7 days are deleted automatically (failed jobs are kept). Thumbnails are no longer generated for images over about 67 megapixels or SVG files over 8 MiB.

The first start after upgrading builds new database indexes; writes to the affected tables wait until that finishes.

## [0.19.0] — 2026-10-09

**Reorder, re-parent and trash sub-pages from the page tree.** Drag a row above or below another to reorder it, or onto the middle of a row to make it a sub-page of it; Alt+↑/↓ moves it among its siblings from the keyboard, and a row's trash button removes it with an Undo toast. The order is kept per parent (`PATCH /nodes/{id}/move` takes an optional `index`); pages never moved keep their creation order. Includes slash-editor 0.8.0 and migration `0026_page_order.sql`, applied automatically.

## [0.18.2] — 2026-10-09

**Fix: administration pages share one layout.** Instance settings and Apps were capped at a narrow width and centered, while Users and Security log used a wider cap. Every administration page now fills the page with the same padding, like the drive.

**Fix: files on a WebDAV mount with accented names could not be opened from Finder.** Finder and macOS apps ask for a name like `Bùi Đức Nhật` in decomposed Unicode (NFD) while pp-hub stores and lists it composed (NFC), so listing a folder worked but opening a file in it failed with "can't be found". WebDAV paths now match in either form.

## [0.18.1] — 2026-10-09

**Instance settings are split into tabs.** `/admin/settings` no longer stacks every setting in one long page: General (name and personal spaces), Storage, Security, Sharing & agents and Mail each have their own tab, linkable as `?tab=`. Changes on several tabs are still saved together from the one "unsaved changes" bar; Mail keeps its own Save.

## [0.18.0] — 2026-10-09

**Mount your drive over WebDAV.** People can mount their spaces at `${PP_EXTERNAL_URL}/dav/` in Finder, Windows Explorer or rclone (recommended for coding agents, with `--vfs-cache-mode full`) and work on files as files. A mount is another view of the same nodes, versions and grants: every write goes through the API as the person, a save over a file becomes a new version, a write that raced someone else's lands beside the file as a conflicted copy, and a delete moves to the trash. It signs in with a new kind of personal token, minted in **Settings › Security › Mount as a drive** (a WebDAV token is not accepted at `/mcp/`, nor an MCP token at `/dav`). Administrators turn it on in **Admin › Settings › WebDAV** (`webdav_enabled`, off by default). The bundled nginx proxies `/dav` with no body limit, no buffering and one-hour timeouts; a proxy you put in front of pp-hub must allow the same, because a WebDAV upload is a single request.

## [0.17.0] — 2026-10-08

Apps: pp-hub can now carry small apps (extensions) compiled into the server and the web build, and the first one is a kanban board. An app ships switched off — an instance administrator turns it on under Admin › Apps, and a space administrator can turn it off for a space under Settings › Apps. An app's items are listed under Apps in the sidebar, not among the drive's files and folders; a new one is made from its app's entry there, is private to its creator until shared, and is shared, starred, copied and trashed like a file. Cards move between columns by drag and drop, carry assignees, a due date, labels, links to drive files and a discussion with @mentions, and several people see each other's changes live. Being put on a card sends a notification. Each app keeps its tables in a Postgres schema of its own (`ext_<id>`), which migrations create at startup. Operators who terminate TLS with their own nginx must give `/api/v1/ext/<id>/…/events` the same unbuffered, long-timeout treatment as `/api/v1/notifications/stream` (the shipped `nginx.conf` does). A second small app, Checklist (a list of items to tick, live for everyone who has it open), ships beside it, switched off like any app. An app is now a plugin package: one `pp-app.json` manifest, a Go package and an npm package built against public SDKs (`extsdk`, `@pp-hub/ext-sdk`), registered in `pp-apps.json`; the `pp-apps` command scaffolds, wires and checks them, and Admin › Apps shows each app's version, description and author.

## [0.16.0] — 2026-10-08

Agents connected over MCP can now download files: the new `download_file` tool returns a file's bytes (base64, up to 8 MiB) — office documents, PDFs, images — for anyone who has download permission on it. It is read-only; writing binary files back is not supported.

**Moving between pages in a document no longer blanks the screen.** Opening a sub-page or a linked page used to tear the editor down: "Opening editor…", then a skeleton and an empty page tree before the page appeared. The page you are on now stays until the next one is ready, and the two swap at once. Hovering a page in the tree, a sub-page block or a page link also starts loading it ahead of the click.

## [0.15.0] — 2026-10-07

**Server administration redesigned.** `/admin` now has its own sidebar (Users, Security log, Instance settings) instead of a top bar. Users get icon stat tiles, search with a result count, and a `⋯` menu for admin and suspend actions. The security log filters sit in a toolbar with a clear-filters button. Instance settings are grouped into cards with a source badge and Reset on each row, and a sticky save bar appears only when something changed. A non-admin opening an admin page is now redirected to the drive.
