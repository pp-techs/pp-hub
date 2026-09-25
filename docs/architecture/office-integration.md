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

Mounted under `/wopi`:

| Endpoint                                                              | Purpose                                                                                                               |
| --------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------- |
| `GET /wopi/files/{id}`                                                | `CheckFileInfo` — returns `BaseFileName, Size, OwnerId, UserId, UserCanWrite, Version, SupportsUpdate, SupportsLocks` |
| `GET /wopi/files/{id}/contents`                                       | `GetFile` — streams the blob of `current_version_id`; **rank ≥ 2** for a user token                                   |
| `POST /wopi/files/{id}/contents`                                      | `PutFile` — writes new blob, appends `node_version` (which counts the blob reference); **rank 3**                     |
| `POST /wopi/files/{id}` (`X-WOPI-Override: LOCK/UNLOCK/REFRESH_LOCK`) | Lock management against `file_locks`; **rank 3**                                                                      |

Those routes are **not** published by the `web` container. `PP_WOPI_SERVER_URL` is
`http://server:8080` in compose, and that value is what becomes the `WOPISrc` the Document
Server fetches, so the browser never calls `/wopi/` and nginx no longer proxies it.
Publishing it turned the access token from a credential that mattered only inside the network
into an internet-wide bearer token — on the one route group exempt from both the same-origin
check and the rate limiter, because the Document Server sends no `Origin` and is authorized by
the signed token instead. A deployment that runs OnlyOffice outside the compose network must
set `PP_WOPI_SERVER_URL` to a public URL **and** re-add a `location /wopi/` in
`apps/web/nginx.conf`, ideally restricted to the Document Server's address. See
[deployment.md](../deployment.md).

## WOPI Access Token

- HMAC-signed, TTL ~10h, payload: `user_id`, `node_id`, `perm` — the rank, on the scale
  `rankFromCaps` produces: 3 = edit, 2 = download, 1 = view, 0 = no access.
- Issued by the server when the web app opens an editor.
- OnlyOffice's callbacks authenticate with this token — no session cookies cross the service boundary.

## Editor URL (OnlyOffice 9.x)

- Compose sets `WOPI_ENABLED=true`; OnlyOffice serves `/hosting/discovery` and the editor at `/hosting/wopi/{documentType}/{mode}` (`word|cell|slide`, mapped from the file extension; `edit` or `view`, see below).
- The browser loads the editor by **POSTing a form** into an iframe with `access_token` and `access_token_ttl` in the body — 9.x does not accept the token as a query param on this route.
- `PP_WOPI_SERVER_URL` must be reachable **from the OnlyOffice container** — on macOS/Windows Docker Desktop use `http://host.docker.internal:8080`, not `localhost`.

## Read-Only Sessions

`POST /nodes/{id}/edit-session` takes **`CapDownload`**, not `CapView` and not
`CapEdit`. Opening a document in OnlyOffice means the Document Server fetches
the whole file and the browser renders it, so there is no version of this that
is "viewing without downloading" — the bytes leave the server either way.

It used to take `CapView`, on the argument that OnlyOffice is the only
component able to render an office document at all (the preview worker
produces a 256px thumbnail of the first page and nothing more), so demanding
more would leave a reader with no way to look at a `.docx` they were
legitimately shared. That argument does not survive contact with `GetFile`:
the session it minted could fetch the complete file, so it was a route around
the no-download half of a grant — mint a token with `CapView`, let the editor
fetch, keep the document — while `GET /nodes/{id}/content` refused the same
person for the same bytes.

The cost is real and accepted: **a reader granted view-without-download
cannot open an office document at all, only its thumbnail.** That is the
honest consequence of the grant they were given, and it is the same line
[permissions.md](permissions.md) draws for `/content` and
`/preview?kind=text`.

What the caller holds still decides the _session_, not whether there is one:

| Caller        | Token `perm` | Editor URL | `CheckFileInfo.UserCanWrite` | `PutFile` / lock ops |
| :------------ | :----------- | :--------- | :--------------------------- | :------------------- |
| `CapEdit`     | 3            | `/edit`    | `true`                       | allowed              |
| `CapDownload` | 2            | `/view`    | `false`                      | **403**              |
| `CapView`     | —            | —          | —                            | no session is minted |

A read-only session is rank **2**, not rank 1. `GetFile` requires rank ≥ 2 for
a user token — handing over the complete file is a download however the editor
frames it — so rank 1 would mint a session that cannot fetch its own document.
Rank 1 is still meaningful on the scale `rankFromCaps` produces; it simply
cannot open an editor.

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
working — verified against 9.4.0. The historical claim that enabling JWT
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
