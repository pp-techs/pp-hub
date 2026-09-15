import { useEffect, useMemo, useRef, useState } from "react";
import { useNavigate } from "@tanstack/react-router";
import { ArrowRight, CornerDownLeft, Loader2, Search, Users } from "lucide-react";
import { Dialog, DialogContent, DialogTitle } from "@workspace/ui/components/dialog";
import { cn } from "@workspace/ui/lib/utils";
import { canEdit, openEditor, type SearchKind, type SearchResult, type Space } from "@/lib/api";
import { MIN_QUERY_LENGTH, useNodeSearch, useSpaces } from "@/lib/queries";
import { useUiStore } from "@/lib/ui-store";
import { useDebounced } from "@/lib/use-debounced";
import { PreviewModal } from "@/components/preview-modal";
import { NodeIcon } from "@/components/node-icon";

/** One selectable line. Sections are flattened so ↑/↓ crosses their headers. */
interface Row {
  key: string;
  section: string;
  label: string;
  hint?: string;
  icon: React.ReactNode;
  run: () => void;
}

const KIND_FILTERS: { value: SearchKind | undefined; label: string }[] = [
  { value: undefined, label: "All" },
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
  const [active, setActive] = useState(0);
  const listRef = useRef<HTMLDivElement>(null);

  const settled = useDebounced(query, 200);
  const search = useNodeSearch(settled, { kind });

  // Every open starts clean; a stale query from last time is never what you want.
  useEffect(() => {
    if (open) {
      setQuery("");
      setKind(undefined);
      setActive(0);
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

  const rows = useMemo<Row[]>(() => {
    const q = settled.trim().toLowerCase();
    const out: Row[] = [];

    if (kind !== "file") {
      for (const space of (spaces.data ?? []).filter((s) => s.name.toLowerCase().includes(q))) {
        out.push({
          key: `space:${space.id}`,
          section: "Spaces",
          label: space.name,
          hint: space.role,
          icon: <Users className="text-muted-foreground size-3.5 shrink-0" />,
          run: () => openSpace(space),
        });
      }
    }

    for (const node of search.data ?? []) {
      out.push({
        key: `node:${node.id}`,
        section: "Files and folders",
        label: node.name,
        hint: [node.space_name, node.parent_name].filter(Boolean).join(" / "),
        icon: <NodeIcon node={node} className="size-3.5 shrink-0" />,
        run: () => openNode(node),
      });
    }

    return out;
    // openNode and openSpace are intentionally not dependencies. They are
    // rebuilt every render but only ever *called* from a row, and adding them
    // would rebuild rows on every keystroke and reset the active index.
  }, [search.data, spaces.data, settled, kind]);

  // A shrinking result set must never leave the highlight past the end.
  useEffect(() => setActive((i) => Math.min(i, Math.max(0, rows.length - 1))), [rows.length]);

  useEffect(() => {
    listRef.current
      ?.querySelector<HTMLElement>(`[data-index="${active}"]`)
      ?.scrollIntoView({ block: "nearest" });
  }, [active]);

  const tooShort = settled.trim().length < MIN_QUERY_LENGTH;

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogContent
        className="top-[20%] gap-0 overflow-hidden p-0 sm:max-w-xl"
        onKeyDown={(e) => {
          if (e.key === "ArrowDown") {
            e.preventDefault();
            setActive((i) => (rows.length === 0 ? 0 : (i + 1) % rows.length));
          } else if (e.key === "ArrowUp") {
            e.preventDefault();
            setActive((i) => (rows.length === 0 ? 0 : (i - 1 + rows.length) % rows.length));
          } else if (e.key === "Enter") {
            e.preventDefault();
            rows[active]?.run();
          }
        }}
      >
        <DialogTitle className="sr-only">Search</DialogTitle>

        <div className="border-border flex h-11 items-center gap-2 border-b px-3">
          <Search className="text-muted-foreground size-4 shrink-0" />
          <input
            autoFocus
            value={query}
            onChange={(e) => {
              setQuery(e.target.value);
              setActive(0);
            }}
            placeholder="Search files, folders and spaces"
            aria-label="Search files, folders and spaces"
            className="placeholder:text-muted-foreground w-full min-w-0 border-none bg-transparent p-0 text-sm outline-none"
          />
          {search.isFetching && (
            <Loader2 className="text-muted-foreground size-3.5 shrink-0 animate-spin" />
          )}
        </div>

        <div className="border-border flex h-9 items-center gap-1 border-b px-3">
          {KIND_FILTERS.map((filter) => (
            <button
              key={filter.label}
              type="button"
              aria-pressed={kind === filter.value}
              className={cn(
                "h-6 rounded-md px-2 text-[11px] transition-colors",
                kind === filter.value
                  ? "bg-primary text-primary-foreground"
                  : "text-muted-foreground hover:bg-muted",
              )}
              onClick={() => {
                setKind(filter.value);
                setActive(0);
              }}
            >
              {filter.label}
            </button>
          ))}
        </div>

        <div ref={listRef} className="max-h-80 overflow-y-auto p-1">
          {tooShort ? (
            <p className="text-muted-foreground px-2 py-6 text-center text-xs">
              Type at least {MIN_QUERY_LENGTH} characters to search.
            </p>
          ) : search.isError ? (
            <p className="text-destructive px-2 py-6 text-center text-xs">
              {(search.error as Error).message}
            </p>
          ) : rows.length === 0 ? (
            <p className="text-muted-foreground px-2 py-6 text-center text-xs">
              {search.isFetching ? "Searching…" : `No matches for “${settled.trim()}”`}
            </p>
          ) : (
            rows.map((row, index) => (
              <div key={row.key}>
                {row.section !== rows[index - 1]?.section && (
                  <p className="text-muted-foreground px-2 pt-2 pb-1 text-[10px] font-medium tracking-wider uppercase">
                    {row.section}
                  </p>
                )}
                <button
                  type="button"
                  data-index={index}
                  className={cn(
                    "flex h-8 w-full items-center gap-2 rounded-md px-2 text-left text-xs",
                    index === active ? "bg-accent text-foreground" : "text-foreground",
                  )}
                  onMouseMove={() => setActive(index)}
                  onClick={row.run}
                >
                  {row.icon}
                  <span className="truncate">{row.label}</span>
                  {row.hint && (
                    <span className="text-muted-foreground ml-auto truncate pl-2 text-[10px]">
                      {row.hint}
                    </span>
                  )}
                  {index === active && (
                    <CornerDownLeft className="text-muted-foreground size-3 shrink-0" />
                  )}
                </button>
              </div>
            ))
          )}
        </div>

        <div className="border-border text-muted-foreground flex h-8 items-center gap-3 border-t px-3 text-[10px]">
          <span className="flex items-center gap-1">
            <ArrowRight className="size-3 rotate-90" /> navigate
          </span>
          <span className="flex items-center gap-1">
            <CornerDownLeft className="size-3" /> open
          </span>
          <span className="ml-auto">esc to close</span>
        </div>
      </DialogContent>
    </Dialog>
  );
}
