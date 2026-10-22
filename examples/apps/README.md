# Example packaged apps

Packaged apps are pp-hub extensions an instance admin uploads as a `.zip`; they work at once, with
no rebuild. Each folder here is one app. pp-hub installs none of them by itself.

- [`tasks`](tasks/): a shared task list — tick, edit, assign (with a notification) and comment on
  tasks; read-only for viewers without edit rights. Written without a build step: a `pp-app.json`
  manifest, plain HTML/CSS/JS, and a copy of the bridge SDK (`app-sdk.js`, from `packages/app-sdk`).
- [`kanban`](kanban/): project boards — columns, cards, labels, assignees, due dates, links and
  card comments. A Vite + React + Lingui app on `@workspace/ui` and `@pp-hub/app-sdk`.
- [`checklist`](checklist/): a list of items to tick, live for everyone who has it open. Built
  like `kanban`.

`kanban` and `checklist` were built into pp-hub before; migration `0032` moved their data into the
record store, so an install upgraded from then needs both zips uploaded for its existing boards and
checklists to open again.

## Pack and upload

From the repo root:

```sh
bun run apps pack examples/apps/tasks   # writes examples/apps/tasks-<version>.zip
bun run apps:build                      # builds and packs kanban and checklist into <app>/dist-pack/
```

`pack` checks the manifest and the files it references, refuses root-absolute asset paths, and
writes `examples/apps/tasks-<version>.zip` (`-o <file.zip>` to choose). Upload that zip in pp-hub
under **Admin › Apps**, then create a "Task list" in any space. To ship a change, bump `version` in
`pp-app.json`, pack again and upload the new zip as an upgrade; the records are kept.

## Start your own

```sh
bun run apps init path/to/my_app --name "My App"
```

scaffolds the same layout with a minimal list app to edit. If `packages/app-sdk/src/index.js`
changes, copy it over the app's `app-sdk.js` (and `index.d.ts` over `app-sdk.d.ts`).
