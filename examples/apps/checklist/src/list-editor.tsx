import { useState } from "react";
import { Trans, useLingui } from "@lingui/react/macro";
import { ListChecks, Trash2 } from "lucide-react";
import { Button } from "@workspace/ui/components/button";
import { Checkbox } from "@workspace/ui/components/checkbox";
import { Input } from "@workspace/ui/components/input";
import { Skeleton } from "@workspace/ui/components/skeleton";
import { cn } from "@workspace/ui/lib/utils";
import { Unavailable, useApp } from "./bridge/app-provider";
import { useRecords } from "./bridge/use-records";
import { cleanText, ITEMS, itemsOf, MAX_ITEMS, MAX_TEXT, progress } from "./list-state";

/**
 * The checklist of one node: items to tick, add and delete, live for everyone
 * who has it open. The host draws the header; this fills the frame under it.
 */
export function ListEditor() {
  const app = useApp();
  const store = useRecords(app);
  const { t } = useLingui();
  const [text, setText] = useState("");
  const canEdit = app.context.caps.edit;

  if (store.status === "loading") {
    return (
      <div role="status" className="mx-auto flex max-w-xl flex-col gap-2 p-6">
        {[0, 1, 2].map((i) => (
          <Skeleton key={i} className="h-7 w-full" />
        ))}
      </div>
    );
  }

  if (store.status === "error") {
    return (
      <Unavailable
        what={t`checklist`}
        refused={store.refused}
        onRetry={() => void store.reload()}
      />
    );
  }

  const items = itemsOf(store.records);
  const { done, total } = progress(items);
  const trimmed = cleanText(text);

  // A failed write: show the list as it really is, and say so.
  const fail = () => {
    void store.reload();
    // eslint-disable-next-line lingui/no-unlocalized-strings -- toast variant
    void app.ui.toast(t`Could not update the checklist`, "error");
  };

  const add = (value: string) => {
    if (items.length >= MAX_ITEMS) {
      // eslint-disable-next-line lingui/no-unlocalized-strings -- toast variant
      void app.ui.toast(t`A checklist holds at most 500 items`, "error");
      return;
    }
    app.records.create(ITEMS, { text: value, done: false }).then(store.upsert, fail);
  };

  const tick = (id: string, checked: boolean) => {
    // Ticking is the common act: show it at once; a failure reloads.
    store.patch(id, { done: checked });
    app.records.merge(id, { done: checked }).then(store.upsert, fail);
  };

  const remove = (id: string) => {
    store.remove(id);
    app.records.delete(id).catch(fail);
  };

  return (
    <div className="mx-auto flex h-full min-h-0 w-full max-w-xl flex-col gap-3 overflow-y-auto p-6">
      <p className="text-muted-foreground text-xs tabular-nums" aria-live="polite">
        <Trans>
          {done} of {total} done
        </Trans>
      </p>

      {items.length === 0 && (
        <div className="text-muted-foreground flex flex-col items-center gap-1 py-8 text-center text-xs">
          <ListChecks className="size-5" aria-hidden />
          <Trans>Nothing on this list yet</Trans>
        </div>
      )}

      {items.length > 0 && (
        <ul className="flex flex-col divide-y rounded-md border">
          {items.map((item) => (
            <li key={item.id} className="group flex items-center gap-2 px-3 py-1.5">
              <label className="flex min-w-0 flex-1 cursor-pointer items-center gap-2">
                <Checkbox
                  checked={item.done}
                  disabled={!canEdit}
                  onCheckedChange={(checked) => tick(item.id, checked === true)}
                />
                <span
                  className={cn(
                    "min-w-0 text-xs break-words",
                    item.done && "text-muted-foreground line-through",
                  )}
                >
                  {item.text}
                </span>
              </label>
              {canEdit && (
                <Button
                  variant="ghost"
                  size="icon-xs"
                  aria-label={t`Delete item`}
                  className="opacity-0 group-focus-within:opacity-100 group-hover:opacity-100"
                  onClick={() => remove(item.id)}
                >
                  <Trash2 />
                </Button>
              )}
            </li>
          ))}
        </ul>
      )}

      {canEdit && (
        <form
          className="flex gap-2"
          onSubmit={(e) => {
            e.preventDefault();
            if (!trimmed) return;
            add(trimmed);
            setText("");
          }}
        >
          <Input
            value={text}
            maxLength={MAX_TEXT}
            aria-label={t`New item`}
            placeholder={t`Add an item…`}
            onChange={(e) => setText(e.target.value)}
          />
          <Button type="submit" disabled={!trimmed}>
            <Trans>Add</Trans>
          </Button>
        </form>
      )}
    </div>
  );
}
