import { useState } from "react";
import { Trans, useLingui } from "@lingui/react/macro";
import { ListChecks, Trash2 } from "lucide-react";
import { Button } from "@workspace/ui/components/button";
import { Checkbox } from "@workspace/ui/components/checkbox";
import { Input } from "@workspace/ui/components/input";
import { Skeleton } from "@workspace/ui/components/skeleton";
import { cn } from "@workspace/ui/lib/utils";
import { ExtUnavailable, isAppOff, LoadingStatus } from "@pp-hub/ext-sdk";
import { progress } from "./list-state";
import { useList, useListEvents, useListMutations } from "./queries";

/**
 * The checklist of one node: items to tick, add and delete, live for everyone
 * who has it open. The route supplies the header; this fills what is under it.
 */
export default function ListEditor({ nodeId }: { nodeId: string }) {
  const list = useList(nodeId);
  useListEvents(nodeId);
  const { add, update, remove } = useListMutations(nodeId);
  const { t } = useLingui();
  const [text, setText] = useState("");

  if (list.isPending) {
    return (
      <LoadingStatus className="mx-auto flex max-w-xl flex-col gap-2 p-6">
        {[0, 1, 2].map((i) => (
          <Skeleton key={i} className="h-7 w-full" />
        ))}
      </LoadingStatus>
    );
  }

  if (!list.data) {
    return (
      <ExtUnavailable
        what={t`checklist`}
        off={isAppOff(list.error)}
        onRetry={() => void list.refetch()}
      />
    );
  }

  const { items, caps } = list.data;
  const { done, total } = progress(items);
  const trimmed = text.trim();

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

      <ul className="flex flex-col divide-y rounded-md border">
        {items.map((item) => (
          <li key={item.id} className="group flex items-center gap-2 px-3 py-1.5">
            <label className="flex min-w-0 flex-1 cursor-pointer items-center gap-2">
              <Checkbox
                checked={item.done}
                disabled={!caps.edit}
                onCheckedChange={(checked) =>
                  update.mutate({ id: item.id, done: checked === true })
                }
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
            {caps.edit && (
              <Button
                variant="ghost"
                size="icon-xs"
                aria-label={t`Delete item`}
                className="opacity-0 group-focus-within:opacity-100 group-hover:opacity-100"
                onClick={() => remove.mutate(item.id)}
              >
                <Trash2 />
              </Button>
            )}
          </li>
        ))}
      </ul>

      {caps.edit && (
        <form
          className="flex gap-2"
          onSubmit={(e) => {
            e.preventDefault();
            if (!trimmed) return;
            add.mutate(trimmed);
            setText("");
          }}
        >
          <Input
            value={text}
            maxLength={500}
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
