/* eslint-disable lingui/no-unlocalized-strings -- record fields, notification verb and subjects, not copy */
// Every change to a board, as record writes through the bridge. This is the
// old kanban server's logic run by the app: the checks of `rules.ts`, append
// and move positions from `position.ts`, the renumbering a long key triggers,
// label cleanup, and the notice a new assignee is owed. Each write shows at
// once (an optimistic edit or the write's answer); a failure throws, and the
// caller reloads.

import type { App, AppRecord } from "@pp-hub/app-sdk";
import type { RecordStore } from "./bridge/use-records";
import {
  type Board,
  CARDS,
  type Card,
  type CardData,
  cardData,
  cardsIn,
  COLUMNS,
  type Label,
  LABELS,
  placeCard,
  placeColumn,
  type Placement,
  sortedColumns,
} from "./board-state";
import { after } from "./position";
import {
  checkCard,
  checkLabel,
  checkNewCard,
  checkNewColumn,
  checkWipLimit,
  cleanText,
  LIMITS,
  RuleError,
} from "./rules";

/** The comment and notification subject of a card. */
export const cardSubject = (cardId: string) => `record:${cardId}`;

export interface ColumnPatch {
  title?: string;
  /** A number sets, null clears. */
  wip_limit?: number | null;
  /** Moves the column: absent = no move, null = first, an id = after that column. */
  after_id?: string | null;
}

/** A card edit; an absent field is left as it is. Id sets are complete. */
export type CardPatch = Partial<
  Pick<CardData, "title" | "description_md" | "due_at" | "assignees" | "label_ids" | "link_ids">
>;

/** The board's writes. Each throws a `RuleError` for a refused write, or the bridge's `AppError`. */
export interface BoardOps {
  createColumn(title: string, wipLimit?: number | null): Promise<AppRecord>;
  updateColumn(columnId: string, patch: ColumnPatch): Promise<AppRecord>;
  deleteColumn(columnId: string, moveTo?: string): Promise<void>;
  createCard(columnId: string, title: string): Promise<AppRecord>;
  updateCard(card: Card, patch: CardPatch): Promise<AppRecord>;
  moveCard(cardId: string, columnId: string, afterId: string): Promise<AppRecord>;
  deleteCard(cardId: string): Promise<void>;
  createLabel(name: string, color: string): Promise<Label>;
  updateLabel(labelId: string, patch: { name?: string; color?: string }): Promise<AppRecord>;
  deleteLabel(labelId: string): Promise<void>;
}

/** `getBoard` reads the latest snapshot at call time. */
export function createOps(
  app: App,
  store: Pick<RecordStore, "upsert" | "remove" | "patch">,
  getBoard: () => Board,
): BoardOps {
  const put = (r: AppRecord) => {
    store.upsert(r);
    return r;
  };

  /** Rewrites the positions a renumbering moved, then the moving item. */
  const writePlacement = async (place: Placement, id: string, data: Record<string, unknown>) => {
    for (const r of place.renumber) store.patch(r.id, { position: r.position });
    store.patch(id, data);
    await Promise.all(
      place.renumber.map((r) => app.records.merge(r.id, { position: r.position }).then(put)),
    );
    return put(await app.records.merge(id, data));
  };

  return {
    async createColumn(title: string, wipLimit: number | null = null) {
      const board = getBoard();
      const data = {
        title: cleanText(title, LIMITS.columnTitle),
        position: "",
        wip_limit: checkWipLimit(wipLimit),
      };
      checkNewColumn(board);
      data.position = after(sortedColumns(board).at(-1)?.position ?? "");
      return put(await app.records.create(COLUMNS, data));
    },

    async updateColumn(columnId: string, patch: ColumnPatch) {
      const board = getBoard();
      if (!board.columns.some((c) => c.id === columnId)) throw new RuleError("gone");
      const data: Record<string, unknown> = {};
      if (patch.title !== undefined) data.title = cleanText(patch.title, LIMITS.columnTitle);
      if (patch.wip_limit !== undefined) data.wip_limit = checkWipLimit(patch.wip_limit);
      if (patch.after_id === undefined) {
        store.patch(columnId, data);
        return put(await app.records.merge(columnId, data));
      }
      const place = placeColumn(board, columnId, patch.after_id);
      if (!place) throw new RuleError("gone");
      return writePlacement(place, columnId, { ...data, position: place.position });
    },

    /**
     * Deletes a column. Its cards go to the end of `moveTo`, in order; without
     * `moveTo` they are deleted with it (the host cascades nothing).
     */
    async deleteColumn(columnId: string, moveTo?: string) {
      const board = getBoard();
      if (
        moveTo !== undefined &&
        (moveTo === columnId || !board.columns.some((c) => c.id === moveTo))
      ) {
        throw new RuleError("gone");
      }
      const cards = cardsIn(board, columnId);
      if (moveTo !== undefined) {
        let last = cardsIn(board, moveTo).at(-1)?.position ?? "";
        const moves = cards.map((c) => {
          last = after(last);
          return { id: c.id, data: { column_id: moveTo, position: last } };
        });
        for (const m of moves) store.patch(m.id, m.data);
        await Promise.all(moves.map((m) => app.records.merge(m.id, m.data).then(put)));
      } else {
        for (const c of cards) store.remove(c.id);
        await Promise.all(cards.map((c) => app.records.delete(c.id)));
      }
      store.remove(columnId);
      await app.records.delete(columnId);
    },

    async createCard(columnId: string, title: string) {
      const board = getBoard();
      checkNewCard(board, columnId);
      const data: CardData = checkCard(
        board,
        {
          column_id: columnId,
          title,
          description_md: "",
          position: after(cardsIn(board, columnId).at(-1)?.position ?? ""),
          due_at: null,
          assignees: [],
          label_ids: [],
          link_ids: [],
        },
        app.context.node.id,
      );
      return put(await app.records.create(CARDS, data));
    },

    /**
     * Saves a card edit against the rev it was read at: a card someone else
     * changed meanwhile is refused (409) rather than overwritten. A link added
     * must be one the editor can open in this space. Each person newly assigned
     * is notified.
     */
    async updateCard(card: Card, patch: CardPatch) {
      const board = getBoard();
      const data = checkCard(board, { ...cardData(card), ...patch }, app.context.node.id);
      const added = data.link_ids.filter((id) => !card.link_ids.includes(id));
      if (added.length > 0) {
        const found = await app.links.resolve(added);
        const space = app.context.node.space_id;
        const ok = new Set(found.filter((t) => t.space_id === space).map((t) => t.id));
        if (added.some((id) => !ok.has(id))) throw new RuleError("link_unavailable");
      }
      store.patch(card.id, { ...data });
      const saved = put(await app.records.update(card.id, data, { ifRev: card.rev }));
      const me = app.context.user.id;
      for (const to of data.assignees) {
        if (card.assignees.includes(to) || to === me) continue;
        // Best effort, as on the server: the card is saved whatever happens here.
        app
          .notify({ to, verb: "assigned", excerpt: data.title, subject: cardSubject(card.id) })
          .catch(() => {});
      }
      return saved;
    },

    /** Moves a card into `columnId` under `afterId` ("" = the top). */
    async moveCard(cardId: string, columnId: string, afterId: string) {
      const board = getBoard();
      if (!board.cards.some((c) => c.id === cardId)) throw new RuleError("gone");
      if (!board.columns.some((c) => c.id === columnId)) throw new RuleError("gone");
      const place = placeCard(board, cardId, columnId, afterId);
      if (!place) throw new RuleError("gone");
      return writePlacement(place, cardId, { column_id: columnId, position: place.position });
    },

    async deleteCard(cardId: string) {
      store.remove(cardId);
      await app.records.delete(cardId);
    },

    async createLabel(name: string, color: string): Promise<Label> {
      const board = getBoard();
      const data = checkLabel(board, { name, color });
      const r = put(await app.records.create(LABELS, data));
      return { id: r.id, rev: r.rev, name: data.name!, color: data.color! };
    },

    async updateLabel(labelId: string, patch: { name?: string; color?: string }) {
      const board = getBoard();
      if (!board.labels.some((l) => l.id === labelId)) throw new RuleError("gone");
      const data = checkLabel(board, patch, labelId);
      store.patch(labelId, data);
      return put(await app.records.merge(labelId, data));
    },

    /** Deletes a label, then takes it off the cards that carry it (best effort: readers ignore a dangling id). */
    async deleteLabel(labelId: string) {
      const board = getBoard();
      store.remove(labelId);
      await app.records.delete(labelId);
      const carrying = board.cards.filter((c) => c.label_ids.includes(labelId));
      await Promise.all(
        carrying.map((c) =>
          app.records
            .merge(
              c.id,
              { label_ids: c.label_ids.filter((id) => id !== labelId) },
              { ifRev: c.rev },
            )
            .then(put, () => {}),
        ),
      );
    },
  };
}
