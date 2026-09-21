import type { Node } from "@/lib/api";

/**
 * Nodes cut or copied, waiting for a Paste.
 *
 * Space-scoped because both mutations behind a paste are: `copy` and `move`
 * relocate within one space, so a clipboard can never cross one.
 */
export interface ClipboardState {
  mode: "cut" | "copy";
  spaceId: string;
  nodes: Node[];
}

/** Where a create, an upload or a paste lands. */
export interface WriteTarget {
  spaceId?: string;
  /** Folder id; undefined = the space root. */
  dir?: string;
  /** "Acme / Reports" — named in toasts and the drop overlay. */
  label: string;
  canWrite: boolean;
}

/** dir id -> its parent dir id, as recorded in `ui-store.dirs`. */
export type DirParents = Record<string, { parent?: string }>;

/**
 * Resolves the write destination from what the URL says, rather than from
 * whichever component happens to be asking.
 *
 * The rail's New menu and the listing's background menu are mounted in
 * different scopes — the rail is a sibling of the inset, the listing is inside
 * it — so a destination each derives for itself is a destination that can
 * disagree. It did: the rail uploaded to the space root from inside a folder.
 * `?dir=` is the only shared truth, and a surface that has none (Home, Recent,
 * Starred, Shared, Trash, Account) writes to the space root by definition.
 */
export function writeTarget(input: {
  spaceId?: string;
  spaceName?: string;
  /** `?dir=` of the folder listing; absent on every other drive surface. */
  dir?: string;
  dirName?: string;
  /** Edit capability on `dir`, once resolved. */
  dirCanEdit?: boolean;
  /**
   * What to call a destination whose real name is not known yet — the space
   * or folder row has not loaded, or a deep link named an id nothing has
   * resolved. The caller supplies them because this label is interpolated
   * into user-facing toasts, and this module is a pure resolver that must not
   * reach for the i18n runtime. The English defaults keep every existing
   * caller and the unit tests unchanged.
   */
  fallbacks?: { space: string; dir: string };
}): WriteTarget {
  const { spaceId, dir } = input;
  // eslint-disable-next-line lingui/no-unlocalized-strings -- English defaults; DriveWriteProvider passes translated ones
  const fallback = input.fallbacks ?? { space: "this space", dir: "this folder" };
  return {
    spaceId,
    dir,
    label: [input.spaceName ?? fallback.space, dir ? (input.dirName ?? fallback.dir) : undefined]
      .filter(Boolean)
      .join(" / "),
    // Any member may create at the space root and owns what they create there;
    // inside a folder it takes edit capability on that folder.
    canWrite: Boolean(spaceId) && (dir === undefined || input.dirCanEdit === true),
  };
}

/**
 * The nodes a Paste into `target` would actually land — empty when the item
 * has to be disabled, so the guard and the executor cannot disagree about
 * which nodes move.
 */
export function pasteTargets(
  clipboard: ClipboardState | null,
  target: WriteTarget,
  dirs: DirParents,
): Node[] {
  if (!clipboard || clipboard.nodes.length === 0) return [];
  if (!target.spaceId || !target.canWrite) return [];
  if (clipboard.spaceId !== target.spaceId) return [];
  // A copy into the folder the original sits in is the ordinary "make a copy",
  // so it stays offered. A cut is a move, and two moves are not moves at all:
  // into the folder the node already sits in, and into the node's own subtree
  // — which would detach that subtree from the root.
  if (clipboard.mode === "copy") return clipboard.nodes;
  const ancestry = ancestors(target.dir, dirs);
  return clipboard.nodes.filter((n) => n.parent_id !== target.dir && !ancestry.has(n.id));
}

/** `dir` and every folder above it, up to the space root. */
function ancestors(dir: string | undefined, dirs: DirParents): Set<string> {
  const seen = new Set<string>();
  let cur = dir;
  // `dirs` is a client-side trail, so a cycle in it is not impossible; the
  // seen set makes the walk terminate either way.
  while (cur && !seen.has(cur)) {
    seen.add(cur);
    cur = dirs[cur]?.parent;
  }
  return seen;
}
