/* eslint-disable lingui/no-unlocalized-strings -- drag-kind discriminators, not copy */
import { useEffect, useMemo, useRef, useState } from "react";
import { Trans, useLingui } from "@lingui/react/macro";
import { Eye, Lightbulb, Plus, X } from "lucide-react";
import type { Person } from "@pp-hub/app-sdk";
import { Button } from "@workspace/ui/components/button";
import { Empty, EmptyHeader, EmptyTitle } from "@workspace/ui/components/empty";
import { Input } from "@workspace/ui/components/input";
import { Skeleton } from "@workspace/ui/components/skeleton";
import { Unavailable } from "./bridge/app-provider";
import { useBoard } from "./board-context";
import { BoardColumn, type DragState } from "./board-column";
import {
  type Card,
  type CardFilter,
  cardsIn,
  columnDropAfter,
  dropPosition,
  isFiltering,
  isNoopCardMove,
  isNoopColumnMove,
  matchesFilter,
  NO_FILTER,
  slotFromPointer,
  sortedColumns,
} from "./board-state";
import { BoardToolbar } from "./board-toolbar";
import { CardDialog } from "./card-dialog";
import { ColumnDialog, DeleteColumnDialog } from "./column-dialogs";
import { LabelsDialog } from "./labels-dialog";
import { LIMITS } from "./rules";

// Custom MIME types: a file dragged in from the desktop carries neither, so the
// board's drop targets ignore it.
const CARD_DATA_TYPE = "application/x-pphub-kanban-card";
const COLUMN_DATA_TYPE = "application/x-pphub-kanban-column";

/** A notification links a card as `record:<cardId>`. */
const SUBJECT_PREFIX = "record:";

type Over = { kind: "card"; columnId: string; slot: number } | { kind: "column"; slot: number };

function BoardSkeleton() {
  return (
    <div role="status" className="flex h-full gap-3 overflow-hidden p-3">
      {[3, 2, 1, 2].map((rows, i) => (
        <div
          key={i}
          className="bg-muted/40 flex h-fit w-72 shrink-0 flex-col gap-2 rounded-lg border p-2"
        >
          <Skeleton className="h-4 w-24" />
          {Array.from({ length: rows }, (_, r) => (
            <Skeleton key={r} className="h-14 w-full" />
          ))}
        </div>
      ))}
    </div>
  );
}

function AddColumn({ onAdd }: { onAdd: (title: string) => void }) {
  const { t } = useLingui();
  const [adding, setAdding] = useState(false);
  const [title, setTitle] = useState("");
  const trimmed = title.trim();

  if (!adding) {
    return (
      <button
        type="button"
        className="text-muted-foreground hover:border-ring/60 hover:text-foreground focus-visible:ring-ring/50 flex h-24 w-72 shrink-0 flex-col items-center justify-center gap-1.5 rounded-md border border-dashed text-xs font-medium transition-colors outline-none focus-visible:ring-2"
        onClick={() => setAdding(true)}
      >
        <span className="bg-muted flex size-7 items-center justify-center rounded-full">
          <Plus className="size-4" />
        </span>
        <Trans>Add column</Trans>
      </button>
    );
  }

  return (
    <form
      className="bg-muted/40 flex h-fit w-72 shrink-0 flex-col gap-1.5 rounded-md border p-2"
      onSubmit={(e) => {
        e.preventDefault();
        if (!trimmed) return;
        onAdd(trimmed);
        setTitle("");
        setAdding(false);
      }}
    >
      <Input
        autoFocus
        value={title}
        maxLength={LIMITS.columnTitle}
        aria-label={t`Column name`}
        placeholder={t`Column name`}
        onChange={(e) => setTitle(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Escape") {
            e.stopPropagation();
            setTitle("");
            setAdding(false);
          }
        }}
      />
      <div className="flex gap-1.5">
        <Button type="submit" size="sm" disabled={!trimmed}>
          <Trans>Add column</Trans>
        </Button>
        <Button
          type="button"
          size="sm"
          variant="ghost"
          onClick={() => {
            setTitle("");
            setAdding(false);
          }}
        >
          <Trans>Cancel</Trans>
        </Button>
      </div>
    </form>
  );
}

/** A short note shown over an empty board, until dismissed. */
function BoardHint({ onDismiss }: { onDismiss: () => void }) {
  const { t } = useLingui();
  return (
    <aside className="bg-popover text-popover-foreground absolute bottom-6 left-1/2 z-10 flex w-96 max-w-[calc(100%-2rem)] -translate-x-1/2 flex-col gap-2 rounded-lg border p-3 text-xs shadow-md">
      <div className="flex items-center gap-2">
        <span className="bg-primary/10 text-primary flex size-6 items-center justify-center rounded-full">
          <Lightbulb className="size-3.5" aria-hidden />
        </span>
        <h2 className="flex-1 text-sm font-medium">
          <Trans>Plan work on this board</Trans>
        </h2>
        <Button variant="ghost" size="icon-xs" aria-label={t`Dismiss`} onClick={onDismiss}>
          <X />
        </Button>
      </div>
      <ul className="text-muted-foreground flex list-disc flex-col gap-1.5 pl-5">
        <li>
          <Trans>Add cards to a column and drag them between columns.</Trans>
        </li>
        <li>
          <Trans>
            Open a card to set a due date, assign people, add labels and link drive files.
          </Trans>
        </li>
        <li>
          <Trans>Share the board from the header — people see changes live.</Trans>
        </li>
      </ul>
      <div className="flex justify-end border-t pt-2">
        <Button variant="ghost" className="text-primary" onClick={onDismiss}>
          <Trans>Got it</Trans>
        </Button>
      </div>
    </aside>
  );
}

/**
 * The kanban board of one node: columns side by side in a row that scrolls on
 * its own, cards that drag between and within columns, columns that drag by
 * their header. The host draws the header; this fills the frame under it.
 */
export function BoardEditor() {
  const { app, board: data, canEdit, store, people: known, ops, run } = useBoard();
  const { t } = useLingui();

  const [drag, setDrag] = useState<DragState | null>(null);
  const [over, setOver] = useState<Over | null>(null);
  const [filter, setFilter] = useState<CardFilter>(NO_FILTER);
  // The frame has no storage, so a dismissed hint comes back on the next visit.
  const [hintOpen, setHintOpen] = useState(true);
  const ready = store.status === "ready";

  // A notification link (`context.subject` = "record:<cardId>") opens that card
  // once, after the board has loaded, and brings its tile into view.
  const opened = useRef(false);
  const subject = app.context.subject;
  useEffect(() => {
    if (opened.current || !ready || !subject?.startsWith(SUBJECT_PREFIX)) return;
    opened.current = true;
    const id = subject.slice(SUBJECT_PREFIX.length);
    if (!data.cards.some((c) => c.id === id)) return;
    void CardDialog.show({ cardId: id });
    requestAnimationFrame(() =>
      document
        .querySelector(`[data-card-id="${id}"]`)
        ?.scrollIntoView({ block: "nearest", inline: "center" }),
    );
  }, [subject, ready, data]);

  const columns = useMemo(() => sortedColumns(data), [data]);
  const allByColumn = useMemo(
    () => new Map(columns.map((c) => [c.id, cardsIn(data, c.id)])),
    [columns, data],
  );
  // What the toolbar filter lets through. Drag and drop and "move to" work on
  // this view: a card lands under the card it is dropped beside.
  const byColumn = useMemo(
    () =>
      isFiltering(filter)
        ? new Map(
            [...allByColumn].map(([id, cards]) => [
              id,
              cards.filter((c) => matchesFilter(c, filter)),
            ]),
          )
        : allByColumn,
    [allByColumn, filter],
  );
  // The people the assignee filter offers: everyone on a card of this board.
  const people = useMemo(() => {
    const seen = new Map<string, Person>();
    for (const card of data.cards) {
      for (const id of card.assignees) seen.set(id, known.get(id) ?? { id, name: "" });
    }
    return [...seen.values()].sort((a, b) => a.name.localeCompare(b.name));
  }, [data, known]);
  const shown = useMemo(
    () => [...byColumn.values()].reduce((n, cards) => n + cards.length, 0),
    [byColumn],
  );

  if (store.status === "loading") return <BoardSkeleton />;

  if (store.status === "error") {
    return (
      <Unavailable what={t`board`} refused={store.refused} onRetry={() => void store.reload()} />
    );
  }

  const endDrag = () => {
    setDrag(null);
    setOver(null);
  };

  const setSlot = (next: Over) =>
    setOver((prev) =>
      prev &&
      prev.kind === next.kind &&
      prev.slot === next.slot &&
      (prev.kind !== "card" || next.kind !== "card" || prev.columnId === next.columnId)
        ? prev
        : next,
    );

  const moveCard = (cardId: string, columnId: string, afterId: string) =>
    void run(() => ops.moveCard(cardId, columnId, afterId), t`Could not move the card`);

  const moveColumn = (columnId: string, afterId: string | null) =>
    void run(
      () => ops.updateColumn(columnId, { after_id: afterId }),
      t`Could not update the column`,
    );

  const moveTo = (card: Card, columnId: string) =>
    moveCard(card.id, columnId, allByColumn.get(columnId)?.at(-1)?.id ?? "");

  const deleteCard = async (card: Card) => {
    const cardTitle = card.title;
    const ok = await app.ui.confirm({
      title: t`Delete this card?`,
      message: t`"${cardTitle}" will be deleted for everyone who can open this board.`,
      confirmLabel: t`Delete`,
      destructive: true,
    });
    if (ok) void run(() => ops.deleteCard(card.id), t`Could not delete the card`);
  };

  const columnSlotAt = (e: React.DragEvent<HTMLElement>) => {
    const els = e.currentTarget.querySelectorAll<HTMLElement>("[data-column-id]");
    const mids = Array.from(els, (el) => {
      const box = el.getBoundingClientRect();
      return box.left + box.width / 2;
    });
    return slotFromPointer(mids, e.clientX);
  };

  return (
    <div className="relative flex h-full min-h-0 flex-col">
      {!canEdit && (
        <div className="bg-muted/40 text-muted-foreground flex shrink-0 items-center gap-2 border-b px-3 py-1.5 text-xs">
          <Eye className="size-3.5" aria-hidden />
          <Trans>You can view this board but not change it.</Trans>
        </div>
      )}
      <BoardToolbar
        filter={filter}
        onFilter={setFilter}
        labels={data.labels}
        people={people}
        total={data.cards.length}
        shown={shown}
        columns={columns.length}
        onManageLabels={canEdit ? () => void LabelsDialog.show({}) : undefined}
      />
      <div
        className="flex min-h-0 flex-1 items-start gap-3 overflow-x-auto p-3"
        onDragOver={(e) => {
          if (drag?.kind !== "column") return;
          e.preventDefault();
          e.dataTransfer.dropEffect = "move";
          setSlot({ kind: "column", slot: columnSlotAt(e) });
        }}
        onDrop={(e) => {
          if (drag?.kind !== "column") return;
          e.preventDefault();
          const ids = columns.map((c) => c.id);
          const afterId = columnDropAfter(ids, columnSlotAt(e), drag.id);
          if (!isNoopColumnMove(data, drag.id, afterId)) moveColumn(drag.id, afterId);
          endDrag();
        }}
      >
        {columns.length === 0 && !canEdit && (
          <Empty className="h-full w-full">
            <EmptyHeader>
              <EmptyTitle>
                <Trans>This board has no columns yet</Trans>
              </EmptyTitle>
            </EmptyHeader>
          </Empty>
        )}

        {columns.map((column, i) => {
          const cards = byColumn.get(column.id) ?? [];
          const columnEdge =
            over?.kind === "column"
              ? over.slot === i
                ? "before"
                : over.slot === columns.length && i === columns.length - 1
                  ? "after"
                  : null
              : null;
          return (
            <BoardColumn
              key={column.id}
              column={column}
              columns={columns}
              cards={cards}
              total={allByColumn.get(column.id)?.length ?? 0}
              labels={data.labels}
              canEdit={canEdit}
              drag={drag}
              cardSlot={over?.kind === "card" && over.columnId === column.id ? over.slot : null}
              columnEdge={columnEdge}
              onCardSlot={(slot) => setSlot({ kind: "card", columnId: column.id, slot })}
              onCardDrop={(ids, slot) => {
                if (drag?.kind === "card") {
                  const drop = dropPosition(column.id, ids, slot, drag.id);
                  if (!isNoopCardMove(data, drag.id, drop)) {
                    moveCard(drag.id, drop.column_id, drop.after_id);
                  }
                }
                endDrag();
              }}
              onDragLeave={() => setOver(null)}
              onCardDragStart={(card, e) => {
                e.dataTransfer.effectAllowed = "move";
                e.dataTransfer.setData(CARD_DATA_TYPE, card.id);
                setDrag({ kind: "card", id: card.id });
              }}
              onColumnDragStart={(e, element) => {
                e.dataTransfer.effectAllowed = "move";
                e.dataTransfer.setData(COLUMN_DATA_TYPE, column.id);
                e.dataTransfer.setDragImage(element, 24, 16);
                setDrag({ kind: "column", id: column.id });
              }}
              onDragEnd={endDrag}
              onOpenCard={(card) => void CardDialog.show({ cardId: card.id })}
              onMoveCard={moveTo}
              onDeleteCard={(card) => void deleteCard(card)}
              onAddCard={(title) =>
                void run(() => ops.createCard(column.id, title), t`Could not add the card`)
              }
              onRename={() => void ColumnDialog.show({ columnId: column.id, field: "title" })}
              onWipLimit={() => void ColumnDialog.show({ columnId: column.id, field: "wip_limit" })}
              onDelete={() => void DeleteColumnDialog.show({ columnId: column.id })}
              onMoveColumn={(afterId) => moveColumn(column.id, afterId)}
            />
          );
        })}

        {canEdit && (
          <AddColumn
            onAdd={(title) => void run(() => ops.createColumn(title), t`Could not add the column`)}
          />
        )}
      </div>
      {canEdit && data.cards.length === 0 && hintOpen && (
        <BoardHint onDismiss={() => setHintOpen(false)} />
      )}
    </div>
  );
}
