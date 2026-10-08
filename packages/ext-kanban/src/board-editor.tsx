/* eslint-disable lingui/no-unlocalized-strings -- drag-kind discriminators, not copy */
import { useEffect, useMemo, useRef, useState } from "react";
import { Trans, useLingui } from "@lingui/react/macro";
import { Eye, Lightbulb, Plus, X } from "lucide-react";
import { Button } from "@workspace/ui/components/button";
import { Empty, EmptyHeader, EmptyTitle } from "@workspace/ui/components/empty";
import { Input } from "@workspace/ui/components/input";
import { Skeleton } from "@workspace/ui/components/skeleton";
import {
  ConfirmDialog,
  ExtUnavailable,
  isAppOff,
  LoadingStatus,
  type ExtEditorProps,
} from "@pp-hub/ext-sdk";
import type { Card, Person } from "./api";
import { BoardColumn, type DragState } from "./board-column";
import {
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
import {
  useBoard,
  useCreateCard,
  useCreateColumn,
  useDeleteCard,
  useMoveCard,
  useUpdateColumn,
} from "./queries";
import { useBoardEvents } from "./use-board-events";

// Custom MIME types: a file dragged in from the desktop, or a drive node from
// the listing, carries neither, so the board's drop targets ignore them.
// eslint-disable-next-line lingui/no-unlocalized-strings -- MIME data transfer type
const CARD_DATA_TYPE = "application/x-pphub-kanban-card";
// eslint-disable-next-line lingui/no-unlocalized-strings -- MIME data transfer type
const COLUMN_DATA_TYPE = "application/x-pphub-kanban-column";

type Over = { kind: "card"; columnId: string; slot: number } | { kind: "column"; slot: number };

function BoardSkeleton() {
  return (
    <LoadingStatus className="flex h-full gap-3 overflow-hidden p-3">
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
    </LoadingStatus>
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
        maxLength={200}
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

// eslint-disable-next-line lingui/no-unlocalized-strings -- storage key, not copy
const HINT_KEY = "pphub.kanban.hint-dismissed";

/** Whether this browser has already dismissed the how-it-works card. */
function hintDismissed(): boolean {
  try {
    return localStorage.getItem(HINT_KEY) === "1";
  } catch {
    return false;
  }
}

/** A short note shown over an empty board; once dismissed it stays away in this browser. */
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
 * their header. The route supplies the header; this fills what is under it.
 */
export default function BoardEditor({ nodeId, subject, thread }: ExtEditorProps) {
  const board = useBoard(nodeId);
  useBoardEvents(nodeId);
  const moveCard = useMoveCard(nodeId);
  const createCard = useCreateCard(nodeId);
  const deleteCard = useDeleteCard(nodeId);
  const createColumn = useCreateColumn(nodeId);
  const updateColumn = useUpdateColumn(nodeId);

  const [drag, setDrag] = useState<DragState | null>(null);
  const [over, setOver] = useState<Over | null>(null);
  const [deleting, setDeleting] = useState<Card | null>(null);
  const [filter, setFilter] = useState<CardFilter>(NO_FILTER);
  const [hintOpen, setHintOpen] = useState(() => !hintDismissed());
  const { t } = useLingui();

  const deletingTitle = deleting?.title ?? "";
  const data = board.data;

  // `/edit/$nodeId?subject=card:<id>` (a notification link) opens that card
  // once, after the snapshot has loaded, and brings its tile into view.
  const opened = useRef<string>(undefined);
  useEffect(() => {
    const id = subject?.startsWith("card:") ? subject.slice("card:".length) : undefined;
    if (!id || !data || opened.current === subject) return;
    opened.current = subject;
    if (!data.cards.some((c) => c.id === id)) return;
    void CardDialog.show({ nodeId, cardId: id, focusThreadId: thread });
    requestAnimationFrame(() =>
      document
        .querySelector(`[data-card-id="${id}"]`)
        ?.scrollIntoView({ block: "nearest", inline: "center" }),
    );
  }, [subject, data, nodeId, thread]);
  const columns = useMemo(() => (data ? sortedColumns(data) : []), [data]);
  const allByColumn = useMemo(
    () => new Map(columns.map((c) => [c.id, data ? cardsIn(data, c.id) : []])),
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
    for (const card of data?.cards ?? []) for (const p of card.assignees) seen.set(p.id, p);
    return [...seen.values()].sort((a, b) => a.name.localeCompare(b.name));
  }, [data]);
  const shown = useMemo(
    () => [...byColumn.values()].reduce((n, cards) => n + cards.length, 0),
    [byColumn],
  );

  if (board.isPending) return <BoardSkeleton />;

  if (!data) {
    return (
      <ExtUnavailable
        what={t`board`}
        off={isAppOff(board.error)}
        onRetry={() => void board.refetch()}
      />
    );
  }

  const canEdit = data.caps.edit;

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

  const moveTo = (card: Card, columnId: string) => {
    const last = allByColumn.get(columnId)?.at(-1);
    moveCard.mutate({ cardId: card.id, drop: { column_id: columnId, after_id: last?.id ?? "" } });
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
        onManageLabels={canEdit ? () => void LabelsDialog.show({ nodeId }) : undefined}
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
          if (!isNoopColumnMove(data, drag.id, afterId)) {
            updateColumn.mutate({ columnId: drag.id, patch: { after_id: afterId } });
          }
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
                    moveCard.mutate({ cardId: drag.id, drop });
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
              onOpenCard={(card) => void CardDialog.show({ nodeId, cardId: card.id })}
              onMoveCard={moveTo}
              onDeleteCard={setDeleting}
              onAddCard={(title) => createCard.mutate({ column_id: column.id, title })}
              onRename={() =>
                void ColumnDialog.show({ nodeId, columnId: column.id, field: "title" })
              }
              onWipLimit={() =>
                void ColumnDialog.show({ nodeId, columnId: column.id, field: "wip_limit" })
              }
              onDelete={() => void DeleteColumnDialog.show({ nodeId, columnId: column.id })}
              onMoveColumn={(afterId) =>
                updateColumn.mutate({ columnId: column.id, patch: { after_id: afterId } })
              }
            />
          );
        })}

        {canEdit && <AddColumn onAdd={(title) => createColumn.mutate({ title })} />}

        <ConfirmDialog
          open={deleting !== null}
          onOpenChange={(open) => !open && setDeleting(null)}
          title={t`Delete this card?`}
          description={t`"${deletingTitle}" will be deleted for everyone who can open this board.`}
          confirmLabel={t`Delete`}
          onConfirm={() => deleting && deleteCard.mutate(deleting.id)}
        />
      </div>
      {canEdit && data.cards.length === 0 && hintOpen && (
        <BoardHint
          onDismiss={() => {
            setHintOpen(false);
            try {
              localStorage.setItem(HINT_KEY, "1");
            } catch {
              // Storage is off: the note comes back next visit, which is harmless.
            }
          }}
        />
      )}
    </div>
  );
}
