import { useEffect, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { Trans, useLingui } from "@lingui/react/macro";
import { Check, Plus, Trash2, X } from "lucide-react";
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
import { Label as FieldLabel } from "@workspace/ui/components/label";
import { Popover, PopoverContent, PopoverTrigger } from "@workspace/ui/components/popover";
import { Textarea } from "@workspace/ui/components/textarea";
import { cn } from "@workspace/ui/lib/utils";
import { ConfirmDialog, createModal, ThreadList, useKeyedState } from "@pp-hub/ext-sdk";
import type { Card, Link, Person } from "./api";
import { dueFromInput, dueToInput, isValidDay, type LocalCardPatch } from "./board-state";
import { CAPTION } from "./label-ui";
import { LabelsDialog, NewLabelForm } from "./labels-dialog";
import { LinkItem, LinkPicker } from "./link-picker";
import { PeoplePicker } from "./people-picker";
import { boardKey, useBoard, useDeleteCard, useUpdateCard } from "./queries";

type Props = {
  nodeId: string;
  cardId: string;
  /** A thread to scroll to, from a comment notification. */
  focusThreadId?: string;
};

const sameIds = (a: string[], b: string[]) =>
  a.length === b.length && a.every((id) => b.includes(id));

/** What the form changed compared to the card; empty when nothing did. */
function diff(
  card: Card,
  form: {
    title: string;
    description: string;
    due: string;
    people: Person[];
    labels: string[];
    links: Link[];
  },
): LocalCardPatch {
  const patch: LocalCardPatch = {};
  if (form.title !== card.title) patch.title = form.title;
  if (form.description !== card.description) patch.description = form.description;
  const due = card.due_at ? dueToInput(card.due_at) : "";
  if (form.due !== due) {
    const next = dueFromInput(form.due);
    if (next !== undefined) patch.due_at = next;
  }
  if (
    !sameIds(
      form.people.map((p) => p.id),
      card.assignees.map((p) => p.id),
    )
  ) {
    patch.assignees = form.people;
  }
  if (!sameIds(form.labels, card.label_ids)) patch.labels = form.labels;
  if (
    !sameIds(
      form.links.map((l) => l.id),
      card.link_ids,
    )
  ) {
    patch.links = form.links;
  }
  return patch;
}

/**
 * Opens a card: title, description, due date, assignees and labels. It saves
 * with the card's current `rev`, so a card someone else changed since the board
 * last updated is refused (409) rather than overwritten.
 */
export const CardDialog = createModal<Props, null>(
  "kanban-card",
  ({ nodeId, cardId, focusThreadId, modal }) => {
    const { t } = useLingui();
    const qc = useQueryClient();
    const board = useBoard(nodeId);
    const update = useUpdateCard(nodeId);
    const remove = useDeleteCard(nodeId);
    const [confirming, setConfirming] = useState(false);

    const card = board.data?.cards.find((c) => c.id === cardId);
    const cardTitle = card?.title ?? "";
    const canEdit = board.data?.caps.edit ?? false;
    const labels = board.data?.labels ?? [];

    const key = `${cardId}:${modal.visible}`;
    const [title, setTitle] = useKeyedState(key, card?.title ?? "");
    const [description, setDescription] = useKeyedState(key, card?.description ?? "");
    const [due, setDue] = useKeyedState(key, card?.due_at ? dueToInput(card.due_at) : "");
    const [people, setPeople] = useKeyedState<string, Person[]>(key, card?.assignees ?? []);
    const [labelIds, setLabelIds] = useKeyedState<string, string[]>(key, card?.label_ids ?? []);
    const [links, setLinks] = useKeyedState<string, Link[]>(
      key,
      card?.links ?? card?.link_ids.map((id) => ({ id, accessible: false })) ?? [],
    );

    // Comments are posted outside the board's cache, so the card's count is
    // refreshed when the dialog closes.
    const close = () => {
      void qc.invalidateQueries({ queryKey: boardKey(nodeId) });
      modal.resolve(null);
    };

    // The card was deleted from under an open dialog.
    const gone = modal.visible && board.isSuccess && !card;
    useEffect(() => {
      if (gone) close();
      // eslint-disable-next-line react-hooks/exhaustive-deps -- close only reads stable refs
    }, [gone]);

    const trimmed = title.trim();
    const patch = card
      ? diff(card, { title: trimmed, description, due, people, labels: labelIds, links })
      : {};
    const dirty = Object.keys(patch).length > 0;

    const toggleLabel = (id: string) =>
      setLabelIds((ids) => (ids.includes(id) ? ids.filter((x) => x !== id) : [...ids, id]));

    const save = () => {
      if (!card || !canEdit || !trimmed) return;
      if (!dirty) {
        close();
        return;
      }
      update.mutate({ card, patch }, { onSuccess: () => close() });
    };

    return (
      <Dialog open={modal.visible} onOpenChange={(open) => !open && close()}>
        <DialogContent className="gap-0 p-0 sm:max-w-[560px]">
          <DialogHeader className="border-b px-5 py-4">
            <DialogTitle className="text-sm font-semibold">
              {canEdit ? <Trans>Edit card</Trans> : <Trans>Card</Trans>}
            </DialogTitle>
            <DialogDescription>
              <Trans>Title, description, due date, assignees and labels of this card.</Trans>
            </DialogDescription>
          </DialogHeader>

          {card && (
            <div className="flex max-h-[70vh] flex-col gap-4 overflow-y-auto px-5 py-4">
              <form
                id="kanban-card-form"
                className="flex flex-col gap-4"
                onSubmit={(e) => {
                  e.preventDefault();
                  save();
                }}
              >
                <div className="flex flex-col gap-1.5">
                  <FieldLabel htmlFor="kanban-card-title" className={CAPTION}>
                    <Trans>Title</Trans>
                  </FieldLabel>
                  <Input
                    id="kanban-card-title"
                    value={title}
                    maxLength={500}
                    readOnly={!canEdit}
                    onChange={(e) => setTitle(e.target.value)}
                  />
                </div>

                <div className="flex flex-col gap-1.5">
                  <FieldLabel htmlFor="kanban-card-description" className={CAPTION}>
                    <Trans>Description</Trans>
                  </FieldLabel>
                  <Textarea
                    id="kanban-card-description"
                    value={description}
                    rows={5}
                    readOnly={!canEdit}
                    placeholder={canEdit ? t`Add a more detailed description…` : undefined}
                    onChange={(e) => setDescription(e.target.value)}
                  />
                </div>

                <div className="flex flex-col gap-1.5">
                  <FieldLabel htmlFor="kanban-card-due" className={CAPTION}>
                    <Trans>Due date</Trans>
                  </FieldLabel>
                  <div className="flex items-center gap-1.5">
                    <Input
                      id="kanban-card-due"
                      type="date"
                      className="w-44"
                      value={due}
                      min="1970-01-01"
                      max="9999-12-31"
                      aria-invalid={due !== "" && !isValidDay(due)}
                      readOnly={!canEdit}
                      onChange={(e) => setDue(e.target.value)}
                    />
                    {canEdit && due && (
                      <Button
                        type="button"
                        variant="ghost"
                        size="icon"
                        aria-label={t`Clear due date`}
                        onClick={() => setDue("")}
                      >
                        <X />
                      </Button>
                    )}
                  </div>
                </div>

                <div className="flex flex-col gap-1.5">
                  <FieldLabel className={CAPTION}>
                    <Trans>Assignees</Trans>
                  </FieldLabel>
                  <PeoplePicker
                    nodeId={nodeId}
                    value={people}
                    onChange={setPeople}
                    disabled={!canEdit}
                  />
                </div>

                <div className="flex flex-col gap-1.5">
                  <FieldLabel className={CAPTION}>
                    <Trans>Labels</Trans>
                  </FieldLabel>
                  <div className="flex flex-wrap items-center gap-1.5">
                    {labels.map((label) => {
                      const on = labelIds.includes(label.id);
                      return (
                        <button
                          key={label.id}
                          type="button"
                          disabled={!canEdit}
                          aria-pressed={on}
                          onClick={() => toggleLabel(label.id)}
                          className={cn(
                            "text-foreground inline-flex max-w-full items-center gap-1 rounded-full border px-2 py-0.5 text-xs outline-offset-2 focus-visible:outline-2 focus-visible:outline-ring",
                            !on && "text-muted-foreground hover:text-foreground",
                          )}
                          style={
                            on
                              ? {
                                  borderColor: `${label.color}99`,
                                  backgroundColor: `${label.color}26`,
                                }
                              : undefined
                          }
                        >
                          {on ? (
                            <Check className="size-3 shrink-0" />
                          ) : (
                            <span
                              aria-hidden
                              className="size-2 shrink-0 rounded-full"
                              style={{ backgroundColor: label.color }}
                            />
                          )}
                          <span className="truncate">{label.name}</span>
                        </button>
                      );
                    })}
                    {canEdit && (
                      <>
                        <Popover>
                          <PopoverTrigger
                            render={
                              <Button
                                type="button"
                                variant="outline"
                                size="xs"
                                className="rounded-full border-dashed"
                              >
                                <Plus />
                                <Trans>New label</Trans>
                              </Button>
                            }
                          />
                          <PopoverContent align="start" className="w-72 p-2">
                            <NewLabelForm
                              nodeId={nodeId}
                              onCreated={(label) => setLabelIds((ids) => [...ids, label.id])}
                            />
                          </PopoverContent>
                        </Popover>
                        <Button
                          type="button"
                          variant="ghost"
                          size="xs"
                          className="text-muted-foreground"
                          onClick={() => void LabelsDialog.show({ nodeId })}
                        >
                          <Trans>Manage labels</Trans>
                        </Button>
                      </>
                    )}
                    {!canEdit && labels.length === 0 && (
                      <span className="text-muted-foreground text-xs">
                        <Trans>No labels</Trans>
                      </span>
                    )}
                  </div>
                </div>

                <div className="flex flex-col gap-1.5">
                  <FieldLabel className={CAPTION}>
                    <Trans>Linked items</Trans>
                  </FieldLabel>
                  <div className="flex flex-col items-start gap-1.5">
                    {links.map((link) => (
                      <LinkItem
                        key={link.id}
                        link={link}
                        onRemove={
                          canEdit
                            ? () => setLinks((ls) => ls.filter((l) => l.id !== link.id))
                            : undefined
                        }
                      />
                    ))}
                    {canEdit && <LinkPicker nodeId={nodeId} value={links} onChange={setLinks} />}
                    {!canEdit && links.length === 0 && (
                      <span className="text-muted-foreground text-xs">
                        <Trans>No linked items</Trans>
                      </span>
                    )}
                  </div>
                </div>
              </form>

              {/* Discussion on this card; polled while the dialog is open. */}
              {modal.visible && (
                <div className="flex flex-col gap-1.5 border-t pt-4">
                  <FieldLabel className={CAPTION}>
                    <Trans>Discussion</Trans>
                  </FieldLabel>
                  <ThreadList
                    nodeId={nodeId}
                    subject={`card:${card.id}`}
                    canComment={board.data?.caps.view ?? false}
                    canResolve={canEdit}
                    focusThreadId={focusThreadId}
                  />
                </div>
              )}
            </div>
          )}

          {card && (
            <DialogFooter className="border-t px-5 py-3 sm:justify-between">
              {canEdit ? (
                <Button
                  type="button"
                  variant="ghost"
                  className="text-destructive hover:text-destructive"
                  onClick={() => setConfirming(true)}
                >
                  <Trash2 />
                  <Trans>Delete card</Trans>
                </Button>
              ) : (
                <span />
              )}
              <div className="flex gap-2">
                <Button type="button" variant="outline" onClick={() => close()}>
                  {canEdit ? <Trans>Cancel</Trans> : <Trans>Close</Trans>}
                </Button>
                {canEdit && (
                  <Button
                    type="submit"
                    form="kanban-card-form"
                    disabled={
                      !trimmed || !dirty || (due !== "" && !isValidDay(due)) || update.isPending
                    }
                  >
                    <Trans>Save</Trans>
                  </Button>
                )}
              </div>
            </DialogFooter>
          )}

          <ConfirmDialog
            open={confirming}
            onOpenChange={setConfirming}
            title={t`Delete this card?`}
            description={t`"${cardTitle}" will be deleted for everyone who can open this board.`}
            confirmLabel={t`Delete`}
            onConfirm={() => {
              remove.mutate(cardId);
              close();
            }}
          />
        </DialogContent>
      </Dialog>
    );
  },
);
