import { useEffect, useMemo, useState } from "react";
import { useNavigate } from "@tanstack/react-router";
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
import { canEdit, openEditor, type SearchKind, type SearchResult, type Space } from "@/lib/api";
import { MIN_QUERY_LENGTH, useNodeSearch, useSpaces } from "@/lib/queries";
import { useUiStore } from "@/lib/ui-store";
import { useDebounced } from "@/lib/use-debounced";
import { PreviewModal } from "@/components/preview-modal";
import { NodeIcon } from "@/components/node-icon";

const KIND_FILTERS: { value: SearchKind | "all"; label: string }[] = [
  { value: "all", label: "All" },
  { value: "file", label: "Files" },
  { value: "dir", label: "Folders" },
];

/**
 * ⌘K search across every space the user belongs to.
 *
 * Mounted by the drive shell so the shortcut works from any drive surface.
 * Results come from the server; spaces are matched locally because the full
 * list is already in the cache and a round trip for three rows is waste.
 */
export function CommandPalette() {
  const open = useUiStore((s) => s.paletteOpen);
  const setOpen = useUiStore((s) => s.setPaletteOpen);
  const navigate = useNavigate();
  const spaces = useSpaces();

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
    } else if (canEdit(node)) {
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

  const tooShort = settled.trim().length < MIN_QUERY_LENGTH;

  const matchedSpaces = useMemo(() => {
    if (tooShort || kind === "file") return [];
    const q = settled.trim().toLowerCase();
    return (spaces.data ?? []).filter((s) => s.name.toLowerCase().includes(q));
  }, [spaces.data, settled, kind, tooShort]);

  const matchedNodes = useMemo(() => {
    if (tooShort) return [];
    return search.data ?? [];
  }, [search.data, tooShort]);

  return (
    <CommandDialog
      open={open}
      onOpenChange={setOpen}
      title="Search"
      description="Search files, folders and spaces"
      className="top-[20%] gap-0 sm:max-w-xl"
    >
      <Command shouldFilter={false} loop className="rounded-none bg-transparent">
        <CommandInput
          autoFocus
          value={query}
          onValueChange={setQuery}
          placeholder="Search files, folders and spaces"
          aria-label="Search files, folders and spaces"
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
                {filter.label}
              </ToggleGroupItem>
            ))}
          </ToggleGroup>
        </div>

        <CommandList className="max-h-80">
          <CommandEmpty className={cn(search.isError && "text-destructive")}>
            {tooShort
              ? `Type at least ${MIN_QUERY_LENGTH} characters to search.`
              : search.isError
                ? (search.error as Error).message
                : search.isFetching && matchedSpaces.length === 0 && matchedNodes.length === 0
                  ? "Searching…"
                  : `No matches for “${settled.trim()}”`}
          </CommandEmpty>

          {!tooShort && !search.isError && (
            <>
              {matchedSpaces.length > 0 && (
                <CommandGroup heading="Spaces">
                  {matchedSpaces.map((space) => (
                    <CommandItem
                      key={`space:${space.id}`}
                      value={`space:${space.id}`}
                      onSelect={() => openSpace(space)}
                    >
                      <Users className="text-muted-foreground size-3.5 shrink-0" />
                      <span className="truncate">{space.name}</span>
                      <CommandShortcut className="flex items-center gap-1.5 text-[10px] font-normal tracking-normal text-muted-foreground">
                        <span className="truncate">{space.is_admin ? "Admin" : "Member"}</span>
                        <CornerDownLeft className="text-muted-foreground hidden size-3 shrink-0 group-data-selected/command-item:inline-block" />
                      </CommandShortcut>
                    </CommandItem>
                  ))}
                </CommandGroup>
              )}

              {matchedSpaces.length > 0 && matchedNodes.length > 0 && <CommandSeparator />}

              {matchedNodes.length > 0 && (
                <CommandGroup heading="Files and folders">
                  {matchedNodes.map((node) => {
                    const hint = [node.space_name, node.parent_name].filter(Boolean).join(" / ");
                    return (
                      <CommandItem
                        key={`node:${node.id}`}
                        value={`node:${node.id}`}
                        onSelect={() => openNode(node)}
                      >
                        <NodeIcon node={node} className="size-3.5 shrink-0" />
                        <span className="truncate">{node.name}</span>
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
            <ArrowRight className="size-3 rotate-90" /> navigate
          </span>
          <span className="flex items-center gap-1">
            <CornerDownLeft className="size-3" /> open
          </span>
          <span className="ml-auto">esc to close</span>
        </div>
      </Command>
    </CommandDialog>
  );
}
