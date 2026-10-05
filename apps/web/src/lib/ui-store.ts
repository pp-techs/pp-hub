import { create } from "zustand";
import { persist } from "zustand/middleware";
import type { Node, NodeView, SortField, SortOrder } from "@/lib/api";
import type { ClipboardState } from "@/lib/write-target";
import type { DraggingNodes } from "@/lib/drag-drop";

export type PanelKind = "versions";

export type OpenPanel = (kind: PanelKind, node: Node) => void;

interface DirEntry {
  name: string;
  /** parent dir id; undefined = space root */
  parent?: string;
}

export interface DocPanels {
  /** The page tree. */
  pages: boolean;
  outline: boolean;
}

interface UiState {
  /** Which dialog panel is open and on which node. */
  panel: { kind: PanelKind; node: Node } | null;
  /**
   * dir id -> {name, parent}, recorded as the user navigates. The API has
   * no single-node GET, so a cold deep-link can't resolve ancestors;
   * persisted so reloads keep the trail.
   */
  dirs: Record<string, DirEntry>;
  /** Last opened space, used to auto-select when landing on /drive. */
  lastSpaceId: string | null;
  /**
   * Name filter for the current listing. It lives here rather than in the
   * space view because the search field sits in the left rail, which is
   * mounted by the drive layout above the route that consumes it.
   */
  filterQuery: string;
  setFilterQuery: (v: string) => void;
  /**
   * How a folder listing is drawn when the URL says nothing. The search params
   * still win, so a shared link keeps the view it was copied with; these are
   * only the fallback, edited from Account → Preferences.
   */
  listingPrefs: { view: NodeView; sort: SortField; order: SortOrder };
  setListingPrefs: (next: Partial<UiState["listingPrefs"]>) => void;
  openPanel: OpenPanel;
  closePanel: () => void;
  /** Record dir id + its parent (from the listing it appeared in). */
  rememberDir: (node: Pick<Node, "id" | "name" | "parent_id">) => void;
  setLastSpaceId: (id: string) => void;
  clearLastSpaceId: (id: string) => void;
  newSpaceOpen: boolean;
  setNewSpaceOpen: (v: boolean) => void;
  /**
   * Cut/copied nodes awaiting a Paste. Ephemeral by nature and deliberately
   * outside `partialize`: a clipboard that survives a reload offers to move
   * nodes the user has long forgotten selecting.
   */
  clipboard: ClipboardState | null;
  setClipboard: (c: ClipboardState) => void;
  clearClipboard: () => void;
  /**
   * ⌘K palette. Lives here because the shortcut is bound by the drive shell
   * but the palette is opened from the rail search field too.
   */
  paletteOpen: boolean;
  setPaletteOpen: (v: boolean) => void;
  /** Right-hand details panel on the folder listing. */
  detailsOpen: boolean;
  setDetailsOpen: (v: boolean) => void;
  toggleDetails: () => void;
  /** Active drag operation within drive. Ephemeral and not persisted. */
  draggingNodes: DraggingNodes | null;
  setDraggingNodes: (val: DraggingNodes | null) => void;
  /**
   * Which of the block editor's side panels are open on a wide screen. Kept
   * here, and persisted, so moving from page to page (each page remounts its
   * editor) or reloading keeps the layout the reader chose. The page tree
   * starts open; the outline starts closed.
   */
  docPanels: DocPanels;
  setDocPanel: (panel: keyof DocPanels, open: boolean) => void;
}

export const useUiStore = create<UiState>()(
  persist(
    (set, get) => ({
      panel: null,
      dirs: {},
      lastSpaceId: null,
      filterQuery: "",
      setFilterQuery: (v) => set({ filterQuery: v }),
      // eslint-disable-next-line lingui/no-unlocalized-strings -- NodeView/SortField/SortOrder ids
      listingPrefs: { view: "grid", sort: "name", order: "asc" },
      setListingPrefs: (next) => set((s) => ({ listingPrefs: { ...s.listingPrefs, ...next } })),
      openPanel: (kind, node) => set({ panel: { kind, node } }),
      closePanel: () => set({ panel: null }),
      rememberDir: (node) => {
        const cur = get().dirs[node.id];
        if (cur && cur.name === node.name && cur.parent === node.parent_id) return;
        set((s) => ({
          dirs: {
            ...s.dirs,
            [node.id]: { name: node.name, parent: node.parent_id },
          },
        }));
      },
      setLastSpaceId: (id) => set({ lastSpaceId: id }),
      clearLastSpaceId: (id) => set((s) => (s.lastSpaceId === id ? { lastSpaceId: null } : s)),
      newSpaceOpen: false,
      setNewSpaceOpen: (v) => set({ newSpaceOpen: v }),
      clipboard: null,
      setClipboard: (c) => set({ clipboard: c }),
      clearClipboard: () => set({ clipboard: null }),
      paletteOpen: false,
      setPaletteOpen: (v: boolean) => set({ paletteOpen: v }),
      detailsOpen: false,
      setDetailsOpen: (v: boolean) => set({ detailsOpen: v }),
      toggleDetails: () => set((s) => ({ detailsOpen: !s.detailsOpen })),
      draggingNodes: null,
      setDraggingNodes: (val) => set({ draggingNodes: val }),
      docPanels: { pages: true, outline: false },
      setDocPanel: (panel, open) => set((s) => ({ docPanels: { ...s.docPanels, [panel]: open } })),
    }),
    {
      name: "pp-hub-ui",
      partialize: (s) => ({
        dirs: s.dirs,
        lastSpaceId: s.lastSpaceId,
        listingPrefs: s.listingPrefs,
        docPanels: s.docPanels,
      }),
    },
  ),
);
