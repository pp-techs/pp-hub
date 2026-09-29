/* eslint-disable lingui/no-unlocalized-strings -- room phases and provider event names */
import { useEffect, useState, useSyncExternalStore } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { HocuspocusProvider, WebSocketStatus } from "@hocuspocus/provider";
import { Awareness } from "y-protocols/awareness";
import * as Y from "yjs";
import { ApiError, collabSocketUrl, type CollabSession } from "@/lib/api";
import { collabOptions } from "@/lib/queries";

export type CollabRoom =
  | { phase: "loading" }
  /** The server refused the document (4xx): access revoked, or no longer a markdown file. */
  | { phase: "denied" }
  /** The first `/collab` read failed for another reason (network, 5xx). */
  | { phase: "failed"; error: unknown }
  | {
      phase: "open";
      session: CollabSession;
      doc: Y.Doc;
      provider: HocuspocusProvider;
      /** The provider's awareness, typed non-null: presence and carets read it. */
      awareness: Awareness;
      /**
       * The first sync with the server has completed. The editor mounts only
       * then: mounted on an empty document it would write its own empty
       * paragraph (with a fresh block id) into the room, ahead of the content.
       */
      ready: boolean;
    };

interface Connection {
  session: CollabSession;
  doc: Y.Doc;
  provider: HocuspocusProvider;
  awareness: Awareness;
  ready: boolean;
}

const isRefusal = (e: unknown) => e instanceof ApiError && e.status >= 400 && e.status < 500;

/**
 * The live room for a markdown document, kept honest across reconnects.
 *
 * The server closes a socket without a close code for every reason it has
 * — a revoked share, a downgrade, a restart, a room unloaded under a laptop
 * that slept — and a client cannot tell those apart from a network drop.
 * So on every close this re-reads `GET /nodes/{id}/collab`:
 *
 * - a 4xx means access is gone: the provider is destroyed (it would retry a
 *   401 forever otherwise) and the page says so;
 * - a different room means the server reloaded the document under a new
 *   epoch: this Y.Doc is from the old generation and merging it would
 *   duplicate the document, so the doc and provider are both replaced;
 * - the same room: the provider reconnects on its own, and `can_write` from
 *   the answer is what the editor's editability follows.
 *
 * Anything else (the API itself unreachable) leaves the provider retrying;
 * its next close asks again.
 */
export function useCollabRoom(nodeId: string): { room: CollabRoom; retry: () => void } {
  const qc = useQueryClient();
  const [room, setRoom] = useState<CollabRoom>({ phase: "loading" });
  const [attempt, setAttempt] = useState(0);

  useEffect(() => {
    let disposed = false;
    let current: Connection | null = null;
    let checking = false;

    const publish = (c: Connection) => {
      if (disposed || current !== c) return;
      const { session, doc, provider, awareness, ready } = c;
      setRoom({ phase: "open", session, doc, provider, awareness, ready });
    };

    const close = () => {
      const c = current;
      if (!c) return;
      // Cleared first: destroying the provider closes its socket, and that
      // close must not be taken for one worth re-reading the room over.
      current = null;
      c.provider.destroy();
      c.awareness.destroy();
      c.doc.destroy();
    };

    const recheck = async (c: Connection) => {
      if (checking || disposed || current !== c) return;
      checking = true;
      try {
        const next = await qc.fetchQuery(collabOptions(nodeId));
        if (disposed || current !== c) return;
        if (next.room !== c.session.room) {
          close();
          open(next);
        } else {
          c.session = next;
          publish(c);
        }
      } catch (e) {
        if (disposed || current !== c || !isRefusal(e)) return;
        close();
        setRoom({ phase: "denied" });
      } finally {
        checking = false;
      }
    };

    const open = (session: CollabSession) => {
      const doc = new Y.Doc();
      const awareness = new Awareness(doc);
      const provider = new HocuspocusProvider({
        url: collabSocketUrl(session.room),
        name: session.room,
        document: doc,
        awareness,
      });
      const c: Connection = { session, doc, provider, awareness, ready: false };
      current = c;
      provider.on("synced", ({ state }: { state: boolean }) => {
        if (!state || c.ready) return;
        c.ready = true;
        publish(c);
      });
      // Fires for every closed socket, a refused upgrade included.
      provider.on("close", () => void recheck(c));
      publish(c);
    };

    // Chrome keeps a page with an open socket in the back/forward cache, so a
    // navigation away left this tab joined — holding the room, its file lock
    // and a presence avatar — until the cache evicted it. Leave on pagehide;
    // a page restored from the cache joins again from scratch.
    const onPageShow = (e: PageTransitionEvent) => {
      if (e.persisted) setAttempt((n) => n + 1);
    };
    window.addEventListener("pagehide", close);
    window.addEventListener("pageshow", onPageShow);

    setRoom({ phase: "loading" });
    qc.fetchQuery(collabOptions(nodeId)).then(
      (session) => {
        if (!disposed) open(session);
      },
      (error: unknown) => {
        if (!disposed) setRoom(isRefusal(error) ? { phase: "denied" } : { phase: "failed", error });
      },
    );

    return () => {
      disposed = true;
      window.removeEventListener("pagehide", close);
      window.removeEventListener("pageshow", onPageShow);
      close();
    };
  }, [nodeId, qc, attempt]);

  return { room, retry: () => setAttempt((n) => n + 1) };
}

export type SyncState = "saved" | "syncing" | "offline";

/**
 * What the header says about the local edits: `saved` once the server has
 * acknowledged every update this tab sent, `syncing` while some are
 * unacknowledged, `offline` while the socket is down (edits keep, and go
 * out on reconnect).
 */
export function useSyncState(provider: HocuspocusProvider): SyncState {
  return useSyncExternalStore(
    (onChange) => {
      provider.on("status", onChange);
      provider.on("unsyncedChanges", onChange);
      provider.on("synced", onChange);
      return () => {
        provider.off("status", onChange);
        provider.off("unsyncedChanges", onChange);
        provider.off("synced", onChange);
      };
    },
    () => {
      if (provider.configuration.websocketProvider.status !== WebSocketStatus.Connected) {
        return "offline";
      }
      return provider.hasUnsyncedChanges ? "syncing" : "saved";
    },
  );
}
