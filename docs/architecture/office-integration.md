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

The backend `handleEditSession` validates the file's MIME type via `isOfficeMime` before
returning an editor URL — only the MIME types in the list above pass.

`application/octet-stream` is deliberately **not** on that list, and used to be. It is what
`fs.PutFile` stores whenever Go's MIME table does not recognise an uploaded file's extension
— `PutFile`'s fallback for anything unfamiliar — so accepting it turned the allowlist into
"any file with an unfamiliar name", which is the opposite of an allowlist. The practical
effect was that an arbitrary blob could be handed to the Document Server to parse. A file
whose MIME really is unknown now gets "file type not supported by editor" instead, which is
the truth.

## WOPI Host Endpoints

Mounted under `/wopi` on a **separate listener** (`PP_WOPI_ADDR`, default `:8081`; `Server.WopiRouter()`):

| Endpoint                                                              | Purpose                                                                                                               |
| --------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------- |
| `GET /wopi/files/{id}`                                                | `CheckFileInfo` — returns `BaseFileName, Size, OwnerId, UserId, UserCanWrite, Version, SupportsUpdate, SupportsLocks` |
| `GET /wopi/files/{id}/contents`                                       | `GetFile` — streams the blob of `current_version_id`; **rank ≥ 1** for rendering in view mode                         |
| `POST /wopi/files/{id}/contents`                                      | `PutFile` — writes new blob, appends `node_version` (which counts the blob reference); **rank 3**                     |
| `POST /wopi/files/{id}` (`X-WOPI-Override: LOCK/UNLOCK/REFRESH_LOCK`) | Lock management against `file_locks`; **rank 3**                                                                      |

The public listener (`:8080`, which also serves the SPA) has **no `/wopi` route**
(`TestWopiIsNotOnThePublicRouter`). `PP_WOPI_SERVER_URL` is `http://server:8081` in compose, and
that value is what becomes the `WOPISrc` the Document Server fetches, so the browser never calls
`/wopi/`. Port 8081 is `expose`d on the compose networks only — not `EXPOSE`d in the image, never
published, never given a domain. Serving it publicly turned the access token from a credential
that mattered only inside the network into an internet-wide bearer token — on the one route group
exempt from both the same-origin check and the rate limiter, because the Document Server sends no
`Origin` and is authorized by the signed token instead. A deployment that runs OnlyOffice outside
the compose network must publish or route **8081** (not 8080) to the Document Server, restricted
to its address, and set `PP_WOPI_SERVER_URL` to that URL (the worker uses it too). See
[deployment.md](../deployment.md#the-wopi-listener).

## WOPI Access Token

- HMAC-signed, TTL ~10h, payload: `user_id`, `node_id`, `perm` — the rank, on the scale
  `rankFromCaps` produces: 3 = edit, 2 = download, 1 = view, 0 = no access.
- Issued by the server when the web app opens an editor.
- OnlyOffice's callbacks authenticate with this token — no session cookies cross the service boundary.

## Editor URL (OnlyOffice 9.x and 10)

- Compose sets `WOPI_ENABLED=true`; OnlyOffice serves `/hosting/discovery` and the editor at `/hosting/wopi/{documentType}/{mode}` (`word|cell|slide`, mapped from the file extension; `edit` or `view`, see below).
- The browser loads the editor by **POSTing a form** into an iframe with `access_token` and `access_token_ttl` in the body — 9.x does not accept the token as a query param on this route.
- `PP_WOPI_SERVER_URL` must be reachable **from the OnlyOffice container** and, since 10.0, **named by an ipfilter rule** (`* allowed` no longer counts; without one the frame shows only "Access forbidden"). Compose writes that rule in the `onlyoffice` entrypoint: `server` in production, `wopi-proxy` in development. Development cannot point at `host.docker.internal` — OrbStack resolves it to `0.250.250.254`, which OnlyOffice 10 refuses outright for WOPI — so `wopi-proxy` (socat) forwards to the host server's WOPI listener. See [deployment.md](../deployment.md#onlyoffice-10-name-the-wopi-host).
- Saving: a clean close writes back within seconds; after an unclean disconnect the Document Server holds the session for `expire.presence` = 300 s and `documentsCron` runs every 2 minutes, so `PutFile` (a new version) arrives roughly 5–10 minutes later.

## Read-Only Sessions

`POST /nodes/{id}/edit-session` takes **`CapView`**. Reading an office document
is viewing it: OnlyOffice opens in read-only mode (`/view`), and the Document
Server fetches the document so the browser can render it.

What the caller holds decides the _session_ capability:

| Caller        | Token `perm` | Editor URL | `CheckFileInfo.UserCanWrite` | `PutFile` / lock ops | `DisablePrint` / `DisableExport` |
| :------------ | :----------- | :--------- | :--------------------------- | :------------------- | :------------------------------- |
| `CapEdit`     | 3            | `/edit`    | `true`                       | allowed              | `false`                          |
| `CapDownload` | 2            | `/view`    | `false`                      | **403**              | `false`                          |
| `CapView`     | 1            | `/view`    | `false`                      | **403**              | `true`                           |

A read-only session is rank **1** (view-only) or rank **2** (view + download).
`GetFile` requires rank ≥ 1 so OnlyOffice can fetch the file bytes to render
the view. When rank is 1 (view without download), `CheckFileInfo` sets
`DisablePrint: true, DisableExport: true, DisableCopy: true` so the viewer
cannot export or save a copy from the OnlyOffice UI.

**Shared links.** An anonymous visitor on `/s/{token}` views office files
through `GET /s/{token}/edit-session?node=<id>`, which mints a rank 1 (or 2 if
the link allows download) read-only session. The token is tied to the link's
creator, so revoking the creator's access revokes the link's editor session too.

The rank is signed into the token, so the write refusal does not depend on
OnlyOffice honouring `UserCanWrite`. The response carries `can_write`, and the
editor page's subtitle says "read-only" rather than "saved by OnlyOffice" when
it is false.

A **machine token** is the exception to the rank check on `GetFile`:
`wopi.SystemSubject` is the preview worker's conversion fetch, which acts for
nobody, so there is no capability to resolve. It is minted by this deployment,
scoped to one node, read-only and five minutes long, and the signing key is
its authorization. It is refused at `CheckFileInfo`, which only a person's
session has any business calling.

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
OnlyOffice container, `-8` is a signature the Document Server would not accept.

## JWT

The Document Server has **no access control of its own**. It is not a service
that authenticates callers and authorizes actions; it does what it is asked.
With `JWT_ENABLED=false`, anything that can open a TCP connection to it can
ask it to convert — and since the conversion API takes a URL, that is a
request to fetch an arbitrary address from inside the network, which is
exactly what `ALLOW_PRIVATE_IP_ADDRESS=true` then permits.

So both compose stacks set `JWT_ENABLED=true` with a shared
`PP_ONLYOFFICE_JWT_SECRET`, and the worker signs every conversion request:
`preview.signOnlyOfficeJWT` puts an HS256 token over the request body in its
`token` field and the same request, wrapped in `payload`, in an
`Authorization: Bearer` header — the documentation is inconsistent about which
endpoints honour which, and sending both costs nothing. Unsigned or
wrongly-signed requests come back `-8` without the fetch being attempted.

The signing helper is hand-rolled rather than a dependency, and that is only
defensible because it **signs and never verifies**: algorithm confusion and
`alg: none` are verification bugs. Anything here that ever needs to _check_ a
JWT should take the dependency instead.

**JWT does not apply to WOPI.** The editor launch, `CheckFileInfo`, `GetFile`,
`PutFile` and the lock operations are authorized by the WOPI access token
described above, not by a JWT, and turning `JWT_ENABLED` on leaves all of them
working — verified against 10.0.0.1. The historical claim that enabling JWT
"breaks the WOPI integration" was only ever true of the unsigned conversion
call.

What is deliberately **not** built is WOPI proof-key verification, the
WOPI-native way for a host to confirm a request really came from its Document
Server. The access token already binds one node, one subject and one rank, and
that rank is re-resolved against live access on every call, so a replayed
token — the browser holds one too, in the launch form — yields nothing its
holder cannot already get through `/api/v1`.

See [preview-pipeline.md](preview-pipeline.md).

## Resource Note

OnlyOffice Document Server needs roughly 2–4 GB RAM. It is the heaviest service in the compose stack; acceptable for the self-host target, documented for operators.
