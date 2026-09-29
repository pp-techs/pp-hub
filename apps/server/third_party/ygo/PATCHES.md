# reearth/ygo v1.50.0, patched for pp-hub

A copy of `github.com/reearth/ygo@v1.50.0` holding only the packages
`provider/websocket` and `crdt` need (no tests, examples, cmd or mobile). It
is wired in by the `replace` in `apps/server/go.mod`.

## Patches

All in `provider/websocket`; every patched spot is marked `pp-hub patch`.

1. **Connection origin.** `ConnectionConfig.Origin any` plus
   `ConnectionOrigin(txnOrigin any) (any, bool)`. `Authorize` returns an
   application value for the connection; ygo keeps its unexported `*peer` as
   the transaction origin and `ConnectionOrigin` maps that back to the value.
   pp-hub uses it to attribute a flushed version to the user whose updates it
   holds (see `docs/architecture/block-documents.md`). Touches `server.go`
   (`ConnectionConfig`, `ConnectionOrigin`, `ServeHTTP`) and `peer.go` (one
   field).
2. **SyncStatus acknowledgements.** Under `HocuspocusFraming`, every
   SyncStep2 or Update a peer sends is answered with Hocuspocus's SyncStatus
   message (tag 8) the way `@hocuspocus/server` answers it: 1 once applied; for
   a read-only peer 0 for an Update, and for a SyncStep2 1 when it inserts
   nothing the room lacks, else 0. The provider counts its unsynced changes
   down on each 1, so without it `hasUnsyncedChanges` never clears. `peer.go`
   (`handleMessage`, `dropReadOnlyWrite`, `addsNothing`, `sendSyncStatus`).
3. **Server Ping.** `Server.PingInterval`: under `HocuspocusFraming` each peer
   gets Hocuspocus's connection-level Ping (the bare type byte, no docName) at
   that interval, and the provider's bare Pong is consumed. The provider
   reconnects after 30 s without a message, and a peer alone in a room gets
   none — ygo does not echo a peer's own awareness back to it the way
   Hocuspocus does. `server.go` (`PingInterval`, `ServeHTTP`) and `peer.go`
   (`runPinger`, `hocuspocusPing`, `writeToConn`, `handleMessage`).

## Upgrading

Copy the same package set from the new release, re-apply the patches, and drop
this directory and the `replace` once upstream carries equivalent APIs.
