import { useEffect, useState } from "react";
import { Trans, useLingui } from "@lingui/react/macro";
import {
  Box,
  ExternalLink,
  File,
  Folder,
  ListChecks,
  Lock,
  Plus,
  SquareKanban,
  X,
} from "lucide-react";
import type { LinkTarget, NodeHit } from "@pp-hub/app-sdk";
import { Button } from "@workspace/ui/components/button";
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from "@workspace/ui/components/command";
import { Popover, PopoverContent, PopoverTrigger } from "@workspace/ui/components/popover";
import { useBoard } from "./board-context";
import { LIMITS } from "./rules";
import { useDebounced } from "./use-debounced";

/** A node's glyph by kind; an app's node by its kind's app. */
export function NodeIcon({ node }: { node: Pick<LinkTarget, "kind" | "ext_kind"> }) {
  const className = "text-muted-foreground size-3.5 shrink-0";
  if (node.kind === "dir") return <Folder className={className} aria-hidden />;
  if (node.kind === "file") return <File className={className} aria-hidden />;
  // eslint-disable-next-line lingui/no-unlocalized-strings -- node kinds
  if (node.ext_kind === "kanban.board") return <SquareKanban className={className} aria-hidden />;
  // eslint-disable-next-line lingui/no-unlocalized-strings -- node kinds
  if (node.ext_kind === "checklist.list") return <ListChecks className={className} aria-hidden />;
  return <Box className={className} aria-hidden />;
}

/**
 * One linked item: a name that opens it, or a nameless "No access" chip for a
 * node the viewer cannot open (or that is gone). `target` undefined is one not
 * resolved yet.
 */
export function LinkItem({
  target,
  onRemove,
}: {
  target: LinkTarget | null | undefined;
  onRemove?: () => void;
}) {
  const { t } = useLingui();
  const { app } = useBoard();
  const name = target?.name ?? "";
  return (
    <span className="flex min-h-9 w-full items-center gap-2 rounded-lg border px-2.5 py-1 text-xs">
      {target ? (
        <>
          <NodeIcon node={target} />
          <span className="min-w-0 flex-1 truncate font-medium">{name}</span>
          <button
            type="button"
            className="text-muted-foreground hover:text-foreground"
            aria-label={t`Open ${name}`}
            onClick={() => void app.nodes.open(target.id)}
          >
            <ExternalLink className="size-3.5" />
          </button>
        </>
      ) : (
        <span className="text-muted-foreground flex min-w-0 flex-1 items-center gap-2">
          <Lock className="size-3.5" />
          {target === null && <Trans>No access</Trans>}
        </span>
      )}
      {onRemove && (
        <button
          type="button"
          className="text-muted-foreground hover:text-foreground"
          aria-label={target ? t`Remove link ${name}` : t`Remove link`}
          onClick={onRemove}
        >
          <X className="size-3.5" />
        </button>
      )}
    </span>
  );
}

/** Name search in the board's space, for a settled query of at least two letters. */
function useNodeSearch(query: string) {
  const { app } = useBoard();
  const tooShort = query.length < 2;
  const [state, setState] = useState<{ query: string; hits: NodeHit[] } | null>(null);
  useEffect(() => {
    if (tooShort) return;
    let live = true;
    app.nodes.search(query).then(
      (hits) => live && setState({ query, hits }),
      () => live && setState({ query, hits: [] }),
    );
    return () => {
      live = false;
    };
  }, [app, query, tooShort]);
  return {
    tooShort,
    fetching: !tooShort && state?.query !== query,
    hits: !tooShort && state?.query === query ? state.hits : [],
  };
}

/** Add a link: a name search over the board's space. `value` holds node ids. */
export function LinkPicker({
  value,
  onChange,
}: {
  value: string[];
  onChange: (ids: string[]) => void;
}) {
  const { t } = useLingui();
  const { app, rememberLinks } = useBoard();
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const results = useNodeSearch(useDebounced(query.trim()));
  const linked = new Set(value);
  const boardId = app.context.node.id;
  const hits = results.hits.filter((n) => n.id !== boardId && !linked.has(n.id));

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger
        render={
          <Button
            type="button"
            variant="outline"
            size="xs"
            className="border-dashed"
            disabled={value.length >= LIMITS.links}
          >
            <Plus />
            <Trans>Add link</Trans>
          </Button>
        }
      />
      <PopoverContent align="start" className="w-80 p-0">
        <Command shouldFilter={false}>
          <CommandInput value={query} onValueChange={setQuery} placeholder={t`Search files…`} />
          <CommandList>
            <CommandEmpty>
              {results.tooShort ? (
                <Trans>Type at least two letters</Trans>
              ) : results.fetching ? (
                <Trans>Searching…</Trans>
              ) : (
                <Trans>Nothing found</Trans>
              )}
            </CommandEmpty>
            <CommandGroup>
              {hits.map((node) => (
                <CommandItem
                  key={node.id}
                  value={node.id}
                  onSelect={() => {
                    rememberLinks([node]);
                    onChange([...value, node.id]);
                  }}
                >
                  <NodeIcon node={node} />
                  <span className="flex min-w-0 flex-col">
                    <span className="truncate">{node.name}</span>
                    {node.parent_name && (
                      <span className="text-muted-foreground truncate text-[0.625rem]">
                        {node.parent_name}
                      </span>
                    )}
                  </span>
                </CommandItem>
              ))}
            </CommandGroup>
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  );
}
