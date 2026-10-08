import { useState } from "react";
import { Trans, useLingui } from "@lingui/react/macro";
import { ExternalLink, Lock, Plus, X } from "lucide-react";
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
import { NodeIcon, useDebouncedValue, useNode, useNodeSearch } from "@pp-hub/ext-sdk";
import type { Link } from "./api";

/** The server's cap on links per card. */
export const MAX_LINKS = 20;

/** One linked item: a name that opens it in a new tab, or a nameless "No access" chip. */
export function LinkItem({ link, onRemove }: { link: Link; onRemove?: () => void }) {
  const { t } = useLingui();
  const name = link.name ?? "";
  return (
    <span className="flex min-h-9 w-full items-center gap-2 rounded-lg border px-2.5 py-1 text-xs">
      {link.accessible ? (
        <>
          <NodeIcon
            node={{
              id: link.id,
              space_id: link.space_id ?? "",
              // eslint-disable-next-line lingui/no-unlocalized-strings -- node kind
              kind: link.kind ?? "file",
              ext_kind: link.ext_kind,
              name,
            }}
          />
          <span className="min-w-0 flex-1 truncate font-medium">{name}</span>
          <a
            href={`/open/${link.id}`}
            target="_blank"
            rel="noreferrer"
            className="text-muted-foreground hover:text-foreground"
            aria-label={t`Open ${name}`}
          >
            <ExternalLink className="size-3.5" />
          </a>
        </>
      ) : (
        <span className="text-muted-foreground flex min-w-0 flex-1 items-center gap-2">
          <Lock className="size-3.5" />
          <Trans>No access</Trans>
        </span>
      )}
      {onRemove && (
        <button
          type="button"
          className="text-muted-foreground hover:text-foreground"
          aria-label={link.accessible ? t`Remove link ${name}` : t`Remove link`}
          onClick={onRemove}
        >
          <X className="size-3.5" />
        </button>
      )}
    </span>
  );
}

interface LinkPickerProps {
  nodeId: string;
  value: Link[];
  onChange: (links: Link[]) => void;
}

/** Add a link: a name search over the board's space. */
export function LinkPicker({ nodeId, value, onChange }: LinkPickerProps) {
  const { t } = useLingui();
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const settled = useDebouncedValue(query.trim());
  const board = useNode(nodeId);
  const spaceId = board.data?.space_id;
  const results = useNodeSearch(settled, { spaceId });
  const linked = new Set(value.map((l) => l.id));
  const hits = (results.data ?? []).filter((n) => n.id !== nodeId && !linked.has(n.id));
  const full = value.length >= MAX_LINKS;

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger
        render={
          <Button
            type="button"
            variant="outline"
            size="xs"
            className="border-dashed"
            disabled={full || !spaceId}
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
              ) : results.isFetching ? (
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
                  onSelect={() =>
                    onChange([
                      ...value,
                      {
                        id: node.id,
                        accessible: true,
                        name: node.name,
                        kind: node.kind,
                        ext_kind: node.ext_kind,
                        space_id: node.space_id,
                      },
                    ])
                  }
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
