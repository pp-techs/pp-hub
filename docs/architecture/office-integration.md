# Office Integration (WOPI + OnlyOffice)

## Supported File Types

OnlyOffice Document Server renders and edits the following formats natively via WOPI:

| Format family   | Extensions                               | MIME type (registered in `preview.init`)                                         |
| --------------- | ---------------------------------------- | -------------------------------------------------------------------------------- |
| Word processing | `.docx`, `.doc`, `.odt`, `.rtf`, `.epub` | `application/rtf`, `application/epub+zip` registered via `mime.AddExtensionType` |
| Spreadsheets    | `.xlsx`, `.xls`, `.csv`, `.ods`          | —                                                                                |
| Presentations   | `.pptx`, `.ppt`, `.odp`                  | —                                                                                |

PDF goes through the preview pipeline rather than OnlyOffice — Community edition does not
support PDF editing. `.epub` and `.rtf` open in the editor and have no inline preview; see
[preview-pipeline.md](preview-pipeline.md).

The backend `handleEditSession` validates the file's MIME type via `isOfficeMime` before returning an editor URL — only known office MIME types pass.

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

- Compose sets `WOPI_ENABLED=true`; OnlyOffice serves `/hosting/discovery` and the editor at `/hosting/wopi/{documentType}/{mode}` (`word|cell|slide`, mapped from the file extension; `edit` or `view`, see below).
- The browser loads the editor by **POSTing a form** into an iframe with `access_token` and `access_token_ttl` in the body — 9.x does not accept the token as a query param on this route.
- `PP_WOPI_SERVER_URL` must be reachable **from the OnlyOffice container** — on macOS/Windows Docker Desktop use `http://host.docker.internal:8080`, not `localhost`.

## Read-Only Sessions

`POST /nodes/{id}/edit-session` takes **`CapView`**, not `CapEdit`. OnlyOffice
is the only component that can render an office document at all — the preview
worker produces a 256px thumbnail of the first page and nothing more — so
demanding edit would leave a reader with no way to look at a `.docx` they were
legitimately shared.

What the caller holds decides the _session_, not whether there is one:

| Caller    | Token `perm` | Editor URL | `CheckFileInfo.UserCanWrite` | `PutFile` / lock ops |
| :-------- | :----------- | :--------- | :--------------------------- | :------------------- |
| `CapEdit` | 3            | `/edit`    | `true`                       | allowed              |
| `CapView` | 1            | `/view`    | `false`                      | **403**              |

The rank is signed into the token, so the write refusal does not depend on
OnlyOffice honouring `UserCanWrite`. The response carries `can_write`, and the
editor page's subtitle says "read-only" rather than "saved by OnlyOffice" when
it is false.

**The rank in the token is a ceiling, not an authority.** It was resolved when
the editor opened, and the token lives ten hours; trusting it alone meant a
revoked grant, a departure from the space or a suspended account did not reach
an open editor until the token expired, while `/api/v1` refused the same
person on the next request. `wopiClaims` re-resolves on every WOPI call —
`share.EffectiveCapsForNode` collapsed onto the same scale by `rankFromCaps`,
with the lower of the two winning — so the table above describes the caller's
access _now_, not when they clicked Open. An edit grant withdrawn mid-session
downgrades to the second row rather than ending the session, because
`CheckFileInfo` reports the live value and OnlyOffice switches itself to
read-only. A node in the trash refuses `PutFile` outright: a version committed
into a tombstone reappears on restore as content nobody wrote there.

## Co-Editing

- Same file → same WOPI `FileId` → OnlyOffice merges concurrent edits in realtime.
- Save produces one `PutFile` with merged content; a `Version` mismatch → HTTP 409 → OnlyOffice reloads/re-merges.

## Conversion API

`cmd/worker` uses `POST {PP_ONLYOFFICE_INTERNAL_URL}/converter` to turn office
documents into PDF, then rasterizes the first page into a thumbnail. Request:
`{async:false, url, filetype, outputtype:"pdf", key, title}` with
`Accept: application/json` — without that header the service answers in XML.

The service **fetches the document by URL** rather than accepting an upload, so
the worker signs a 5-minute view-only WOPI token and hands over
`{PP_WOPI_SERVER_URL}/wopi/files/{id}/contents?access_token=…`. That means
`PP_SESSION_KEY` must be the same in the worker and the API server: it is the
master secret both processes derive the WOPI signing subkey from
(`config.DeriveKey(wopi.KeyPurpose)`), so the worker signs and the server
verifies with the same key without either holding the master for that purpose.
The worker refuses to register the office renderer when the key was generated
rather than supplied.

Failures come back as HTTP 200 with a negative `error` code in the body, so the
status alone is not enough to tell success from failure. `preview.convertError`
translates the codes — `-4` is the source URL being unreachable from the
OnlyOffice container, `-8` is JWT being enabled while this request is unsigned.

**JWT signing is not implemented**, here or anywhere else in the integration;
the compose stack sets `JWT_ENABLED=false`.

See [preview-pipeline.md](preview-pipeline.md).

## Resource Note

OnlyOffice Document Server needs roughly 2–4 GB RAM. It is the heaviest service in the compose stack; acceptable for the self-host target, documented for operators.
