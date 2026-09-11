# Office Integration (WOPI + OnlyOffice)

## Approach

OnlyOffice Document Server (community edition) runs as a bundled compose service and owns all realtime collaboration inside a document. The Go server implements the WOPI **host** side. We deliberately do not build a CRDT/OT engine.

## WOPI Host Endpoints

Mounted under `/wopi`:

| Endpoint                                                              | Purpose                                                                                                               |
| --------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------- |
| `GET /wopi/files/{id}`                                                | `CheckFileInfo` — returns `BaseFileName, Size, OwnerId, UserId, UserCanWrite, Version, SupportsUpdate, SupportsLocks` |
| `GET /wopi/files/{id}/contents`                                       | `GetFile` — streams the blob of `current_version_id`                                                                  |
| `POST /wopi/files/{id}/contents`                                      | `PutFile` — writes new blob, appends `node_version`, updates `ref_count`                                              |
| `POST /wopi/files/{id}` (`X-WOPI-Override: LOCK/UNLOCK/REFRESH_LOCK`) | Lock management against `file_locks`                                                                                  |

## WOPI Access Token

- HMAC-signed, TTL ~10h, payload: `user_id`, `node_id`, `perm`.
- Issued by the server when the web app opens an editor.
- OnlyOffice's callbacks authenticate with this token — no session cookies cross the service boundary.

## Editor URL (OnlyOffice 9.x)

- Compose sets `WOPI_ENABLED=true`; OnlyOffice serves `/hosting/discovery` and the editor at `/hosting/wopi/{documentType}/edit` (`word|cell|slide`, mapped from the file extension).
- The browser loads the editor by **POSTing a form** into an iframe with `access_token` and `access_token_ttl` in the body — 9.x does not accept the token as a query param on this route.
- `PP_WOPI_SERVER_URL` must be reachable **from the OnlyOffice container** — on macOS/Windows Docker Desktop use `http://host.docker.internal:8080`, not `localhost`.

## Co-Editing

- Same file → same WOPI `FileId` → OnlyOffice merges concurrent edits in realtime.
- Save produces one `PutFile` with merged content; a `Version` mismatch → HTTP 409 → OnlyOffice reloads/re-merges.

## Conversion API (optional)

Preview generation may use OnlyOffice's conversion endpoint (office → PDF) as one `preview` job kind; not required for MVP previews of PDF/image/video/text.

## Resource Note

OnlyOffice Document Server needs roughly 2–4 GB RAM. It is the heaviest service in the compose stack; acceptable for the self-host target, documented for operators.
