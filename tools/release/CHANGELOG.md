# @pp-hub/release

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
