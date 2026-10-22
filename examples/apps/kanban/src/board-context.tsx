// Everything the board's screens share: the connected app, the live records
// read as a board, the names of the people and linked items it mentions, the
// comment counts, and the writes (`ops.ts`) wrapped with their failure toasts.
// The dialogs render in better-modal's container, which sits inside this
// provider, so they read the same live board.

import {
  createContext,
  use,
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { useLingui } from "@lingui/react/macro";
import { AppError, type App, type LinkTarget, type Person } from "@pp-hub/app-sdk";
import { useApp } from "./bridge/app-provider";
import { useRecords, type RecordStore } from "./bridge/use-records";
import { type Board, boardOf } from "./board-state";
import { type BoardOps, createOps } from "./ops";
import { type Rule, RuleError } from "./rules";

/** How often comment counts are refreshed while the board is open; the host tells the frame nothing when a comment is posted. */
const COUNTS_EVERY_MS = 30_000;
/** A burst of record events refreshes the counts once. */
const COUNTS_SETTLE_MS = 1_500;

interface BoardContextValue {
  app: App;
  canEdit: boolean;
  store: RecordStore;
  board: Board;
  /** People by id: assignees fetched with `users.get`, plus everyone a search returned. */
  people: ReadonlyMap<string, Person>;
  rememberPeople: (people: Person[]) => void;
  /** Linked nodes by id; null is one the viewer cannot open (or that is gone). Absent: not resolved yet. */
  links: ReadonlyMap<string, LinkTarget | null>;
  rememberLinks: (targets: LinkTarget[]) => void;
  /** Comment counts by subject (`record:<cardId>`). */
  counts: Readonly<Record<string, number>>;
  ops: BoardOps;
  /**
   * Runs a write. A failure toasts — `failMessage`, a refused rule's own
   * sentence, or "changed by someone else" for a stale rev — and, unless the
   * app refused it before writing, reloads the board. Resolves undefined then.
   */
  run: <T>(write: () => Promise<T>, failMessage: string) => Promise<T | undefined>;
}

const BoardContext = createContext<BoardContextValue | null>(null);

export function useBoard(): BoardContextValue {
  const value = use(BoardContext);
  // eslint-disable-next-line lingui/no-unlocalized-strings -- developer error
  if (!value) throw new Error("useBoard is used outside BoardProvider");
  return value;
}

/** Ids in `ids` not yet asked for, marked as asked. */
function unasked(ids: Iterable<string>, asked: Set<string>): string[] {
  const out: string[] = [];
  for (const id of ids) {
    if (asked.has(id)) continue;
    asked.add(id);
    out.push(id);
  }
  return out;
}

export function BoardProvider({ children }: { children: ReactNode }) {
  const app = useApp();
  const store = useRecords(app);
  const { t } = useLingui();
  const board = useMemo(() => boardOf(store.records), [store.records]);

  const [people, setPeople] = useState<ReadonlyMap<string, Person>>(() => new Map());
  const [links, setLinks] = useState<ReadonlyMap<string, LinkTarget | null>>(() => new Map());
  const [counts, setCounts] = useState<Record<string, number>>({});
  const askedPeople = useRef(new Set<string>());
  const askedLinks = useRef(new Set<string>());

  const rememberPeople = useCallback((found: Person[]) => {
    if (found.length === 0) return;
    setPeople((map) => new Map([...map, ...found.map((p) => [p.id, p] as const)]));
  }, []);
  const rememberLinks = useCallback((found: LinkTarget[]) => {
    if (found.length === 0) return;
    setLinks((map) => new Map([...map, ...found.map((l) => [l.id, l] as const)]));
  }, []);

  // Names of assignees and linked items the board mentions, fetched once each.
  useEffect(() => {
    const userIds = unasked(
      board.cards.flatMap((c) => c.assignees),
      askedPeople.current,
    );
    if (userIds.length > 0) app.users.get(userIds).then(rememberPeople, () => {});
    const linkIds = unasked(
      board.cards.flatMap((c) => c.link_ids),
      askedLinks.current,
    );
    if (linkIds.length > 0) {
      app.links.resolve(linkIds).then(
        (found) =>
          setLinks((map) => {
            const next = new Map(map);
            for (const id of linkIds) next.set(id, found.find((l) => l.id === id) ?? null);
            return next;
          }),
        () => {},
      );
    }
  }, [app, board, rememberPeople]);

  // Comment counts: on open, on reconnect, after record changes, and on a timer.
  useEffect(() => {
    let timer: ReturnType<typeof setTimeout> | undefined;
    const refresh = () => {
      clearTimeout(timer);
      app.comments.counts().then(setCounts, () => {});
    };
    const soon = () => {
      clearTimeout(timer);
      timer = setTimeout(refresh, COUNTS_SETTLE_MS);
    };
    refresh();
    const every = setInterval(refresh, COUNTS_EVERY_MS);
    /* eslint-disable lingui/no-unlocalized-strings -- bridge event names */
    const offRecords = app.on("records", soon);
    const offStream = app.on("stream", (e) => {
      /* eslint-enable lingui/no-unlocalized-strings */
      if (e.state === "reconnected") {
        // A link or a person may have changed while the stream was down.
        askedLinks.current.clear();
        refresh();
      }
    });
    return () => {
      clearTimeout(timer);
      clearInterval(every);
      offRecords();
      offStream();
    };
  }, [app]);

  // The writes read the board as it is when they run, not as it was rendered.
  const latest = useRef(board);
  useEffect(() => {
    latest.current = board;
  }, [board]);
  const { upsert, remove, patch } = store;
  const ops = useMemo(
    () => createOps(app, { upsert, remove, patch }, () => latest.current),
    [app, upsert, remove, patch],
  );

  const ruleMessages: Partial<Record<Rule, string>> = {
    columns_full: t`A board holds at most 30 columns`,
    labels_full: t`A board holds at most 50 labels`,
    cards_full: t`A board holds at most 5000 cards`,
    links_full: t`A card links to at most 20 items`,
    too_many: t`A card holds at most 50 assignees and 50 labels`,
    label_exists: t`A label with this name exists`,
    label_missing: t`A label on this card was deleted meanwhile`,
    link_unavailable: t`A linked item is not available`,
  };
  const changed = t`This card was changed by someone else. The board has been refreshed. Review it and try again.`;

  const run = async <T,>(write: () => Promise<T>, failMessage: string) => {
    try {
      return await write();
    } catch (e) {
      const refused = e instanceof RuleError;
      const message = refused
        ? (ruleMessages[e.rule] ?? failMessage)
        : e instanceof AppError && e.status === 409
          ? changed
          : failMessage;
      // A refusal wrote nothing, unless the board moved on under it ("gone").
      if (!refused || e.rule === "gone") void store.reload();
      // eslint-disable-next-line lingui/no-unlocalized-strings -- toast variant
      void app.ui.toast(message, "error");
      return undefined;
    }
  };

  const value: BoardContextValue = {
    app,
    canEdit: app.context.caps.edit,
    store,
    board,
    people,
    rememberPeople,
    links,
    rememberLinks,
    counts,
    ops,
    run,
  };
  return <BoardContext value={value}>{children}</BoardContext>;
}
