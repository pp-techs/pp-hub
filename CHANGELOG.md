# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Versions up to and including 0.14.1 predate this changelog; see the
[GitHub Releases](https://github.com/pp-techs/pp-hub/releases) and
[docs/SUMMARY.md](docs/SUMMARY.md) for what shipped in them.

<!-- `bun run version-packages` adds each release's section from its changesets; edit it in the Version Packages PR. -->

## [0.15.0] — 2026-10-07

**Server administration redesigned.** `/admin` now has its own sidebar (Users, Security log, Instance settings) instead of a top bar. Users get icon stat tiles, search with a result count, and a `⋯` menu for admin and suspend actions. The security log filters sit in a toolbar with a clear-filters button. Instance settings are grouped into cards with a source badge and Reset on each row, and a sticky save bar appears only when something changed. A non-admin opening an admin page is now redirected to the drive.
