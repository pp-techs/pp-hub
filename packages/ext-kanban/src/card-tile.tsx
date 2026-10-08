import { Trans, useLingui } from "@lingui/react/macro";
import {
  CalendarDays,
  Ellipsis,
  ExternalLink,
  Link2,
  MessageSquare,
  MoveRight,
  Trash2,
} from "lucide-react";
import {
  Avatar,
  AvatarFallback,
  AvatarGroup,
  AvatarGroupCount,
} from "@workspace/ui/components/avatar";
import { Button } from "@workspace/ui/components/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuSub,
  DropdownMenuSubContent,
  DropdownMenuSubTrigger,
  DropdownMenuTrigger,
} from "@workspace/ui/components/dropdown-menu";
import { cn } from "@workspace/ui/lib/utils";
import { useLocale } from "@pp-hub/ext-sdk";
import type { Card, Column, Label, Person } from "./api";
import { formatDue, isOverdue } from "./board-state";
import { LabelChip } from "./label-ui";

const MAX_AVATARS = 3;

/** The two letters a person's avatar shows, as `UserAvatar` picks them. */
const initials = (p: Person) => (p.name || p.email || "?").slice(0, 2).toUpperCase();

interface CardTileProps {
  card: Card;
  labels: Label[];
  /** Every column, in order; the card's own is left out of the move menu. */
  columns: Column[];
  canEdit: boolean;
  dragging: boolean;
  onOpen: () => void;
  onMoveTo: (columnId: string) => void;
  onDelete: () => void;
  onDragStart: (e: React.DragEvent) => void;
  onDragEnd: () => void;
}

export function CardTile({
  card,
  labels,
  columns,
  canEdit,
  dragging,
  onOpen,
  onMoveTo,
  onDelete,
  onDragStart,
  onDragEnd,
}: CardTileProps) {
  const { t } = useLingui();
  const locale = useLocale();
  const overdue = card.due_at !== null && isOverdue(card.due_at);
  const targets = columns.filter((c) => c.id !== card.column_id);

  const actions = canEdit && (
    // A portal's events still bubble through React: without this a click
    // on a menu item would also open the card.
    <span
      className="-mt-1 -mr-1 shrink-0"
      onClick={(e) => e.stopPropagation()}
      onKeyDown={(e) => e.stopPropagation()}
    >
      <DropdownMenu>
        <DropdownMenuTrigger
          render={
            <Button
              variant="ghost"
              size="icon-xs"
              aria-label={t`Card actions`}
              className="opacity-0 group-focus-within/card:opacity-100 group-hover/card:opacity-100 data-popup-open:opacity-100"
            >
              <Ellipsis />
            </Button>
          }
        />
        <DropdownMenuContent align="end">
          <DropdownMenuItem onClick={onOpen}>
            <ExternalLink />
            <Trans>Open</Trans>
          </DropdownMenuItem>
          {targets.length > 0 && (
            <DropdownMenuSub>
              <DropdownMenuSubTrigger>
                <MoveRight />
                <Trans>Move to column…</Trans>
              </DropdownMenuSubTrigger>
              <DropdownMenuSubContent>
                {targets.map((column) => (
                  <DropdownMenuItem key={column.id} onClick={() => onMoveTo(column.id)}>
                    {column.title}
                  </DropdownMenuItem>
                ))}
              </DropdownMenuSubContent>
            </DropdownMenuSub>
          )}
          <DropdownMenuSeparator />
          <DropdownMenuItem variant="destructive" onClick={onDelete}>
            <Trash2 />
            <Trans>Delete card</Trans>
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
    </span>
  );

  return (
    <div
      data-card-id={card.id}
      data-dragging={dragging}
      draggable={canEdit}
      onDragStart={onDragStart}
      onDragEnd={onDragEnd}
      onClick={onOpen}
      className={cn(
        "bg-card text-card-foreground group/card hover:border-ring/50 relative flex cursor-pointer flex-col gap-1.5 rounded-md border p-2 text-xs shadow-xs transition-colors",
        "data-[dragging=true]:opacity-40",
      )}
    >
      <div className="flex items-start justify-between gap-1">
        <button
          type="button"
          className="focus-visible:ring-ring/50 min-w-0 flex-1 rounded-sm text-left leading-snug font-medium break-words outline-none focus-visible:ring-2"
        >
          {card.title}
        </button>
        {actions}
      </div>

      {(labels.length > 0 ||
        card.due_at ||
        card.assignees.length > 0 ||
        card.comment_count > 0 ||
        card.link_ids.length > 0) && (
        <div className="mt-0.5 flex items-center justify-between gap-2 border-t border-dashed pt-1.5">
          <div className="flex min-w-0 flex-wrap items-center gap-1.5">
            {labels.map((label) => (
              <LabelChip key={label.id} label={label} />
            ))}
            {card.due_at ? (
              <span
                className={cn(
                  "inline-flex items-center gap-1 rounded border px-1 py-px text-[0.625rem]",
                  overdue
                    ? "border-destructive/30 bg-destructive/10 text-destructive"
                    : "text-muted-foreground",
                )}
                title={overdue ? t`Overdue` : t`Due date`}
              >
                <CalendarDays className="size-3" />
                {formatDue(card.due_at, locale)}
                {overdue && (
                  <span className="sr-only">
                    <Trans>Overdue</Trans>
                  </span>
                )}
              </span>
            ) : null}
            {card.comment_count > 0 && (
              <span
                className="text-muted-foreground inline-flex items-center gap-1 text-[0.625rem]"
                title={t`Comments`}
              >
                <MessageSquare className="size-3" />
                {card.comment_count}
                <span className="sr-only">
                  <Trans>Comments</Trans>
                </span>
              </span>
            )}
            {card.link_ids.length > 0 && (
              <span
                className="text-muted-foreground inline-flex items-center gap-1 text-[0.625rem]"
                title={t`Linked items`}
              >
                <Link2 className="size-3" />
                {card.link_ids.length}
                <span className="sr-only">
                  <Trans>Linked items</Trans>
                </span>
              </span>
            )}
          </div>
          {card.assignees.length > 0 && (
            <AvatarGroup>
              {card.assignees.slice(0, MAX_AVATARS).map((person) => (
                <Avatar key={person.id} size="sm" title={person.name} aria-label={person.name}>
                  <AvatarFallback className="text-[0.625rem]">{initials(person)}</AvatarFallback>
                </Avatar>
              ))}
              {card.assignees.length > MAX_AVATARS && (
                <AvatarGroupCount>+{card.assignees.length - MAX_AVATARS}</AvatarGroupCount>
              )}
            </AvatarGroup>
          )}
        </div>
      )}
    </div>
  );
}
