---
"@pp-hub/release": minor
---

**Mount your drive over WebDAV.** People can mount their spaces at `${PP_EXTERNAL_URL}/dav/` in Finder, Windows Explorer or rclone (recommended for coding agents, with `--vfs-cache-mode full`) and work on files as files. A mount is another view of the same nodes, versions and grants: every write goes through the API as the person, a save over a file becomes a new version, a write that raced someone else's lands beside the file as a conflicted copy, and a delete moves to the trash. It signs in with a new kind of personal token, minted in **Settings › Security › Mount as a drive** (a WebDAV token is not accepted at `/mcp/`, nor an MCP token at `/dav`). Administrators turn it on in **Admin › Settings › WebDAV** (`webdav_enabled`, off by default). The bundled nginx proxies `/dav` with no body limit, no buffering and one-hour timeouts; a proxy you put in front of pp-hub must allow the same, because a WebDAV upload is a single request.
