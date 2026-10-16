# Contributing to pp-hub

Start with [docs/SUMMARY.md](docs/SUMMARY.md) for the architecture and
[docs/code-standard](docs/code-standard) for the conventions. The README has
the local setup.

## Workflow

1. Branch from `main`; keep the change to one concern.
2. Run the checks that CI runs:

   | Task                           | Command                                                                                                     |
   | ------------------------------ | ----------------------------------------------------------------------------------------------------------- |
   | Format, lint, type-check       | `vp check` (`vp check --fix` to fix)                                                                        |
   | Web tests                      | `vp test`                                                                                                   |
   | Server tests (with a database) | `cd apps/server && PP_TEST_DATABASE_URL=postgres://pphub:pphub@localhost:5433/pphub go test -p 1 ./...`     |
   | Server vet and sqlc            | `cd apps/server && go vet ./...`, and `sqlc generate` after touching a migration or query                   |
   | Message catalogs               | `vp run web#i18n:extract` after any web change (they hold source line numbers); the pre-commit hook runs it |

3. Add a changeset if the change is visible to someone running or using pp-hub
   (see below).
4. Open a pull request titled `type(scope): description`
   ([Conventional Commits](https://www.conventionalcommits.org/)). CI must be green.

## Changesets

A changeset is the release note for a change. Add one with:

```bash
bun run changeset
```

pp-hub ships a single product version, carried by the private `@pp-hub/release`
workspace package: select it, pick the bump, and write the note for the person
deploying pp-hub rather than for the reviewer.

| Bump    | When                                                                                    |
| ------- | --------------------------------------------------------------------------------------- |
| `major` | An upgrade needs operator action: a removed or renamed env var, a manual migration step |
| `minor` | A new capability, or a schema migration that is applied automatically                   |
| `patch` | A fix, a UI polish, a dependency bump                                                   |

Refactors, tests, docs and CI changes do not need one. Commit the generated
`.changeset/*.md` with the change; several changesets may land before a release.

## Releasing

Releases are cut from `.github/workflows/release.yml`, which runs on every push
to `main`:

1. While changesets are pending, the workflow opens or updates a
   **"chore(release): version packages"** PR. Its commit is
   `bun run version-packages`: it bumps the product version (root `package.json`
   and `@pp-hub/release`), writes the new section of `CHANGELOG.md` from the
   changesets and refreshes `bun.lock`. Edit the changelog wording in that PR if
   needed.
2. Merging the PR is the release. The workflow sees a version without a git tag,
   re-runs every CI check on that commit, prepares the release notes, then waits
   for approval on the `production-release` environment.
3. Once approved, it builds and pushes `ghcr.io/pp-techs/pp-hub-{server,worker}`
   as `:X.Y.Z`, `:X.Y` and `:latest` (a prerelease `X.Y.Z-rc.1` gets only its own tag),
   then tags `vX.Y.Z` and creates the GitHub Release from the changelog section.
   The tag is created last, so a tag always means the images exist.

A failed run is retried by re-running the failed jobs. Never bump versions or
push release tags by hand: nothing listens for a tag. To deploy, set
`PP_IMAGE_TAG=X.Y.Z` (see [docs/deployment.md](docs/deployment.md#releases)).

Every push to `main` also publishes `:edge` and `:sha-<sha>` from `edge.yml`,
for staging; do not pin production to them.

### Repository settings the flow relies on

- Environment **`production-release`** with required reviewers: the human gate
  before images are pushed. Required reviewers are available on a public
  repository on every plan. Without them the environment still exists and the
  job passes at once, so merging the Version Packages PR would be the only gate.
- Actions → General → _Allow GitHub Actions to create and approve pull requests_
  (the Version Packages PR is opened with `GITHUB_TOKEN`). That PR is therefore
  created without CI running on it; the release workflow re-runs CI on the merged
  commit before shipping. If `main` requires status checks, either use a
  maintainer override for that PR or switch the workflow to a PAT or GitHub App token.
- Workflow permissions default to read-only; each job asks for what it needs.
- Actions → General → _Fork pull request workflows_: require approval for
  **all outside collaborators**, so a first-time contributor's workflow does not
  run until a maintainer has read the diff. Workflows from forks run with a
  read-only token and no secrets either way, which is why nothing that publishes
  (`release.yml`, `edge.yml`) is triggered by `pull_request`.
- A ruleset on `main`: pull request required, the `CI` and `i18n` checks
  required, no force pushes. `.github/CODEOWNERS` covers the files that can
  change what is published (workflows, changesets, scripts).
- Code security: Dependabot alerts and security updates (version updates are in
  `dependabot.yml`), secret scanning with push protection, and private
  vulnerability reporting, which [SECURITY.md](SECURITY.md) points reporters to.
- Packages: `ghcr.io/pp-techs/pp-hub-{server,worker}` are created private by the
  first push. Set each to **public** in its package settings once, so a
  deployment can pull without a token.

## Security

Report a vulnerability privately, as [SECURITY.md](SECURITY.md) describes; do
not open an issue or a pull request that discloses it.

## License

pp-hub is licensed under the [GNU Affero General Public License v3.0](LICENSE).
By opening a pull request you agree that your contribution is licensed under
the same terms. Run a modified pp-hub as a service for other people, and the
AGPL requires you to offer them its source.
