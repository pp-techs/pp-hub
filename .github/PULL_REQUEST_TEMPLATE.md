<!--
- Use a meaningful title: type(scope): description
- Add or edit tests to reflect the change
- Update docs/ if behavior, schema or deployment changed
- Add a changeset for anything an operator or user would notice: bun run changeset
-->

## Description

<!-- What does this fix or add, and why? -->

## Type of change

- [ ] **Bug fix** (non-breaking change that fixes an issue)
- [ ] **New feature** (non-breaking change that adds functionality)
- [ ] **Breaking change** (needs a migration or operator action on upgrade)
- [ ] **Documentation** (docs only)
- [ ] **Refactor** (no functional change)
- [ ] **Build / CI / deploy**

## Areas affected

- [ ] `apps/web`
- [ ] `apps/server` (API, worker, migrations)
- [ ] `deploy/` (compose, Logto, backup)
- [ ] `docs/`

## Checklist

- [ ] `vp check` passes
- [ ] `vp test` passes, and server tests with `PP_TEST_DATABASE_URL` if SQL or permissions changed
- [ ] `vp run web#i18n:extract` run if UI strings changed
- [ ] A changeset is added in `.changeset/` (or the change is not user-visible)
- [ ] `docs/` updated if behavior changed
- [ ] PR title follows [Conventional Commits](https://www.conventionalcommits.org/)

## Upgrade notes

<!-- New migration, env var, compose change or manual step for whoever deploys this? -->
