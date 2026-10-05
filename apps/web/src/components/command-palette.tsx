import { Fragment, useEffect, useMemo, useState } from "react";
import { useNavigate } from "@tanstack/react-router";
import { Plural, Trans, useLingui } from "@lingui/react/macro";
import { msg } from "@lingui/core/macro";
import type { MessageDescriptor } from "@lingui/core";
import { ArrowRight, CornerDownLeft, Loader2, Users } from "lucide-react";
import {
  Command,
  CommandDialog,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
  CommandSeparator,
  CommandShortcut,
} from "@workspace/ui/components/command";
import { InputGroupAddon } from "@workspace/ui/components/input-group";
import { ToggleGroup, ToggleGroupItem } from "@workspace/ui/components/toggle-group";
import { cn } from "@workspace/ui/lib/utils";
import {
  opensInEditor,
  openEditor,
  type SearchKind,
  type SearchResult,
  type Space,
} from "@/lib/api";
import { MIN_QUERY_LENGTH, useNodeSearch, useSpaces } from "@/lib/queries";
import { useUiStore } from "@/lib/ui-store";
import { useSpaceLabel } from "@/lib/use-space-label";
import { useDebounced } from "@/lib/use-debounced";
import { highlightSegments } from "@/lib/highlight";
import { PreviewModal } from "@/components/preview-modal";
import { NodeIcon } from "@/components/node-icon";

/**
 * Descriptors rather than strings: this is module scope, evaluated once at
 * import time, so a rendered label here would pin whichever language loaded
 * first and never follow a switch.
 */
const KIND_FILTERS: { value: SearchKind | "all"; label: MessageDescriptor }[] = [
  { value: "all", label: msg`All` },
  { value: "file", label: msg`Files` },
  { value: "dir", label: msg`Folders` },
];

/**
 * ⌘K search across every space the user belongs to.
 *
 * Mounted by the drive shell so the shortcut works from any drive surface.
 * Results come from the server; spaces are matched locally because the full
 * list is already in the cache and a round trip for three rows is waste.
 */
export function CommandPalette() {
  const { t, i18n } = useLingui();
  const open = useUiStore((s) => s.paletteOpen);
  const setOpen = useUiStore((s) => s.setPaletteOpen);
  const navigate = useNavigate();
  const spaces = useSpaces();
  const spaceLabel = useSpaceLabel();

  const [query, setQuery] = useState("");
  const [kind, setKind] = useState<SearchKind | undefined>(undefined);

  const settled = useDebounced(query, 200);
  const search = useNodeSearch(settled, { kind });

  // Every open starts clean; a stale query from last time is never what you want.
  useEffect(() => {
    if (open) {
      setQuery("");
      setKind(undefined);
    }
  }, [open]);

  const close = () => setOpen(false);

  const openNode = (node: SearchResult) => {
    close();
    if (node.kind === "dir") {
      void navigate({
        to: "/drive/$spaceId",
        params: { spaceId: node.space_id },
        search: { dir: node.id },
      });
    } else if (opensInEditor(node)) {
      openEditor(node);
    } else {
      void PreviewModal.show({ node });
    }
  };

  const openSpace = (space: Space) => {
    close();
    void navigate({
      to: "/drive/$spaceId",
      params: { spaceId: space.id },
      search: { dir: undefined },
    });
  };

  // Named local so the extracted message reads `{queryText}` rather than `{0}`.
  const queryText = settled.trim();
  const tooShort = queryText.length < MIN_QUERY_LENGTH;

  const matchedSpaces = useMemo(() => {
    if (tooShort || kind === "file") return [];
    const q = settled.trim().toLowerCase();
    return (spaces.data ?? []).filter((s) => spaceLabel(s).toLowerCase().includes(q));
  }, [spaces.data, settled, kind, tooShort, spaceLabel]);

  const matchedNodes = useMemo(() => {
    if (tooShort) return [];
    return search.data ?? [];
  }, [search.data, tooShort]);

  return (
    <CommandDialog
      open={open}
      onOpenChange={setOpen}
      title={t`Search`}
      description={t`Search files, folders and spaces`}
      className="top-[20%] gap-0 sm:max-w-xl"
    >
      <Command shouldFilter={false} loop className="rounded-none bg-transparent">
        <CommandInput
          autoFocus
          value={query}
          onValueChange={setQuery}
          placeholder={t`Search files, folders and spaces`}
          aria-label={t`Search files, folders and spaces`}
        >
          {search.isFetching && (
            <InputGroupAddon align="inline-end">
              <Loader2 className="text-muted-foreground size-3.5 shrink-0 animate-spin" />
            </InputGroupAddon>
          )}
        </CommandInput>

        <div className="border-border flex items-center border-b px-2.5 py-1.5">
          <ToggleGroup
            variant="outline"
            size="sm"
            value={[kind ?? "all"]}
            onValueChange={(val) => {
              // eslint-disable-next-line lingui/no-unlocalized-strings -- search filter value
              const next = val[0] ?? kind ?? "all";
              setKind(next === "all" ? undefined : (next as SearchKind));
            }}
          >
            {KIND_FILTERS.map((filter) => (
              <ToggleGroupItem
                key={filter.value}
                value={filter.value}
                className="h-6 px-2 text-[11px]"
              >
                {i18n._(filter.label)}
              </ToggleGroupItem>
            ))}
          </ToggleGroup>
        </div>

        <CommandList className="max-h-80">
          <CommandEmpty className={cn(search.isError && "text-destructive")}>
            {tooShort ? (
              <Plural
                value={MIN_QUERY_LENGTH}
                one="Type at least # character to search."
                other="Type at least # characters to search."
              />
            ) : search.isError ? (
              (search.error as Error).message
            ) : search.isFetching && matchedSpaces.length === 0 && matchedNodes.length === 0 ? (
              <Trans>Searching…</Trans>
            ) : (
              <Trans>No matches for “{queryText}”</Trans>
            )}
          </CommandEmpty>

          {!tooShort && !search.isError && (
            <>
              {matchedSpaces.length > 0 && (
                <CommandGroup heading={t`Spaces`}>
                  {matchedSpaces.map((space) => (
                    <CommandItem
                      key={`space:${space.id}`}
                      value={`space:${space.id}`}
                      onSelect={() => openSpace(space)}
                    >
                      <Users className="text-muted-foreground size-3.5 shrink-0" />
                      <span className="truncate">{spaceLabel(space)}</span>
                      <CommandShortcut className="flex items-center gap-1.5 text-[10px] font-normal tracking-normal text-muted-foreground">
                        <span className="truncate">{space.is_admin ? t`Admin` : t`Member`}</span>
                        <CornerDownLeft className="text-muted-foreground hidden size-3 shrink-0 group-data-selected/command-item:inline-block" />
                      </CommandShortcut>
                    </CommandItem>
                  ))}
                </CommandGroup>
              )}

              {matchedSpaces.length > 0 && matchedNodes.length > 0 && <CommandSeparator />}

              {matchedNodes.length > 0 && (
                <CommandGroup heading={t`Files and folders`}>
                  {matchedNodes.map((node) => {
                    const hint = [node.space_name, node.parent_name].filter(Boolean).join(" / ");
                    return (
                      <CommandItem
                        key={`node:${node.id}`}
                        value={`node:${node.id}`}
                        onSelect={() => openNode(node)}
                      >
                        <NodeIcon node={node} className="size-3.5 shrink-0 self-start mt-0.5" />
                        <span className="flex min-w-0 flex-col gap-0.5">
                          <span className="truncate">{node.name}</span>
                          {node.match === "content" && node.snippet ? (
                            <SearchSnippet
                              text={node.snippet}
                              highlights={node.snippet_highlights}
                            />
                          ) : null}
                        </span>
                        <CommandShortcut className="flex items-center gap-1.5 text-[10px] font-normal tracking-normal text-muted-foreground">
                          {hint && <span className="max-w-[200px] truncate">{hint}</span>}
                          <CornerDownLeft className="text-muted-foreground hidden size-3 shrink-0 group-data-selected/command-item:inline-block" />
                        </CommandShortcut>
                      </CommandItem>
                    );
                  })}
                </CommandGroup>
              )}
            </>
          )}
        </CommandList>

        <div className="border-border text-muted-foreground flex h-8 items-center gap-3 border-t px-3 text-[10px]">
          <span className="flex items-center gap-1">
            <ArrowRight className="size-3 rotate-90" /> <Trans>navigate</Trans>
          </span>
          <span className="flex items-center gap-1">
            <CornerDownLeft className="size-3" /> <Trans>open</Trans>
          </span>
          <span className="ml-auto">
            <Trans>esc to close</Trans>
          </span>
        </div>
      </Command>
    </CommandDialog>
  );
}

/**
 * The text around a content match, the matched runs marked. The server
 * already trimmed it and added the ellipses; offsets are in code points.
 */
function SearchSnippet({
  text,
  highlights,
}: {
  text: string;
  highlights: [number, number][] | undefined;
}) {
  return (
    <span className="text-muted-foreground line-clamp-2 text-[11px] leading-snug">
      {highlightSegments(text, highlights).map((segment, i) =>
        segment.hit ? (
          <mark
            key={i}
            className="text-foreground rounded-[2px] bg-amber-200/70 dark:bg-amber-400/30"
          >
            {segment.text}
          </mark>
        ) : (
          <Fragment key={i}>{segment.text}</Fragment>
        ),
      )}
    </span>
  );
}
