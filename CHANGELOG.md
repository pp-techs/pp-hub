# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Versions up to and including 0.14.1 predate this changelog; see the
[GitHub Releases](https://github.com/pp-techs/pp-hub/releases) and
[docs/SUMMARY.md](docs/SUMMARY.md) for what shipped in them.

<!-- `bun run version-packages` adds each release's section from its changesets; edit it in the Version Packages PR. -->

## [0.18.0] — 2026-10-09

**Mount your drive over WebDAV.** People can mount their spaces at `${PP_EXTERNAL_URL}/dav/` in Finder, Windows Explorer or rclone (recommended for coding agents, with `--vfs-cache-mode full`) and work on files as files. A mount is another view of the same nodes, versions and grants: every write goes through the API as the person, a save over a file becomes a new version, a write that raced someone else's lands beside the file as a conflicted copy, and a delete moves to the trash. It signs in with a new kind of personal token, minted in **Settings › Security › Mount as a drive** (a WebDAV token is not accepted at `/mcp/`, nor an MCP token at `/dav`). Administrators turn it on in **Admin › Settings › WebDAV** (`webdav_enabled`, off by default). The bundled nginx proxies `/dav` with no body limit, no buffering and one-hour timeouts; a proxy you put in front of pp-hub must allow the same, because a WebDAV upload is a single request.

## [0.17.0] — 2026-10-08

Apps: pp-hub can now carry small apps (extensions) compiled into the server and the web build, and the first one is a kanban board. An app ships switched off — an instance administrator turns it on under Admin › Apps, and a space administrator can turn it off for a space under Settings › Apps. An app's items are listed under Apps in the sidebar, not among the drive's files and folders; a new one is made from its app's entry there, is private to its creator until shared, and is shared, starred, copied and trashed like a file. Cards move between columns by drag and drop, carry assignees, a due date, labels, links to drive files and a discussion with @mentions, and several people see each other's changes live. Being put on a card sends a notification. Each app keeps its tables in a Postgres schema of its own (`ext_<id>`), which migrations create at startup. Operators who terminate TLS with their own nginx must give `/api/v1/ext/<id>/…/events` the same unbuffered, long-timeout treatment as `/api/v1/notifications/stream` (the shipped `nginx.conf` does). A second small app, Checklist (a list of items to tick, live for everyone who has it open), ships beside it, switched off like any app. An app is now a plugin package: one `pp-app.json` manifest, a Go package and an npm package built against public SDKs (`extsdk`, `@pp-hub/ext-sdk`), registered in `pp-apps.json`; the `pp-apps` command scaffolds, wires and checks them, and Admin › Apps shows each app's version, description and author.

## [0.16.0] — 2026-10-08

Agents connected over MCP can now download files: the new `download_file` tool returns a file's bytes (base64, up to 8 MiB) — office documents, PDFs, images — for anyone who has download permission on it. It is read-only; writing binary files back is not supported.

**Moving between pages in a document no longer blanks the screen.** Opening a sub-page or a linked page used to tear the editor down: "Opening editor…", then a skeleton and an empty page tree before the page appeared. The page you are on now stays until the next one is ready, and the two swap at once. Hovering a page in the tree, a sub-page block or a page link also starts loading it ahead of the click.

## [0.15.0] — 2026-10-07

**Server administration redesigned.** `/admin` now has its own sidebar (Users, Security log, Instance settings) instead of a top bar. Users get icon stat tiles, search with a result count, and a `⋯` menu for admin and suspend actions. The security log filters sit in a toolbar with a clear-filters button. Instance settings are grouped into cards with a source badge and Reset on each row, and a sticky save bar appears only when something changed. A non-admin opening an admin page is now redirected to the drive.
