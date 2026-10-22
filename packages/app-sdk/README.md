# @pp-hub/app-sdk

The SDK of pp-hub **packaged apps** — extensions an instance admin uploads as a `.zip` in
Admin › Apps. The app's UI runs in a sandboxed frame and talks to pp-hub only through this SDK.

- `@pp-hub/app-sdk` (`src/index.js`, types `src/index.d.ts`): the frame side of the bridge. A plain
  ES module with no dependencies.
- `@pp-hub/app-sdk/server` (`src/server.js`): verify the token pp-hub sends your own backend.
- `@pp-hub/app-sdk/pp-app.schema.json`: the JSON Schema of the packaged `pp-app.json`.

## Without a build step

Copy `src/index.js` next to your page as `app-sdk.js` (and `src/index.d.ts` as `app-sdk.d.ts` for
editor types), then:

```html
<script type="module">
  import { connect } from "./app-sdk.js";

  const app = await connect();
  const { caps, user } = app.context;
  const items = await app.records.list("items");
  if (caps.edit) await app.records.create("items", { text: `Hello from ${user.name}` });
  app.on("records", (e) => console.log(e.type));
  app.on("stream", (e) => e.state === "reconnected" && location.reload());
</script>
```

`bun run apps init <dir>` scaffolds a complete app this way and copies the SDK in;
`bun run apps pack <dir>` validates it and writes the zip to upload. See `examples/apps/tasks`.

## With a bundler

```ts
import { connect, AppError } from "@pp-hub/app-sdk";

const app = await connect({ applyTheme: true, timeoutMs: 15000 });
try {
  await app.records.update(id, data, { ifRev: rev });
} catch (e) {
  if (e instanceof AppError && e.status === 409) reload();
}
```

Build with relative asset URLs — Vite: `base: "./"` — and point `web.entry` at the built
`index.html` (for example `dist/index.html`); `pack` then ships only that directory plus the
manifest and icons.

Calling `connect()` from a React effect (or any time after page load) is fine: the SDK catches
the host's handshake as soon as it is imported, and every `connect()` call returns the same
connection.

The theme: `connect()` writes the host's CSS variables (`--background`, `--foreground`,
`--primary`, `--border`, `--radius`, …) onto `<html>`, toggles the `dark` class and
`color-scheme`, and follows theme changes. Pass `applyTheme: false` to do it yourself from
`app.context.theme` and `app.on("theme", …)`.

## Your own backend

With `"backend": { "url": "https://tasks.example.com/pp" }` in the manifest,
`app.backend.fetch("/sync", { method: "POST", body })` reaches
`https://tasks.example.com/pp/sync` through pp-hub, with `Authorization: Bearer <token>`: an HS256
JWT signed with the app's backend secret (generated in Admin › Apps), valid for 5 minutes.

```js
import http from "node:http";
import { tokenFromRequest, verifyToken } from "@pp-hub/app-sdk/server";

const SECRET = process.env.PP_APP_SECRET; // as shown in Admin › Apps

http
  .createServer((req, res) => {
    let claims;
    try {
      claims = verifyToken(tokenFromRequest(req.headers) ?? "", SECRET, { appId: "tasks" });
    } catch (e) {
      res.writeHead(401).end(e.message);
      return;
    }
    // claims.sub (user id), claims.node, claims.space, claims.caps.edit, claims.name, claims.locale…
    res.writeHead(200, { "content-type": "application/json" });
    res.end(JSON.stringify({ hello: claims.name }));
  })
  .listen(8080);
```

The same token authorizes calls back into pp-hub while it is valid:
`${claims.iss}/api/apps/<id>/nodes/${claims.node}/records` (GET, POST),
`…/records/<rid>` (GET, PATCH, DELETE), `…/broadcast` and `…/notify` (POST).

## What the sandbox allows

- **One page.** The frame gets its bridge once, for the page it was opened on. Navigating the frame
  (a link, `location = …`) ends the bridge and pp-hub shows "left its page"; route inside the page
  (hash or history routing) instead.
- **No network but what you declare.** The page's `connect-src` is `'none'` unless the manifest
  lists origins in `web.connect`; pp-hub itself is reached through the bridge, never by `fetch`.
  Scripts, styles, fonts and images load from the package's own files (and the instance's
  published-site CDN allow-list, when an administrator set one).
- **No cookies, no storage.** The frame has an opaque origin: `document.cookie` and
  `localStorage` throw. Keep state in records.
- **Data is per item.** Records belong to the item (node) the frame was opened on; up to 10 000 per
  item, 64 KiB each, in collections named `^[a-z][a-z0-9_]{0,31}$`. Whoever can open the item can
  read them; whoever can edit it can write them. Copying the item copies its records, ids
  included; deleting it for good deletes them.
- Comments hang on `record:<id>` subjects; `app.comments.open("record:" + id)` shows pp-hub's own
  discussion panel, and `notify({ subject: "record:" + id })` links the bell to it.
