# Changesets

Every user-visible change ships with a changeset: `bun run changeset`, pick `@pp-hub/release`
(the package that carries the product version) and the bump, then write the release note.
The note is what lands in the root `CHANGELOG.md` and the GitHub Release, so write it for
the person deploying pp-hub, not for the reviewer.

See [CONTRIBUTING.md](../CONTRIBUTING.md#releasing).
