# Writing an app (extension)

How a pp-hub app is built and shipped. Every app is a **packaged app**: a
sandboxed upload with no code in the server; the decision behind it is in
[extensions.md](extensions.md), and this page is the working guide. An
instance administrator uploads the `.zip` (or a folder) in Admin › Apps and it
works with no rebuild; its UI is static web files in a sandboxed `<iframe>`,
its data the host's record store, and any server logic lives in your own
service, reached through the host's proxy with a signed token. Anyone can write
one: the administrator who uploads it trusts it no further than the people
using it.

The first-party apps, Kanban (`examples/apps/kanban`) and Checklist
(`examples/apps/checklist`), are built this way too — React + Vite builds
against `@pp-hub/app-sdk`, packed by `bun run apps:build` into
`examples/apps/{kanban,checklist}/dist-pack/<id>-<version>.zip` and uploaded like any other app.
They are the reference for a Vite app with several collections, client-side
validation, people pickers, drive links, notifications and per-record
comments.

## Packaged apps

An app is a folder of static files with a `pp-app.json` at its root:

```
my_app/
  pp-app.json        the manifest (packaged form: has web.entry)
  index.html         web.entry — the page the frame opens
  app.js  style.css  your code; reference it relatively ("./app.js")
  icon.svg           optional icon(s), .svg or .png
  app-sdk.js         the bridge SDK, copied from packages/app-sdk (zero-build)
  app-sdk.d.ts       its types, for the editor
```

`examples/apps/tasks` is a complete one, written without a build step: a task
list with live updates, assignees found through people search and told by
`notify`, comments per task, read-only for viewers without edit, and `vi`
translations ([examples/apps/README.md](../../examples/apps/README.md)).

### The packaged manifest

```json
{
  "id": "tasks",
  "name": "Tasks",
  "version": "1.0.0",
  "description": "A shared task list with assignees and comments.",
  "author": "pp-hub",
  "license": "MIT",
  "engines": { "sdk": 1 },
  "icon": "icon.svg",
  "nodeKinds": [
    {
      "kind": "tasks.list",
      "label": "Task list",
      "newLabel": "New task list",
      "defaultName": "Untitled tasks",
      "newHint": "A checklist your team ticks off together",
      "seed": [
        {
          "collection": "tasks",
          "data": { "text": "Tick me off when you are done", "done": false }
        }
      ]
    }
  ],
  "web": { "entry": "index.html" },
  "notifications": { "assigned": "{actor} assigned you a task: {excerpt}" },
  "translations": { "vi": { "Tasks": "Công việc", "Task list": "Danh sách việc" } }
}
```

| Field                     | Rule                                                                                                                                     |
| ------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------- |
| `id`                      | required, `^[a-z][a-z0-9_]{0,31}$`; unique among installed apps                                                                          |
| `name`                    | required, non-blank, ≤ 60 characters                                                                                                     |
| `version`                 | required, semver; shown in Admin › Apps                                                                                                  |
| `description`             | required, non-blank, ≤ 300                                                                                                               |
| `author`                  | required, non-blank, ≤ 100                                                                                                               |
| `license`, `homepage`     | optional; `homepage` must be an http(s) URL                                                                                              |
| `engines.sdk`             | required, must be `1` (bridge protocol v1)                                                                                               |
| `icon`                    | optional; a file in the package, `.svg` or `.png`, ≤ 256 KiB                                                                             |
| `nodeKinds`               | required, 1–8 entries                                                                                                                    |
| `nodeKinds[].kind`        | required, `<id>.` + `^[a-z][a-z0-9_]{0,31}$`, unique                                                                                     |
| `nodeKinds[].label`       | required, non-blank, ≤ 60                                                                                                                |
| `nodeKinds[].newLabel`    | optional, ≤ 60; default `"New " + label`                                                                                                 |
| `nodeKinds[].defaultName` | optional, ≤ 120; default `label`                                                                                                         |
| `nodeKinds[].newHint`     | optional, ≤ 200; shown in the New dialog                                                                                                 |
| `nodeKinds[].icon`        | optional, as `icon`                                                                                                                      |
| `nodeKinds[].seed`        | optional, ≤ 100 `{collection, data}`: records written with every new item; `data` a JSON object ≤ 64 KiB                                 |
| `web.entry`               | required; a file in the package ending `.html`                                                                                           |
| `web.connect`             | optional, ≤ 10 bare `https` origins the page may `fetch()` directly; nothing else is reachable                                           |
| `backend.url`             | optional; http(s) URL, no query or fragment — see [A backend](#a-backend)                                                                |
| `notifications`           | optional; verb (`^[a-z][a-z0-9_.-]{0,31}$`) → template ≤ 200, using `{actor}` and `{excerpt}`                                            |
| `translations`            | optional; locale (`^[a-z]{2,3}(-[A-Za-z0-9]{2,8})?$`) → English string → translation; ≤ 50 locales, ≤ 500 strings each, ≤ 500 characters |

Unknown fields are errors at every level. Collection names
match `^[a-z][a-z0-9_]{0,31}$`; files the manifest names must exist in the
package. The same rules are checked by the server (`internal/apppkg`) and by
`pp-apps pack` (`tools/pp-apps/src/package-manifest.ts`);
`@pp-hub/app-sdk/pp-app.schema.json` is the JSON Schema an editor can use.

**Translations** key on the English text: the name, description, kind labels
and notification templates are looked up in the viewer's locale and fall back
to the English. The strings inside your page are yours to translate —
`context.locale` says which language to use.

### `init` and `pack`

```sh
bun run apps init path/to/my_app [--id my_app] [--name "My App"]
bun run apps pack path/to/my_app [-o my_app.zip]
```

- **`init`** scaffolds a zero-build app into an empty or absent directory —
  manifest, `index.html`, `app.js`, `style.css`, `icon.svg`, a README, and the
  SDK copied in as `app-sdk.js`/`app-sdk.d.ts`. The id defaults to the
  directory's name made valid.
- **`pack`** validates the manifest, checks every file it names exists, applies
  the package limits (2000 files, 10 MiB each, 50 MiB in all, icons ≤ 256 KiB,
  a 20 MiB zip), refuses root-absolute asset references (`src="/…"`,
  `href="/…"`) in the entry page, and writes `<dir>/../<id>-<version>.zip`. It
  packs everything under the folder — minus `node_modules`, dot-files and
  zips — when the entry is at the root, or only the entry's directory plus the
  manifest and its icons when the entry is nested (`dist/index.html`).
  Symlinks are refused: copy the file.

### Zero-build or a bundler

The app is served under `<asset base>/x/<id>/<digest>/`, a path that changes
with every version, so **every asset must be referenced relatively**.

- **Zero-build** — what `init` makes: plain ES modules, `import { connect }
from "./app-sdk.js"`. When `packages/app-sdk/src/index.js` changes, copy it
  over the app's `app-sdk.js` (and `index.d.ts` over `app-sdk.d.ts`).
- **A bundler** — import `@pp-hub/app-sdk` (a dependency-free ES module; private
  to this repository for now, so outside it copy or link `packages/app-sdk`),
  build with relative URLs — Vite: `base: "./"` — and point `web.entry` at the
  built page (`dist/index.html`). Inline scripts and `eval` are allowed by the
  CSP; anything loaded from another origin is not, except the instance's
  published-site CDN allow-list.

### The SDK

```js
import { connect, AppError } from "./app-sdk.js"; // or "@pp-hub/app-sdk"

const app = await connect(); // { applyTheme = true, timeoutMs }
const { node, user, caps, locale } = app.context;
```

`connect()` waits for the host's port, says `hello` and resolves with the
context: `app` (`id`, `version`), `node` (`id`, `name`, `space_id`,
`ext_kind`), `user` (`id`, `name`, `email`), `caps` (`view`, `download`,
`share`, `edit`, `owner`), `locale`, `theme`, and — when the item was opened
from a notification — `subject` and `thread`. A failed call rejects with
`AppError`, whose `status` is HTTP-like: 400 bad params, 403 no permission or
app off, 404, 409 stale `rev`, 502/503 backend.

| SDK call                                                           | Needs  | Does                                                                                |
| ------------------------------------------------------------------ | ------ | ----------------------------------------------------------------------------------- |
| `records.list(collection?)`                                        | `view` | every record (the host pages through them), in creation order                       |
| `records.get(id)`                                                  | `view` | one record                                                                          |
| `records.create(collection, data)`                                 | `edit` | a new record                                                                        |
| `records.update(id, data, { ifRev })`                              | `edit` | replace `data`; with `ifRev`, 409 if somebody wrote first                           |
| `records.merge(id, patch, { ifRev })`                              | `edit` | set the keys in `patch`, `null` removes one (one level deep)                        |
| `records.delete(id)`                                               | `edit` | delete, with the record's comment threads                                           |
| `on("records" \| "broadcast" \| "theme" \| "stream", fn)`          | `view` | subscribe; returns the unsubscribe                                                  |
| `broadcast(event, data?)`                                          | `view` | an event for everyone with the item open, not stored (≤ 16 KiB)                     |
| `people.search(query, limit?)`                                     | `view` | people who can open the item                                                        |
| `users.get(ids)`                                                   | `view` | names for ids — only members of the item's space and its records' authors           |
| `links.resolve(ids)`                                               | `view` | the drive items among `ids` the viewer can open                                     |
| `nodes.search(query)`, `nodes.open(id)`                            | –      | name search in the item's space, as the viewer's own search; open an item in pp-hub |
| `comments.open(subject, thread?)`, `comments.counts()`             | `view` | pp-hub's own comment panel for `record:<id>`; comment counts per subject            |
| `notify({ to, verb, excerpt?, subject? })`                         | `edit` | a bell notification, worded by the manifest's template for `verb`                   |
| `ui.toast(message, variant?)`, `ui.confirm({ title, message, … })` | –      | pp-hub's toast (labelled with the app's name) and confirm dialog                    |
| `backend.fetch(path, init?)`                                       | `view` | a call to your backend through the host — see [A backend](#a-backend)               |

A record is `{ id, collection, data, rev, created_by, updated_by, created_at,
updated_at }`. Hide controls the user cannot use by reading `caps`; the host
enforces them either way.

### Theme

`connect()` writes the host's CSS variables (`--background`, `--foreground`,
`--primary`, `--border`, `--radius`, …) onto `<html>`, toggles the `dark`
class and `color-scheme`, and follows theme changes, so styling with
`var(--background)` makes the app look like pp-hub in either mode. Pass
`applyTheme: false` to do it yourself from `app.context.theme` and
`app.on("theme", …)`.

### Records, realtime, comments, notifications

- **Records belong to the item** the frame was opened on: up to 10 000 per
  item, 64 KiB each. Copying the item copies its records under the same ids
  (so references between records survive); deleting it for good deletes them.
  `seed` records are written as the creator when an item is made.
- **Live updates.** `on("records", …)` receives `{type: "record.upsert",
record}` and `{type: "record.delete", id, collection}` for every write, yours
  included; `on("broadcast", …)` receives `{event, data, from}`. On
  `on("stream", …)` with `state: "reconnected"`, refetch — events may have been
  missed.
- **Comments** hang on `record:<id>` subjects of the item's records:
  `comments.open("record:" + id)` shows pp-hub's discussion panel,
  `comments.counts()` gives the badge numbers. A comment notification links
  back with `subject` and `thread` in the context, and the host opens the
  panel itself.
- **Notifications**: `notify({ to, verb: "assigned", excerpt, subject:
"record:" + id })` tells someone who can open the item; the bell words it
  with the manifest's template and links back to the record.

### A backend

For logic that must not live in the browser — rules the client cannot
bypass, secrets, other systems — declare `"backend": { "url": "https://…" }`
(an administrator can override the address in Admin › Apps › Backend…, and
must generate the secret there before the proxy calls out).
`app.backend.fetch("/sync", { method: "POST", body })` reaches
`<backend url>/sync` through pp-hub with `Authorization: Bearer <token>`,
`X-PP-App`, `X-PP-Node` and `X-PP-User`; the frame gets `{ status, ok,
headers, body, json() }`. The token is an HS256 JWT signed with the app's
backend secret, valid five minutes, with `sub` (the user), `node`, `space`,
`kind`, `caps`, `name`, `email`, `locale` and `aud` = `pp-app:<id>`. Verify it
with `@pp-hub/app-sdk/server`:

```js
import { tokenFromRequest, verifyToken } from "@pp-hub/app-sdk/server";

const claims = verifyToken(tokenFromRequest(req.headers) ?? "", process.env.PP_APP_SECRET, {
  appId: "tasks",
});
// claims.sub, claims.node, claims.caps.edit … — throws TokenError when invalid or expired
```

While the token is valid the backend may call back, with it as the bearer,
into `${claims.iss}/api/apps/<id>/nodes/${claims.node}/` — `records` (GET,
POST), `records/<rid>` (GET, PATCH, DELETE), `broadcast` and `notify` (POST),
bodies as over the bridge. pp-hub re-checks the user's access on every call,
so a viewer's token reads but cannot write. The proxy allows 30 s, 1 MiB up
and 10 MiB down, follows no redirects, and passes no cookies either way; the
backend must be reachable from the pp-hub server.

### Upload, update, remove

In **Admin › Apps** (instance administrators):

- **Upload app** takes the `.zip` or a folder (zipped in the browser, leaving
  out dot-files and `node_modules`). The app arrives **off**: switch it on, and
  its kinds appear under Apps in the sidebar of every space that has not
  switched it off (Settings › Apps).
- **Upload new version…** — or the offer an upload of an installed id makes —
  replaces the files. Records, switches, backend address and secret are kept;
  bump `version` so people can tell.
- **Backend…** sets the address override and generates (rotates) the secret,
  shown once.
- **Remove…** uninstalls; items stay as "not part of this installation", and
  a reinstall brings them back with their data — unless the removal was asked
  to delete the data too.

### What the sandbox allows

- **One page.** The bridge belongs to the page the frame opened. Navigating
  the frame ends it and pp-hub shows "left its page"; route inside the page
  (hash or history routing) instead. A page that has not said `hello` within
  15 s shows "did not start".
- **No network but what you declare.** `connect-src` is `'none'` unless
  `web.connect` lists origins; pp-hub is reached through the SDK, never by
  `fetch`. Scripts, styles, fonts and images load from the package (and the
  CDN allow-list, if an administrator set one).
- **No cookies, no storage.** The frame's origin is opaque: `document.cookie`
  and `localStorage` throw. Keep state in records.
- **No server-side checks on record contents.** Whoever can edit the item can
  write any record; put rules that must hold in your backend.
- **Public files.** The package's files are served to anyone with the URL —
  ship code, not data.
- Popups, downloads and modal dialogs are allowed; a form may run its script
  handlers but never submits (`form-action 'none'`); frames inside the frame
  are not allowed.

## Versioning and compatibility

- `engines.sdk` is the bridge protocol version (1), checked at upload.
- `@pp-hub/app-sdk` is **not yet frozen** and is private to this repository;
  publishing it with a semver promise waits for an app not written by its
  authors.
- The app's own `version` is free-form semver; it is shown, and is what a
  catalog compares.

## Third-party apps and a marketplace

What exists: **packaged apps install at runtime.** A third party ships a
`.zip`, an administrator uploads it in Admin › Apps, and it runs sandboxed with
the item's permissions — no rebuild, no code in the server process.

What does not: a catalog. One — not built — would be a signed JSON index whose
entry is little more than the app's manifest, where to download the zip and its
digest (the same sha256 the server computes, so an installer can check what it
fetched):

```json
{
  "schemaVersion": 1,
  "apps": [
    {
      "id": "tasks",
      "name": "Tasks",
      "version": "1.1.0",
      "description": "…",
      "author": "Acme",
      "license": "MIT",
      "homepage": "https://…",
      "engines": { "sdk": 1, "pp-hub": ">=0.17" },
      "package": { "url": "https://…/tasks-1.1.0.zip", "digest": "<sha256 hex>" }
    }
  ]
}
```

Installing an entry would be the upload Admin › Apps already does, fetched
instead of picked. What a real marketplace still needs, in order: a catalog
host and **signing** (nothing verifies who published a package today; the
digest only names its contents); an `engines.pp-hub` range checked at upload;
and review of what an app may do (its backend sees what its token carries).
