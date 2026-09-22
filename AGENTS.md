<!-- RULES -->

- Use `docs/` as source of context, documentation
- Update `docs/` when having break/changes to current
- Use better-modal when writing a new modal/dialog and use it. Also shadcn skill for latest component documentation
- When necessary to self-test, run the dev environement and use browser to test base on the available tools for open browser
- Use `vp` for all tooling: `vp check` (format + lint + typecheck), `vp lint`, `vp test`, `vp dev`, `vp run <script>` for package.json scripts. Lint config lives in the root `vite.config.ts` `lint` block
- `@shadcn/lint` is registered as an Oxlint JS plugin for design-system rules; add them under `lint.rules` with the `shadcn/` prefix (e.g. `shadcn/no-restyle`). After UI changes, run `vp lint` and fix all errors
- When modifying UI strings, run `bun run i18n:extract` (or `vp run web#i18n:extract`) to keep both the `.po` catalogs and `.pot` template in sync

<!-- RULES END-->
