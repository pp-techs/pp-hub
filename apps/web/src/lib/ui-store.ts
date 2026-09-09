import { create } from "zustand";
import { persist } from "zustand/middleware";
import type { Node } from "@/lib/api";

export type PanelKind = "share" | "versions" | "preview" | "edit";

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
  openPanel: OpenPanel;
  closePanel: () => void;
  /** Record dir id + its parent (from the listing it appeared in). */
  rememberDir: (node: Node) => void;
  setLastSpaceId: (id: string) => void;
}

export const useUiStore = create<UiState>()(
  persist(
    (set, get) => ({
      panel: null,
      dirs: {},
      lastSpaceId: null,
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
    }),
    {
      name: "pp-hub-ui",
      partialize: (s) => ({ dirs: s.dirs, lastSpaceId: s.lastSpaceId }),
    },
  ),
);
