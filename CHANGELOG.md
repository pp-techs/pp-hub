# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Versions up to and including 0.14.1 predate this changelog; see the
[GitHub Releases](https://github.com/pp-techs/pp-hub/releases) and
[docs/SUMMARY.md](docs/SUMMARY.md) for what shipped in them.

<!-- `bun run version-packages` adds each release's section from its changesets; edit it in the Version Packages PR. -->

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
