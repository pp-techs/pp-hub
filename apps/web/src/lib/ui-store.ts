import { create } from "zustand";
import { persist } from "zustand/middleware";
import type { Node } from "@/lib/api";

export type PanelKind = "share" | "versions" | "preview";

export type OpenPanel = (kind: PanelKind, node: Node) => void;

interface DirEntry {
  name: string;
  /** parent dir id; undefined = space root */
  parent?: string;
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
  openPanel: OpenPanel;
  closePanel: () => void;
  /** Record dir id + its parent (from the listing it appeared in). */
  rememberDir: (node: Node) => void;
  setLastSpaceId: (id: string) => void;
  newSpaceOpen: boolean;
  setNewSpaceOpen: (v: boolean) => void;
}

export const useUiStore = create<UiState>()(
  persist(
    (set, get) => ({
      panel: null,
      dirs: {},
      lastSpaceId: null,
      filterQuery: "",
      setFilterQuery: (v) => set({ filterQuery: v }),
      openPanel: (kind, node) => set({ panel: { kind, node } }),
      closePanel: () => set({ panel: null }),
      rememberDir: (node) => {
        const cur = get().dirs[node.id];
        if (cur && cur.name === node.name && cur.parent === node.parent_id) return;
        set((s) => ({
          dirs: { ...s.dirs, [node.id]: { name: node.name, parent: node.parent_id } },
        }));
      },
      setLastSpaceId: (id) => set({ lastSpaceId: id }),
      newSpaceOpen: false,
      setNewSpaceOpen: (v) => set({ newSpaceOpen: v }),
    }),
    {
      name: "pp-hub-ui",
      partialize: (s) => ({ dirs: s.dirs, lastSpaceId: s.lastSpaceId }),
    },
  ),
);
