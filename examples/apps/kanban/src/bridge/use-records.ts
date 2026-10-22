/* eslint-disable lingui/no-unlocalized-strings -- load states and bridge event names, not copy */
// The node's records, kept live: loaded with `records.list()`, patched by the
// host's `records` events, refetched when the event stream reconnects (events
// sent while it was down are not replayed). (Kept identical in app-kanban and
// app-checklist; the apps share no workspace package.)

import { useCallback, useEffect, useMemo, useState } from "react";
import { AppError, type App, type AppRecord } from "@pp-hub/app-sdk";
import {
  applyRecordsEvent,
  deleteRecord,
  fromList,
  patchRecord,
  upsertRecord,
  type RecordMap,
} from "./records";

export interface RecordStore {
  /** "error" only when the first load failed; a failed refetch keeps what is shown. */
  status: "loading" | "ready" | "error";
  /** The failed first load was refused (403): the app is switched off here, or access is gone. */
  refused: boolean;
  records: RecordMap;
  /** Refetches everything. */
  reload: () => Promise<void>;
  /** Puts a write's answer in the snapshot (an older rev than the cached one is ignored). */
  upsert: (record: AppRecord) => void;
  remove: (id: string) => void;
  /** An optimistic edit, replaced by the write's answer or undone by `reload`. */
  patch: (id: string, data: Record<string, unknown>) => void;
}

export function useRecords(app: App): RecordStore {
  const [status, setStatus] = useState<RecordStore["status"]>("loading");
  const [refused, setRefused] = useState(false);
  const [records, setRecords] = useState<RecordMap>(() => new Map());

  const reload = useCallback(async () => {
    try {
      const list = await app.records.list();
      setRecords(fromList(list));
      setStatus("ready");
    } catch (e) {
      setRefused(e instanceof AppError && e.status === 403);
      setStatus((s) => (s === "ready" ? s : "error"));
    }
  }, [app]);

  useEffect(() => {
    void reload();
    const offRecords = app.on("records", (event) =>
      setRecords((map) => applyRecordsEvent(map, event)),
    );
    const offStream = app.on("stream", (event) => {
      if (event.state === "reconnected") void reload();
    });
    return () => {
      offRecords();
      offStream();
    };
  }, [app, reload]);

  const edits = useMemo(
    () => ({
      upsert: (record: AppRecord) => setRecords((map) => upsertRecord(map, record)),
      remove: (id: string) => setRecords((map) => deleteRecord(map, id)),
      patch: (id: string, data: Record<string, unknown>) =>
        setRecords((map) => patchRecord(map, id, data)),
    }),
    [],
  );

  return { status, refused, records, reload, ...edits };
}
