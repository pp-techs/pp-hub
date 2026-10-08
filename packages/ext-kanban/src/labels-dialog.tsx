import { useState } from "react";
import { Plural, Trans, useLingui } from "@lingui/react/macro";
import { Check, Plus, Tag, Trash2 } from "lucide-react";
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
import { Popover, PopoverContent, PopoverTrigger } from "@workspace/ui/components/popover";
import { ConfirmDialog, createModal, useKeyedState } from "@pp-hub/ext-sdk";
import type { Label } from "./api";
import { LABEL_COLORS } from "./board-state";
import { CAPTION } from "./label-ui";
import { useBoard, useCreateLabel, useDeleteLabel, useUpdateLabel } from "./queries";

type Props = { nodeId: string };

/** The fixed palette as square swatches; the chosen one carries a check. */
function Swatches({ value, onChange }: { value: string; onChange: (color: string) => void }) {
  return (
    <div className="grid w-fit grid-cols-5 gap-2">
      {LABEL_COLORS.map((color) => {
        const on = value.toLowerCase() === color;
        return (
          <button
            key={color}
            type="button"
            aria-label={color}
            aria-pressed={on}
            onClick={() => onChange(color)}
            className="flex size-6 items-center justify-center rounded-md text-white outline-offset-2 focus-visible:outline-2 focus-visible:outline-ring"
            style={{ backgroundColor: color }}
          >
            {on && <Check className="size-3.5" />}
          </button>
        );
      })}
    </div>
  );
}

function LabelRow({
  nodeId,
  label,
  canEdit,
  count,
}: {
  nodeId: string;
  label: Label;
  canEdit: boolean;
  count: number;
}) {
  const { t } = useLingui();
  const labelName = label.name;
  const update = useUpdateLabel(nodeId);
  const remove = useDeleteLabel(nodeId);
  const [name, setName] = useKeyedState(label.name, label.name);
  const [confirming, setConfirming] = useState(false);

  const saveName = () => {
    const next = name.trim();
    if (!next) setName(label.name);
    else if (next !== label.name) update.mutate({ labelId: label.id, patch: { name: next } });
  };

  return (
    <li className="hover:bg-muted/50 flex items-center gap-2 rounded-md px-1.5 py-1">
      <Popover>
        <PopoverTrigger
          disabled={!canEdit}
          aria-label={t`Label colour`}
          className="size-5 shrink-0 rounded-[5px] disabled:opacity-100"
          style={{ backgroundColor: label.color }}
        />
        <PopoverContent align="start" className="w-auto p-2">
          <Swatches
            value={label.color}
            onChange={(color) => update.mutate({ labelId: label.id, patch: { color } })}
          />
        </PopoverContent>
      </Popover>
      <Input
        value={name}
        maxLength={60}
        disabled={!canEdit}
        aria-label={t`Label name`}
        className="h-7 flex-1 border-transparent bg-transparent shadow-none dark:bg-transparent"
        onChange={(e) => setName(e.target.value)}
        onBlur={saveName}
        onKeyDown={(e) => e.key === "Enter" && e.currentTarget.blur()}
      />
      <span className="bg-muted text-muted-foreground shrink-0 rounded-full px-2 py-0.5 text-[0.625rem]">
        <Plural value={count} one="# card" other="# cards" />
      </span>
      {canEdit && (
        <Button
          type="button"
          variant="ghost"
          size="icon-sm"
          aria-label={t`Delete label ${labelName}`}
          onClick={() => setConfirming(true)}
        >
          <Trash2 />
        </Button>
      )}
      <ConfirmDialog
        open={confirming}
        onOpenChange={setConfirming}
        title={t`Delete label?`}
        description={t`"${labelName}" will be removed from every card that has it.`}
        confirmLabel={t`Delete`}
        onConfirm={() => remove.mutate(label.id)}
      />
    </li>
  );
}

/** A name and a palette colour; `onCreated` hears the label the server made. */
export function NewLabelForm({
  nodeId,
  onCreated,
}: {
  nodeId: string;
  onCreated?: (label: Label) => void;
}) {
  const { t } = useLingui();
  const create = useCreateLabel(nodeId);
  const [name, setName] = useState("");
  const [color, setColor] = useState<string>(LABEL_COLORS[5]);
  const trimmed = name.trim();

  return (
    <form
      className="bg-muted/40 flex flex-col gap-2.5 rounded-lg border p-3"
      onSubmit={(e) => {
        e.preventDefault();
        if (!trimmed) return;
        create.mutate(
          { name: trimmed, color },
          {
            onSuccess: (label) => {
              setName("");
              onCreated?.(label);
            },
          },
        );
      }}
    >
      <span className={CAPTION}>
        <Trans>Create new label</Trans>
      </span>
      <div className="flex items-center gap-2">
        <Input
          value={name}
          maxLength={60}
          aria-label={t`New label name`}
          placeholder={t`New label`}
          onChange={(e) => setName(e.target.value)}
        />
        <Button type="submit" size="sm" disabled={!trimmed || create.isPending}>
          <Plus />
          <Trans>Add</Trans>
        </Button>
      </div>
      <div className="flex flex-col gap-1.5">
        <span className="text-muted-foreground text-[0.625rem]">
          <Trans>Select color tag</Trans>
        </span>
        <Swatches value={color} onChange={setColor} />
      </div>
    </form>
  );
}

/** Create, recolour, rename and delete the board's labels. Resolves nothing; it edits live. */
export const LabelsDialog = createModal<Props, null>("kanban-labels", ({ nodeId, modal }) => {
  const board = useBoard(nodeId);
  const canEdit = board.data?.caps.edit ?? false;
  const labels = board.data?.labels ?? [];
  const labelCount = labels.length;
  const uses = new Map<string, number>();
  for (const card of board.data?.cards ?? []) {
    for (const id of card.label_ids) uses.set(id, (uses.get(id) ?? 0) + 1);
  }

  return (
    <Dialog open={modal.visible} onOpenChange={(open) => !open && modal.resolve(null)}>
      <DialogContent className="sm:max-w-[480px]">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Tag className="text-muted-foreground size-4" />
            <Trans>Manage labels</Trans>
          </DialogTitle>
          <DialogDescription>
            <Trans>Labels are shared by every card on this board.</Trans>
          </DialogDescription>
        </DialogHeader>
        <div className="flex max-h-[60vh] flex-col gap-3 overflow-y-auto">
          <div className="flex flex-col gap-1.5">
            <span className={CAPTION}>
              <Trans>Board labels ({labelCount})</Trans>
            </span>
            {labels.length > 0 ? (
              <ul className="flex flex-col gap-1">
                {labels.map((label) => (
                  <LabelRow
                    key={label.id}
                    nodeId={nodeId}
                    label={label}
                    canEdit={canEdit}
                    count={uses.get(label.id) ?? 0}
                  />
                ))}
              </ul>
            ) : (
              <p className="text-muted-foreground text-xs">
                <Trans>No labels yet.</Trans>
              </p>
            )}
          </div>
          {canEdit && <NewLabelForm nodeId={nodeId} />}
        </div>
        <DialogFooter className="border-t pt-3">
          <Button type="button" onClick={() => modal.resolve(null)}>
            <Trans>Done</Trans>
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
});
