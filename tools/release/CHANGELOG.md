# @pp-hub/release

## 0.29.0

### Minor Changes

- 4d53ed8: Publish an `.html` file or a folder as a public mini web site, like an AI chat's artifact link: `Publish…` takes a snapshot, gives it a stable `/p/<link>` URL, and Republish updates it in place. Scripts run sandboxed with no network access; styles, scripts and fonts may come from an admin-chosen CDN allowlist. A publication can carry a password and an expiry, and agents can publish over MCP (`publish_artifact`). Operators enable it by switching on Admin › Settings › Sharing › Publishing; it is off by default. No second domain is needed: published pages are served under `/a/` on the app's own address, guarded by the browser's CSP sandbox alone. Before letting untrusted people publish, set `PP_ARTIFACT_URL` to a separate domain routed to `server` so pages run on an origin with no session; the admin page warns until it is set.

## 0.28.1

### Patch Changes

- eba80aa: The open-file bar is more compact: the version and size line is gone, and Share, Versions and Download are icon buttons with tooltips. A long folder breadcrumb now keeps only its last two entries and folds the rest into a `…` dropdown.

## 0.28.0

### Minor Changes

- a98025f: People with no account can now open what was shared with their email address. Turn on **Admin › Settings › Sharing › Let people without an account open what was shared** (`guest_access`, off by default; it also needs external sharing on and outgoing mail configured). A view-only share to an address with no account mails an invitation that says to enter the address; the sign-in page then emails a six-digit code, and a correct code opens the share for seven days of activity, with view and comment but no spaces, search, settings or history. A later sign-in through Logto with the same address turns the guest into an ordinary account. Migration `0029` adds `users.kind` and `guest_otps`.

## 0.27.0

### Minor Changes

- 7b74f01: Each release now attaches a desktop app (macOS arm64 and x64 `.dmg`, Windows x64 `.msi`) built with Pake around your deployment's URL. Set the repository variable `DESKTOP_APP_URL` to the address users open pp-hub at; the build fails until it is set. The apps are unsigned, so macOS and Windows warn on first launch.

## 0.26.0

### Minor Changes

- 3a574a5: Search now looks inside PDFs and office documents (`.docx`, `.xlsx`, `.pptx`, `.doc`, `.xls`, `.ppt` and the OpenDocument formats), accents ignored, as it already did for markdown and plain text. The text is read by the worker, so it needs `cmd/worker` running with OnlyOffice reachable and `PP_SESSION_KEY` matching the server's, as for office thumbnails; the server queues every existing file at its next start and the worker drains them, so no operator step is needed beyond having a worker. Without one those files stay matched by name only. Scanned PDFs are not OCR'd.
- 35334d8: The Share dialog of a file or folder now has a **Copy link** button. The link is the same `/open/<id>` address the share mails carry: it opens the item for anyone who already has access (in the editor, or in the folder's listing) and shows "no access" to anyone else — it grants nothing on its own, unlike a public link.

## 0.25.0

### Minor Changes

- 4b2b810: **Breaking for operators:** the container images are renamed so they are recognisable outside this repository: `ghcr.io/pp-techs/pp-hub/server` is now `ghcr.io/pp-techs/pp-hub-server`, and `ghcr.io/pp-techs/pp-hub/worker` is now `ghcr.io/pp-techs/pp-hub-worker`. `deploy/compose/compose.prod.yml` already uses the new names, so a stack deployed from it needs nothing beyond pulling the new release. A hand-written compose file or platform application that names the old images must be edited, and `PP_IMAGE_TAG` has to be a release from this one on; the old packages are no longer updated.

## 0.24.1

### Patch Changes

- c16ec20: A file or folder shared with you no longer appears at the root of your own drive; it is listed under "Shared with me". Team folders you have access to stay at the root, and WebDAV listings are unchanged.

## 0.24.0

### Minor Changes

- 714d98f: Files now open inside the drive instead of in a new browser tab. A document, spreadsheet, presentation, board, PDF, image, video or text file opens under the same sidebar, and the header's breadcrumb shows its folder path. The header keeps only navigation: the sidebar toggle, the breadcrumb, search and notifications. A document bar under it shows every open file as a tab, alongside the open file's own controls: outline, comments, Share, Versions and Download. The list of open files is kept per space across reloads. Switching tabs brings a recently opened file back without reloading it. Ctrl/⌘-click on a tab still opens it in a separate browser tab, and a middle click closes it. Up to five recently shown files are kept loaded, at most two of them office files. An office file that has been out of sight for ten minutes is unloaded so the file is no longer locked. Old `/edit/…` links keep working.
- 714d98f: PDFs now open in OnlyOffice, the same way documents, spreadsheets and presentations do: as a tab inside the drive. In the editor you can comment on a PDF and fill its forms, and with edit access you can also edit it; saving creates a new version. While a PDF is open in the editor it is locked, the same as an office file. The quick preview in the drive still uses the browser's own PDF viewer. A share link opens a PDF in OnlyOffice too, read-only; without download access, printing and export are turned off.

### Patch Changes

- ace3ff3: OnlyOffice's AI plugin is not in the 10.0.0.1 image's default plugin list or in its in-editor Plugin Manager, so the editor had no AI tab and users could not add one. The `onlyoffice` service now installs it on start (needs outbound access to `onlyoffice.github.io` on first start); nothing is sent to an AI provider until a user sets one up with their own API key in the AI tab. A hand-written compose needs the `grep … sed …` step from the `entrypoint` in `compose.prod.yml`.

## 0.23.0

### Minor Changes

- ddf66ba: OnlyOffice Document Server updated from 9.4.0 to 10.0.0.1. 10.0 refuses to call a WOPI host that no `ipfilter` rule names (`allow: *` no longer counts), so without one the editor shows only "Access forbidden". `compose.prod.yml` now writes that rule itself; nothing to do if you use it as shipped. **If you run your own compose (for example a Dokploy copy)** you must (a) bump the image to `onlyoffice/documentserver:10.0.0.1` and (b) give the `onlyoffice` service this entrypoint, whose rule names the host of `PP_WOPI_SERVER_URL` (`server` here; change it if you renamed the service):

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

- ddf66ba: **Breaking for operators:** the nginx `web` container and the `ghcr.io/pp-techs/pp-hub/web` image are gone. The `server` image now serves the built SPA itself on the same listener as the API (new `PP_WEB_DIR`, preset to `/srv/web` in the image; unset keeps the API-only development mode), with the security headers, CSP, gzip and cache headers nginx used to add. Re-point your Dokploy/Traefik/Caddy domain from `web:80` to `server:8080`, and use `server` instead of `web` in `docker compose logs`/`restart`. WOPI moved to its own listener, `PP_WOPI_ADDR` (default `:8081`), which is not on the public port: set `PP_WOPI_SERVER_URL` to `http://server:8081` in any hand-written environment (`compose.prod.yml` already does). `PP_ONLYOFFICE_URL` is now read only by `server`. Docker builds of `server` and `worker` use the repository root as build context (`docker build -f apps/server/Dockerfile --target server .`).

## 0.22.0

### Minor Changes

- 662e889: Text and code files now preview in full: plain text, logs, JSON, YAML, config and source files (Go, TypeScript, Python, and many more) open in the preview with syntax colouring and line numbers, instead of showing nothing or only the first 4 KB. Large files show their first 256 KB with a note.

### Patch Changes

- 662e889: Mermaid diagrams are no longer all grey: nodes, actors, notes and loop boxes now take a tinted fill and a coloured border from the brand colour, and pie charts, timelines and other multi-series diagrams use a full colour palette, in both light and dark mode.

## 0.21.1

### Patch Changes

- 71cfdba: Trashing a folder no longer drops the items you put inside it onto your top level; they stay in the trash with the folder and come back when you restore it. Work a colleague added to your folder still appears on their own top level.

## 0.21.0

### Minor Changes

- c985868: Folders can now be downloaded from the drive: **Download as .zip** appears in a folder's menu, in the selection bar, and beside folders in "Shared with me", for anyone who holds download on the folder. The archive is the one the server already streamed for `GET /nodes/{id}/download` on a folder.

## 0.20.0

### Minor Changes

- 999ee2e: Markdown documents now show files beside them by relative path, as a README in a repository does: `![](images/a.png)`, `[spec](../spec.pdf)`, and relative video or file blocks render from the drive, and relative links open the page or file they name. This covers documents written by agents through a WebDAV mount, `docs/` folders uploaded from a repository, and unpacked exports. The markdown keeps the path. A writer can **Embed in document** to keep the document's own copy of the file instead.

  Access change: readers of a document, share-link visitors included, now see the files it names inside the folder it sits in, when the author of the version being read can download those files. A document at the top of a space reaches nothing this way, and someone who can edit only that one document cannot use it to read the files around it.

  Document export brings those files into `assets/`.

## 0.19.1

### Patch Changes

- b18c460: Large drives stay fast. Folders with thousands of items render only the rows on screen, folder listings resolve access once per folder instead of once per item, and Home, Recent and the space list no longer slow down with history. Storage usage is now kept current by the database instead of being re-summed on every save, and it now drops when files are purged from Trash; the first start after upgrading recomputes every space's figure, which may lower it. Finished preview jobs older than 7 days are deleted automatically (failed jobs are kept). Thumbnails are no longer generated for images over about 67 megapixels or SVG files over 8 MiB.

  The first start after upgrading builds new database indexes; writes to the affected tables wait until that finishes.

## 0.19.0

### Minor Changes

- 0048f87: **Reorder, re-parent and trash sub-pages from the page tree.** Drag a row above or below another to reorder it, or onto the middle of a row to make it a sub-page of it; Alt+↑/↓ moves it among its siblings from the keyboard, and a row's trash button removes it with an Undo toast. The order is kept per parent (`PATCH /nodes/{id}/move` takes an optional `index`); pages never moved keep their creation order. Includes slash-editor 0.8.0 and migration `0026_page_order.sql`, applied automatically.

## 0.18.2

### Patch Changes

- 1b0de36: **Fix: administration pages share one layout.** Instance settings and Apps were capped at a narrow width and centered, while Users and Security log used a wider cap. Every administration page now fills the page with the same padding, like the drive.
- f98f4f7: **Fix: files on a WebDAV mount with accented names could not be opened from Finder.** Finder and macOS apps ask for a name like `Bùi Đức Nhật` in decomposed Unicode (NFD) while pp-hub stores and lists it composed (NFC), so listing a folder worked but opening a file in it failed with "can't be found". WebDAV paths now match in either form.

## 0.18.1

### Patch Changes

- d5eb576: **Instance settings are split into tabs.** `/admin/settings` no longer stacks every setting in one long page: General (name and personal spaces), Storage, Security, Sharing & agents and Mail each have their own tab, linkable as `?tab=`. Changes on several tabs are still saved together from the one "unsaved changes" bar; Mail keeps its own Save.

## 0.18.0

### Minor Changes

- 209dea7: **Mount your drive over WebDAV.** People can mount their spaces at `${PP_EXTERNAL_URL}/dav/` in Finder, Windows Explorer or rclone (recommended for coding agents, with `--vfs-cache-mode full`) and work on files as files. A mount is another view of the same nodes, versions and grants: every write goes through the API as the person, a save over a file becomes a new version, a write that raced someone else's lands beside the file as a conflicted copy, and a delete moves to the trash. It signs in with a new kind of personal token, minted in **Settings › Security › Mount as a drive** (a WebDAV token is not accepted at `/mcp/`, nor an MCP token at `/dav`). Administrators turn it on in **Admin › Settings › WebDAV** (`webdav_enabled`, off by default). The bundled nginx proxies `/dav` with no body limit, no buffering and one-hour timeouts; a proxy you put in front of pp-hub must allow the same, because a WebDAV upload is a single request.

## 0.17.0

### Minor Changes

- 6354393: Apps: pp-hub can now carry small apps (extensions) compiled into the server and the web build, and the first one is a kanban board. An app ships switched off — an instance administrator turns it on under Admin › Apps, and a space administrator can turn it off for a space under Settings › Apps. An app's items are listed under Apps in the sidebar, not among the drive's files and folders; a new one is made from its app's entry there, is private to its creator until shared, and is shared, starred, copied and trashed like a file. Cards move between columns by drag and drop, carry assignees, a due date, labels, links to drive files and a discussion with @mentions, and several people see each other's changes live. Being put on a card sends a notification. Each app keeps its tables in a Postgres schema of its own (`ext_<id>`), which migrations create at startup. Operators who terminate TLS with their own nginx must give `/api/v1/ext/<id>/…/events` the same unbuffered, long-timeout treatment as `/api/v1/notifications/stream` (the shipped `nginx.conf` does). A second small app, Checklist (a list of items to tick, live for everyone who has it open), ships beside it, switched off like any app. An app is now a plugin package: one `pp-app.json` manifest, a Go package and an npm package built against public SDKs (`extsdk`, `@pp-hub/ext-sdk`), registered in `pp-apps.json`; the `pp-apps` command scaffolds, wires and checks them, and Admin › Apps shows each app's version, description and author.

## 0.16.0

### Minor Changes

- f29ec81: Agents connected over MCP can now download files: the new `download_file` tool returns a file's bytes (base64, up to 8 MiB) — office documents, PDFs, images — for anyone who has download permission on it. It is read-only; writing binary files back is not supported.

### Patch Changes

- a6ebbd3: **Moving between pages in a document no longer blanks the screen.** Opening a sub-page or a linked page used to tear the editor down: "Opening editor…", then a skeleton and an empty page tree before the page appeared. The page you are on now stays until the next one is ready, and the two swap at once. Hovering a page in the tree, a sub-page block or a page link also starts loading it ahead of the click.

## 0.15.0

### Minor Changes

- 44c87aa: **Server administration redesigned.** `/admin` now has its own sidebar (Users, Security log, Instance settings) instead of a top bar. Users get icon stat tiles, search with a result count, and a `⋯` menu for admin and suspend actions. The security log filters sit in a toolbar with a clear-filters button. Instance settings are grouped into cards with a source badge and Reset on each row, and a sticky save bar appears only when something changed. A non-admin opening an admin page is now redirected to the drive.
