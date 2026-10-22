import { createModal } from "@buiducnhat/better-modal";
import { Plural, Trans, useLingui } from "@lingui/react/macro";
import { Info, Trash2, TriangleAlert } from "lucide-react";
import { Button } from "@workspace/ui/components/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@workspace/ui/components/dialog";
import { Input } from "@workspace/ui/components/input";
import { Label } from "@workspace/ui/components/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@workspace/ui/components/select";
import { useBoard } from "./board-context";
import { cardsIn, sortedColumns } from "./board-state";
import { LIMITS } from "./rules";
import { useKeyedState } from "./use-keyed-state";

type ColumnProps = { columnId: string; field: "title" | "wip_limit" };

/** Rename a column or set its WIP limit (empty removes the limit). */
export const ColumnDialog = createModal<ColumnProps, null>(
  "kanban-column",
  ({ columnId, field, modal }) => {
    const { t } = useLingui();
    const { board, ops, run } = useBoard();
    const column = board.columns.find((c) => c.id === columnId);

    const key = `${columnId}:${field}:${modal.visible}`;
    const [value, setValue] = useKeyedState(
      key,
      field === "title" ? (column?.title ?? "") : String(column?.wip_limit ?? ""),
    );
    const trimmed = value.trim();
    const limit = Number(trimmed);
    const valid =
      field === "title"
        ? trimmed.length > 0
        : trimmed === "" || (Number.isInteger(limit) && limit > 0);

    const submit = () => {
      if (!column || !valid) return;
      const patch =
        field === "title" ? { title: trimmed } : { wip_limit: trimmed === "" ? null : limit };
      void run(() => ops.updateColumn(columnId, patch), t`Could not update the column`);
      modal.resolve(null);
    };

    return (
      <Dialog open={modal.visible} onOpenChange={(open) => !open && modal.resolve(null)}>
        <DialogContent className="sm:max-w-sm">
          <DialogHeader>
            <DialogTitle>
              {field === "title" ? <Trans>Rename column</Trans> : <Trans>WIP limit</Trans>}
            </DialogTitle>
            {field === "wip_limit" && (
              <DialogDescription>
                <Trans>
                  The most cards this column should hold. It is a warning, not a lock; leave it
                  empty for no limit.
                </Trans>
              </DialogDescription>
            )}
          </DialogHeader>
          <form
            className="flex flex-col gap-3"
            onSubmit={(e) => {
              e.preventDefault();
              submit();
            }}
          >
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="kanban-column-field">
                {field === "title" ? <Trans>Name</Trans> : <Trans>Limit</Trans>}
              </Label>
              <Input
                id="kanban-column-field"
                autoFocus
                value={value}
                maxLength={field === "title" ? LIMITS.columnTitle : 6}
                inputMode={field === "title" ? undefined : "numeric"}
                placeholder={field === "wip_limit" ? t`No limit` : undefined}
                onFocus={(e) => e.currentTarget.select()}
                onChange={(e) => setValue(e.target.value)}
              />
            </div>
            <DialogFooter>
              <Button type="button" variant="outline" onClick={() => modal.resolve(null)}>
                <Trans>Cancel</Trans>
              </Button>
              <Button type="submit" disabled={!valid}>
                <Trans>Save</Trans>
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
    );
  },
);

type DeleteProps = { columnId: string };

/** Delete a column; one that holds cards asks which column receives them. */
export const DeleteColumnDialog = createModal<DeleteProps, null>(
  "kanban-delete-column",
  ({ columnId, modal }) => {
    const { t } = useLingui();
    const { board, ops, run } = useBoard();

    const column = board.columns.find((c) => c.id === columnId);
    const columnTitle = column?.title ?? "";
    const count = cardsIn(board, columnId).length;
    const others = sortedColumns(board).filter((c) => c.id !== columnId);
    const items = others.map((c) => {
      const n = cardsIn(board, c.id).length;
      return {
        value: c.id,
        label: (
          <span className="flex w-full items-center gap-2 pr-3">
            <span aria-hidden className="bg-muted-foreground/60 size-1.5 shrink-0 rounded-full" />
            <span className="truncate">{c.title}</span>
            <span className="text-muted-foreground ml-auto text-[0.625rem]">
              <Plural value={n} one="# card" other="# cards" />
            </span>
          </span>
        ),
      };
    });

    const [target, setTarget] = useKeyedState(`${columnId}:${modal.visible}`, "");
    const moveTo = target || others[0]?.id;
    const blocked = count > 0 && !moveTo;

    return (
      <Dialog open={modal.visible} onOpenChange={(open) => !open && modal.resolve(null)}>
        <DialogContent className="sm:max-w-[420px]">
          <DialogHeader className="flex-row items-center gap-2.5">
            <span className="bg-destructive/10 text-destructive flex size-8 shrink-0 items-center justify-center rounded-full">
              <TriangleAlert className="size-4" />
            </span>
            <div className="flex min-w-0 flex-col gap-0.5">
              <DialogTitle>
                <Trans>Delete column?</Trans>
              </DialogTitle>
              <DialogDescription>
                {count === 0 ? (
                  <Trans>"{columnTitle}" is empty and will be deleted.</Trans>
                ) : blocked ? (
                  <Trans>
                    "{columnTitle}" still has cards and there is no other column to move them to.
                    Add a column first.
                  </Trans>
                ) : (
                  <Trans>"{columnTitle}" will be deleted. Choose where its cards go.</Trans>
                )}
              </DialogDescription>
            </div>
          </DialogHeader>
          {count > 0 && !blocked && (
            <div className="flex flex-col gap-1.5">
              <Label
                htmlFor="kanban-move-to"
                className="text-muted-foreground text-[0.625rem] font-semibold tracking-wider uppercase"
              >
                <Trans>Move its cards to</Trans>
              </Label>
              <Select
                value={moveTo}
                items={items}
                onValueChange={(v) => setTarget((v as string | null) ?? "")}
              >
                <SelectTrigger id="kanban-move-to" className="w-full" aria-label={t`Target column`}>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {items.map((item) => (
                    <SelectItem key={item.value} value={item.value}>
                      {item.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <p className="text-muted-foreground flex items-center gap-1.5 pt-1 text-[0.625rem]">
                <Info className="text-primary size-3.5 shrink-0" />
                <Plural
                  value={count}
                  one="# card will be moved to the end of that column."
                  other="# cards will be moved to the end of that column."
                />
              </p>
            </div>
          )}
          <DialogFooter className="border-t pt-3">
            <Button type="button" variant="outline" onClick={() => modal.resolve(null)}>
              <Trans>Cancel</Trans>
            </Button>
            <Button
              type="button"
              variant="destructive"
              disabled={blocked || !column}
              onClick={() => {
                void run(
                  () => ops.deleteColumn(columnId, count > 0 ? moveTo : undefined),
                  t`Could not delete the column`,
                );
                modal.resolve(null);
              }}
            >
              <Trash2 />
              <Trans>Delete column</Trans>
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    );
  },
);
