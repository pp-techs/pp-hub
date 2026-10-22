import { useEffect, useRef, useState } from "react";
import { Trans, useLingui } from "@lingui/react/macro";
import {
  ArrowLeft,
  ArrowRight,
  Ellipsis,
  GripVertical,
  Pencil,
  Plus,
  Trash2,
  TriangleAlert,
  Gauge,
  Inbox,
} from "lucide-react";
import { Button } from "@workspace/ui/components/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@workspace/ui/components/dropdown-menu";
import { Textarea } from "@workspace/ui/components/textarea";
import { cn } from "@workspace/ui/lib/utils";
import { type Card, type Column, isOverLimit, type Label, slotFromPointer } from "./board-state";
import { CardTile } from "./card-tile";

/** What is being dragged: a card by id, or a whole column by id. */
export type DragState = { kind: "card"; id: string } | { kind: "column"; id: string };

interface BoardColumnProps {
  column: Column;
  /** Every column in order, for the card and column move menus. */
  columns: Column[];
  /** This column's cards that the toolbar filter lets through, in order. */
  cards: Card[];
  /** How many cards the column holds, filtered out or not: what its WIP limit counts. */
  total: number;
  labels: Label[];
  canEdit: boolean;
  drag: DragState | null;
  /** The gap a dragged card would land in, in this column; null when elsewhere. */
  cardSlot: number | null;
  /** A dragged column's drop line sits on this column's left or right edge. */
  columnEdge: "before" | "after" | null;
  onCardSlot: (slot: number) => void;
  onCardDrop: (ids: string[], slot: number) => void;
  onDragLeave: () => void;
  onCardDragStart: (card: Card, e: React.DragEvent) => void;
  onColumnDragStart: (e: React.DragEvent, element: HTMLElement) => void;
  onDragEnd: () => void;
  onOpenCard: (card: Card) => void;
  onMoveCard: (card: Card, columnId: string) => void;
  onDeleteCard: (card: Card) => void;
  onAddCard: (title: string) => void;
  onRename: () => void;
  onWipLimit: () => void;
  onDelete: () => void;
  /** Moves this column: null puts it first, an id puts it after that column. */
  onMoveColumn: (afterId: string | null) => void;
}

function Composer({ onAdd, onClose }: { onAdd: (title: string) => void; onClose: () => void }) {
  const { t } = useLingui();
  const [title, setTitle] = useState("");
  const trimmed = title.trim();

  const submit = () => {
    if (!trimmed) return;
    onAdd(trimmed);
    setTitle("");
  };

  return (
    <form
      className="flex flex-col gap-1.5 p-2 pt-0"
      onSubmit={(e) => {
        e.preventDefault();
        submit();
      }}
    >
      <Textarea
        autoFocus
        rows={2}
        value={title}
        maxLength={500}
        aria-label={t`Card title`}
        placeholder={t`Enter a title for this card…`}
        onChange={(e) => setTitle(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Enter" && !e.shiftKey) {
            e.preventDefault();
            submit();
          } else if (e.key === "Escape") {
            e.stopPropagation();
            onClose();
          }
        }}
      />
      <div className="flex gap-1.5">
        <Button type="submit" size="sm" disabled={!trimmed}>
          <Trans>Add card</Trans>
        </Button>
        <Button type="button" size="sm" variant="ghost" onClick={onClose}>
          <Trans>Cancel</Trans>
        </Button>
      </div>
    </form>
  );
}

export function BoardColumn({
  column,
  columns,
  cards,
  total,
  labels,
  canEdit,
  drag,
  cardSlot,
  columnEdge,
  onCardSlot,
  onCardDrop,
  onDragLeave,
  onCardDragStart,
  onColumnDragStart,
  onDragEnd,
  onOpenCard,
  onMoveCard,
  onDeleteCard,
  onAddCard,
  onRename,
  onWipLimit,
  onDelete,
  onMoveColumn,
}: BoardColumnProps) {
  const { t } = useLingui();
  const sectionRef = useRef<HTMLElement>(null);
  const listRef = useRef<HTMLDivElement>(null);
  const [adding, setAdding] = useState(false);
  const over = isOverLimit(column, total);
  const atLimit = column.wip_limit !== null && total === column.wip_limit;
  const index = columns.findIndex((c) => c.id === column.id);
  const labelById = new Map(labels.map((l) => [l.id, l]));

  // A card added through the composer lands at the bottom: bring it into view.
  const count = useRef(cards.length);
  useEffect(() => {
    if (adding && cards.length > count.current) {
      listRef.current?.scrollTo({ top: listRef.current.scrollHeight });
    }
    count.current = cards.length;
  }, [adding, cards.length]);

  const slotAt = (e: React.DragEvent) => {
    const tiles = listRef.current?.querySelectorAll<HTMLElement>("[data-card-id]") ?? [];
    const mids = Array.from(tiles, (el) => {
      const box = el.getBoundingClientRect();
      return box.top + box.height / 2;
    });
    return slotFromPointer(mids, e.clientY);
  };

  const indicator = (
    <div
      aria-hidden
      className="bg-primary before:bg-primary after:bg-primary pointer-events-none relative h-0.5 rounded before:absolute before:top-1/2 before:-left-0.5 before:size-1.5 before:-translate-y-1/2 before:rounded-full after:absolute after:top-1/2 after:-right-0.5 after:size-1.5 after:-translate-y-1/2 after:rounded-full"
    />
  );

  return (
    <section
      ref={sectionRef}
      data-column-id={column.id}
      aria-label={column.title}
      className="bg-muted/40 relative flex max-h-full w-72 shrink-0 flex-col rounded-md border"
      onDragOver={(e) => {
        if (drag?.kind !== "card") return;
        e.preventDefault();
        e.dataTransfer.dropEffect = "move";
        const slot = slotAt(e);
        if (slot !== cardSlot) onCardSlot(slot);
      }}
      onDragLeave={(e) => {
        if (drag?.kind === "card" && !e.currentTarget.contains(e.relatedTarget as Node | null)) {
          onDragLeave();
        }
      }}
      onDrop={(e) => {
        if (drag?.kind !== "card") return;
        e.preventDefault();
        onCardDrop(
          cards.map((c) => c.id),
          slotAt(e),
        );
      }}
    >
      {columnEdge && (
        <div
          aria-hidden
          className={cn(
            "bg-primary pointer-events-none absolute inset-y-0 z-10 w-0.5 rounded",
            columnEdge === "before" ? "-left-[7px]" : "-right-[7px]",
          )}
        />
      )}

      <header
        draggable={canEdit}
        onDragStart={(e) => sectionRef.current && onColumnDragStart(e, sectionRef.current)}
        onDragEnd={onDragEnd}
        className={cn(
          "flex items-center gap-1.5 border-b px-2 py-1.5",
          canEdit && "cursor-grab active:cursor-grabbing",
          drag?.kind === "column" && drag.id === column.id && "opacity-40",
        )}
      >
        {canEdit && (
          <GripVertical className="text-muted-foreground size-3.5 shrink-0" aria-hidden />
        )}
        <h3 className="min-w-0 truncate text-xs font-semibold">{column.title}</h3>
        <span
          className={cn(
            "inline-flex shrink-0 items-center gap-1 rounded-full border px-1.5 text-[0.625rem] leading-4 tabular-nums",
            over
              ? "border-destructive/30 bg-destructive/10 text-destructive font-medium"
              : atLimit
                ? "border-amber-500/40 bg-amber-500/10 font-medium text-amber-700 dark:text-amber-400"
                : "bg-background text-muted-foreground",
          )}
          title={over ? t`Over the WIP limit` : atLimit ? t`At the WIP limit` : undefined}
        >
          {(over || atLimit) && <TriangleAlert className="size-3" aria-hidden />}
          {column.wip_limit === null ? total : `${total}/${column.wip_limit}`}
          {over && (
            <span className="sr-only">
              <Trans>Over the WIP limit</Trans>
            </span>
          )}
          {atLimit && (
            <span className="sr-only">
              <Trans>At the WIP limit</Trans>
            </span>
          )}
        </span>
        <span className="flex-1" />
        {canEdit && (
          <Button
            variant="ghost"
            size="icon-xs"
            aria-label={t`Add card`}
            // The header drags the column; a click on this must not start that.
            draggable={false}
            onClick={() => setAdding(true)}
          >
            <Plus />
          </Button>
        )}
        {canEdit && (
          <DropdownMenu>
            <DropdownMenuTrigger
              render={
                <Button variant="ghost" size="icon-xs" aria-label={t`Column actions`}>
                  <Ellipsis />
                </Button>
              }
            />
            <DropdownMenuContent align="end">
              <DropdownMenuGroup>
                <DropdownMenuLabel className="text-[0.625rem] tracking-wide uppercase">
                  <Trans>Column actions</Trans>
                </DropdownMenuLabel>
                <DropdownMenuItem onClick={onRename}>
                  <Pencil />
                  <Trans>Rename</Trans>
                </DropdownMenuItem>
                <DropdownMenuItem onClick={onWipLimit}>
                  <Gauge />
                  <Trans>WIP limit…</Trans>
                </DropdownMenuItem>
                {index > 0 && (
                  <DropdownMenuItem onClick={() => onMoveColumn(columns[index - 2]?.id ?? null)}>
                    <ArrowLeft />
                    <Trans>Move left</Trans>
                  </DropdownMenuItem>
                )}
                {index >= 0 && index < columns.length - 1 && (
                  <DropdownMenuItem onClick={() => onMoveColumn(columns[index + 1]!.id)}>
                    <ArrowRight />
                    <Trans>Move right</Trans>
                  </DropdownMenuItem>
                )}
              </DropdownMenuGroup>
              <DropdownMenuSeparator />
              <DropdownMenuItem variant="destructive" onClick={onDelete}>
                <Trash2 />
                <Trans>Delete column</Trans>
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        )}
      </header>

      <div ref={listRef} className="flex min-h-10 flex-1 flex-col gap-2 overflow-y-auto p-2">
        {cards.length === 0 && cardSlot === 0 && indicator}
        {cards.length === 0 && cardSlot === null && (
          <div className="text-muted-foreground flex flex-col items-center gap-0.5 py-3 text-center text-xs">
            <Inbox className="mb-1 size-4" aria-hidden />
            {total > 0 ? (
              <Trans>No matching cards</Trans>
            ) : (
              <>
                <span className="font-medium">
                  <Trans>No cards yet</Trans>
                </span>
                {canEdit && (
                  <span className="text-[0.625rem]">
                    <Trans>Drag cards here, or add one below</Trans>
                  </span>
                )}
              </>
            )}
          </div>
        )}
        {cards.map((card, i) => (
          <div key={card.id} className="relative">
            {cardSlot === i && (
              <div
                aria-hidden
                className="bg-primary pointer-events-none absolute inset-x-0 -top-1.5 z-10 h-0.5 rounded"
              />
            )}
            <CardTile
              card={card}
              labels={card.label_ids.flatMap((id) => labelById.get(id) ?? [])}
              columns={columns}
              canEdit={canEdit}
              dragging={drag?.kind === "card" && drag.id === card.id}
              onOpen={() => onOpenCard(card)}
              onMoveTo={(columnId) => onMoveCard(card, columnId)}
              onDelete={() => onDeleteCard(card)}
              onDragStart={(e) => onCardDragStart(card, e)}
              onDragEnd={onDragEnd}
            />
            {cardSlot === cards.length && i === cards.length - 1 && (
              <div
                aria-hidden
                className="bg-primary pointer-events-none absolute inset-x-0 -bottom-1.5 z-10 h-0.5 rounded"
              />
            )}
          </div>
        ))}
      </div>

      {canEdit &&
        (adding ? (
          <Composer onAdd={onAddCard} onClose={() => setAdding(false)} />
        ) : (
          <div className="p-2 pt-0">
            <Button
              variant="ghost"
              className="text-muted-foreground w-full justify-start"
              onClick={() => setAdding(true)}
            >
              <Plus />
              <Trans>Add card</Trans>
            </Button>
          </div>
        ))}
    </section>
  );
}
