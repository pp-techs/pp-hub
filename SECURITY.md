# Security policy

pp-hub holds a company's files and decides who can open them, so a report
about access control, authentication, sharing links or uploads is taken
seriously.

## Reporting a vulnerability

**Do not open a public issue.** Report it privately through GitHub:
[Security → Report a vulnerability](https://github.com/pp-techs/pp-hub/security/advisories/new).

Include what you can reach that you should not, the steps or request that
show it, and the version (`PP_IMAGE_TAG`, or the commit for a `:edge` build).
You will get an acknowledgement, and a fix or a decision, in the advisory
thread; the advisory is published once a fixed release is out.

## Supported versions

Fixes land on `main` and ship in the next release. Only the latest release
(`:latest`, the newest `vX.Y.Z` on the
[releases page](https://github.com/pp-techs/pp-hub/releases)) is supported;
upgrade rather than patch an old one.

## Scope

In scope: this repository — the server, the web app, the deploy scripts and
the container images built from it.

Out of scope: Logto, OnlyOffice, Postgres, SeaweedFS and your reverse proxy
(report those upstream), and a deployment that ignores
[docs/deployment.md](docs/deployment.md) — an exposed Postgres, a reused
`PP_SESSION_KEY`, an OnlyOffice JWT secret left at its default.
