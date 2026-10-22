---
"@pp-hub/release": major
---

Kanban and Checklist are no longer built in: they are now ordinary uploadable apps, and pp-hub ships with no apps installed. **Operator action:** after upgrading, an admin uploads `kanban-2.0.0.zip` and `checklist-2.0.0.zip` (build them with `bun run apps:build`; they land in `examples/apps/kanban/dist-pack/` and `examples/apps/checklist/dist-pack/`) in Admin › Apps. Until then, existing boards and checklists stay in the drive but open as "not part of this installation"; nothing is lost. The on/off switches are kept, so an app that was on comes back on with its upload; a fresh install starts with both off, like every app.

Migration `0032` moves every board (columns, labels, cards with their assignees, labels, links and due dates) and every checklist into the app record store under the same ids, keeps card comment threads and notification links, and drops the old `ext_kanban` and `ext_checklist` schemas. It is applied automatically.
