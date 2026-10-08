# Writing an app (extension)

How a pp-hub app is built, wired and shipped. The decision behind it — compile-time
modules behind a public SDK, no sandbox — is in [extensions.md](extensions.md); this page is
the working guide.

An app is **one manifest, a Go package and an npm package**:

```
apps/server/ext/<id>/          Go package (same module as the server; built-ins)
  pp-app.json                  the manifest — canonical copy, go:embed-ed
  <id>.go  handlers.go         New(), Manifest(), Register()
  migrations/0001_init.sql     schema ext_<id>
packages/ext-<id>/             npm workspace package @pp-hub/ext-<id>
  pp-app.json                  byte-identical copy (pp-apps sync makes it)
  package.json  tsconfig.json
  src/index.ts                 default export: the ExtManifest
  src/*-editor.tsx             the full-page editor
```

An app that lives outside this repo has the same two packages in its own repository: a Go
module and an npm package, each carrying `pp-app.json` ([Third-party apps](#third-party-apps-and-a-marketplace)).

## The manifest — `pp-app.json`

```json
{
  "id": "checklist",
  "name": "Checklist",
  "version": "1.0.0",
  "description": "A list of items to tick, live for everyone who has it open.",
  "author": "pp-hub",
  "engines": { "sdk": 1 },
  "nodeKinds": [{ "kind": "checklist.list" }],
  "server": { "package": "github.com/nhatbui/pp-hub/apps/server/ext/checklist" },
  "web": { "package": "@pp-hub/ext-checklist" }
}
```

| Field                 | Rule                                                                                                                  |
| --------------------- | --------------------------------------------------------------------------------------------------------------------- |
| `id`                  | `^[a-z][a-z0-9_]{0,31}$`; names the URL (`/api/v1/ext/<id>`), the schema (`ext_<id>`) and settings                    |
| `name`, `description` | shown on Admin › Apps; `name` is the fallback when the SPA carries no translation                                     |
| `version`             | semver of the app itself                                                                                              |
| `author`              | shown on Admin › Apps; `license` and `homepage` are optional                                                          |
| `engines.sdk`         | the extension API version the app was written for; the host refuses an app whose value is not `extsdk.APIVersion` (1) |
| `nodeKinds[].kind`    | `<id>.<name>`; every kind starts with the app's id; unique across the build                                           |
| `server.package`      | the Go import path of the package that exports `New() extsdk.Ext`                                                     |
| `web.package`         | the npm package name whose default export is the `ExtManifest`                                                        |

Unknown fields are errors. `packages/ext-sdk/pp-app.schema.json` is the JSON Schema; two rules
it cannot say (kinds prefixed by the id, uniqueness across apps) are enforced by `pp-apps check`.

The Go side **parses the embedded JSON** (`extsdk.MustManifest`) and attaches its hooks to the
kinds the JSON declared (`m.Kind("checklist.list").OnCopy = …`); the web side imports the
package's copy for the id and the kind strings. The kind string is written once.

## Server side — `extsdk`

The package imports **only** `apps/server/extsdk` (and libraries): `TestExtImportsOnlySDK` fails
the build on a path through `internal/`, and `TestExtDirsCarryManifest` checks the manifest.

- `Manifest()` — `extsdk.MustManifest(manifestJSON)`, set `Migrations`, attach `OnCreate` / `OnCopy` /
  `ValidSubject` to the declared kinds.
- `Register(h extsdk.Host)` — mount routes on `h.Router()`; put `h.Guard(kind, cap)` in front of every
  route that names a node. Access is `Host.Caps`, never the app's own rule.
- Helpers every handler uses: `extsdk.MustRequest(r)` (the node, caller and caps `Guard` stored),
  `extsdk.Fail(w, err)` (`Conflict` → 409, no rows → 404, else 500 and a log line),
  `extsdk.Conflict(msg)`, `extsdk.Trim(w, v, field, max, required)`, `extsdk.DecodeBody`, `WriteJSON`,
  `WriteProblem`.
- Realtime, notifications, comments on a node's parts, people search and cross-node links come from the
  `Host` ([extensions.md](extensions.md#the-contract)).
- Tables live in `ext_<id>`; qualify names in code, reference `public.nodes(id) ON DELETE CASCADE`
  so a purge takes the rows with it.

## Web side — `@pp-hub/ext-sdk`

A web package may import only `react`, `react-dom`, `@tanstack/react-query`, `@lingui/*`,
`lucide-react`, `sonner`, `@workspace/ui/*`, `@pp-hub/ext-sdk`, relative files and its own
`pp-app.json`. `apps/web/tests/ext-boundary.test.ts` fails on anything else — in particular `@/…`.

The SDK is the app's API for extensions. The app implements `ExtHost` (`apps/web/src/ext/host.ts`)
and installs it at boot; the SDK's exports delegate to it, so extension code never reaches the app:

| Export                                                                                  | What it is                                                              |
| --------------------------------------------------------------------------------------- | ----------------------------------------------------------------------- |
| `defineExtension`, `ExtManifest`, `ExtNodeKind`, `ExtEditorProps`                       | the manifest an extension's `index.ts` default-exports                  |
| `api`, `ApiError`, `connectEventStream`                                                 | the authenticated client and the SSE connector                          |
| `ConfirmDialog`, `LoadingStatus`, `NodeIcon`, `UserAvatar`, `ThreadList`, `createModal` | app components and the dialog helper                                    |
| `useLocale`, `useKeyedState`, `useDebouncedValue`, `useNode`, `useNodeSearch`           | hooks                                                                   |
| `extQueryKey`, `isAppOff`, `extRetry`, `useExtEvents`, `jsonEvent`                      | query keys, the retry policy and the SSE hook with probe-then-reconnect |
| `ExtUnavailable`                                                                        | the "turned off / could not be opened" state                            |
| `Caps`, `Person`, `NodeRef`                                                             | shared wire types                                                       |

Tailwind finds the packages' classes through an `@source` line in `packages/ui/src/styles/globals.css` (`packages/ext-*/src`); an app package outside that path needs its own `@source`. UI strings use Lingui macros like the rest of the web app; the app's catalog scans `packages/ext-*/src`
(`apps/web/lingui.config.ts`), so one catalog still holds them. Per-extension catalogs are phase 2.

## Wiring — `pp-apps`

`pp-apps.json` at the repo root lists the apps in this build. The CLI (`bun run apps <command>`,
source in `tools/pp-apps`) keeps the rest consistent:

| Command                           | Does                                                                                             |
| --------------------------------- | ------------------------------------------------------------------------------------------------ |
| `new <id> [--name "Title"]`       | scaffold both packages from `tools/pp-apps/templates`, register, `gen`                           |
| `gen`                             | write `apps/server/cmd/server/extensions_gen.go` and `apps/web/src/ext/registry.gen.ts`; `sync`  |
| `sync`                            | copy each in-repo `pp-app.json` over its web package's copy                                      |
| `check`                           | validate manifests, copies identical, ids/kinds unique, generated files current, web deps listed |
| `list`                            | show what the build contains                                                                     |
| `add <pkg@ver> --go <module@ver>` | install an external app: `go get`, `bun add`, read its manifest, register, `gen`                 |
| `remove <id>`                     | unregister (files stay), drop the web dependency, `gen`                                          |

`check` also runs as a test (`apps/web/tests/pp-apps-check.test.ts`), so a forgotten `gen` or a
manifest drift fails `vp run web#test`.

Typical new app: `bun run apps new notes --name Notes`, `bun install`, write the migration,
handlers and editor, edit the manifest (description, author), `bun run apps gen`, `bun run apps check`.
Then an administrator switches it on under Admin › Apps.

## Versioning and compatibility

- `engines.sdk` is the contract version. Changing `extsdk`'s or `@pp-hub/ext-sdk`'s public surface in a
  way that breaks an app bumps `APIVersion`; the host refuses an app written for another version at
  startup with the manifest's id in the error.
- The SDK is **not yet frozen** — two apps (kanban, checklist) have been written against it. Publishing
  it with a semver promise waits for a third that was not written by its authors.
- The app's own `version` is free-form semver; it is shown, and is what a catalog compares.

## Third-party apps and a marketplace

What exists: the package shape (a Go module and an npm package, both carrying `pp-app.json`), the
`pp-apps add` command, and the install-time checks (`check`). The build **recompiles** to add an app —
there is no runtime install, no sandbox, and an app runs with the server's privileges. A marketplace
therefore means _a catalog of buildable packages_, not an app store that hot-loads code.

`add` has only been exercised against a fixture: there is no registry to install from yet, so the
network path (`go get`, `bun add`) is untested.

A catalog — not built — would be a signed JSON index of entries like:

```json
{
  "schemaVersion": 1,
  "apps": [
    {
      "id": "notes",
      "name": "Notes",
      "version": "1.2.0",
      "description": "…",
      "author": "Acme",
      "license": "MIT",
      "homepage": "https://…",
      "engines": { "sdk": 1, "pp-hub": ">=0.17" },
      "server": { "module": "github.com/acme/pp-notes", "version": "v1.2.0" },
      "web": { "package": "@acme/pp-notes", "version": "1.2.0" },
      "integrity": { "go": "h1:…", "npm": "sha512-…" }
    }
  ]
}
```

It is the manifest plus where to fetch each half and a checksum; `pp-apps add notes` would resolve
the entry, then do exactly what `add <pkg> --go <module>` does today. What a real marketplace still
needs, in order: a catalog host and signing; an `engines.pp-hub` range checked at `gen`; review of
what an app may do (its migrations run as the server's DB role); per-extension translation
catalogs; and, for untrusted code, the out-of-process model in [extensions.md](extensions.md#later-phase-2).
