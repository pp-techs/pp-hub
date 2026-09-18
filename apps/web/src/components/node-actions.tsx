import { useMemo } from "react";
import {
  ClipboardCopy,
  Copy,
  Download,
  ExternalLink,
  Eye,
  FolderInput,
  History,
  Info,
  Link2,
  Pencil,
  Scissors,
  SquarePen,
  Star,
  StarOff,
  Trash2,
} from "lucide-react";
import type { LucideIcon } from "lucide-react";
import { canOpenInTab, isOfficeFile, openEditor, openInTab, type Node } from "@/lib/api";
import type { OpenPanel } from "@/lib/ui-store";
import { ShareDialog } from "@/components/share-dialog";

export interface NodeAction {
  key: string;
  label: string;
  icon: LucideIcon;
  /** Rendered right-aligned in a menu. Display only — the shortcuts are bound
   *  by the listing, which is what actually has keyboard focus. */
  shortcut?: string;
  destructive?: boolean;
  /** Separator is drawn before this item when rendering a menu. */
  startsGroup?: boolean;
  run: () => void;
}

export interface NodeActionHandlers {
  onPreview: (node: Node) => void;
  /** Selects this node alone and opens the details panel on it. */
  onShowDetails: (node: Node) => void;
  onToggleStar: (node: Node, starred: boolean) => void;
  onRename: (node: Node) => void;
  onMove: (node: Node) => void;
  onCopy: (node: Node) => void;
  /** Puts the node on the clipboard for a Paste in some other folder. */
  onClipboard: (node: Node, mode: "cut" | "copy") => void;
  onDeleteRequest: (node: Node) => void;
}

export function downloadUrl(node: Node) {
  return `/api/v1/nodes/${node.id}/download`;
}

/**
 * The single definition of what you can do to a node. Both the row's ⋯ menu
 * and its right-click menu render from this list, so the two can't drift.
 */
export function nodeActions(
  node: Node,
  handlers: NodeActionHandlers,
  openPanel: OpenPanel,
  starred = false,
): NodeAction[] {
  const actions: NodeAction[] = [];

  if (node.kind === "file") {
    actions.push({
      key: "preview",
      label: "Preview",
      icon: Eye,
      shortcut: "Space",
      run: () => handlers.onPreview(node),
    });
    if (canOpenInTab(node)) {
      actions.push({
        key: "open-tab",
        label: "Open in new tab",
        icon: ExternalLink,
        shortcut: "⌘↵",
        run: () => openInTab(node),
      });
    }
    if (isOfficeFile(node.name)) {
      actions.push({
        key: "edit",
        label: "Edit",
        icon: SquarePen,
        run: () => openEditor(node),
      });
    }
    actions.push({
      key: "download",
      label: "Download",
      icon: Download,
      shortcut: "⌘D",
      run: () => window.open(downloadUrl(node), "_blank"),
    });
    actions.push({
      key: "versions",
      label: "Versions",
      icon: History,
      run: () => openPanel("versions", node),
    });
  }

  actions.push({
    key: "details",
    label: "Information",
    icon: Info,
    startsGroup: node.kind === "file",
    run: () => handlers.onShowDetails(node),
  });
  actions.push({
    key: "star",
    label: starred ? "Remove star" : "Add star",
    icon: starred ? StarOff : Star,
    run: () => handlers.onToggleStar(node, !starred),
  });
  // One entry, because there is one answer: groups, people and the public
  // link all end up as rows in `shares` on this node.
  actions.push({
    key: "share",
    label: "Share",
    icon: Link2,
    shortcut: "⌘⇧S",
    run: () => void ShareDialog.show({ node }),
  });
  actions.push({
    key: "rename",
    label: "Rename",
    icon: Pencil,
    shortcut: "F2",
    run: () => handlers.onRename(node),
  });
  actions.push({
    key: "move",
    label: "Move to…",
    icon: FolderInput,
    run: () => handlers.onMove(node),
  });
  // Cut/Copy stage the node for a Paste in another folder; "Make a copy"
  // duplicates it where it already is. Three neighbours, three different acts
  // — so the duplicate no longer claims the ⌘C badge, and none of the three
  // advertises a shortcut, because nothing binds one yet.
  actions.push({
    key: "cut",
    label: "Cut",
    icon: Scissors,
    run: () => handlers.onClipboard(node, "cut"),
  });
  actions.push({
    key: "clipboard-copy",
    label: "Copy",
    icon: ClipboardCopy,
    run: () => handlers.onClipboard(node, "copy"),
  });
  actions.push({
    key: "copy",
    label: "Make a copy",
    icon: Copy,
    run: () => handlers.onCopy(node),
  });
  actions.push({
    key: "trash",
    label: "Move to trash",
    icon: Trash2,
    shortcut: "⌫",
    destructive: true,
    startsGroup: true,
    run: () => handlers.onDeleteRequest(node),
  });
  return actions;
}

/**
 * Memoized `nodeActions` for the listing rows: the action objects (and their
 * closures) are rebuilt only when the row's own node, capabilities or
 * handlers change — not when an unrelated row's selection state flips.
 */
export function useNodeActions(
  node: Node,
  handlers: NodeActionHandlers,
  openPanel: OpenPanel,
  starred = false,
): NodeAction[] {
  const { onPreview, onShowDetails, onToggleStar, onRename, onMove, onCopy, onDeleteRequest } =
    handlers;
  return useMemo(
    () => nodeActions(node, handlers, openPanel, starred),
    // The handlers bag is reassembled per render; the functions in it are the
    // real inputs.
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [
      node,
      onPreview,
      onShowDetails,
      onToggleStar,
      onRename,
      onMove,
      onCopy,
      onDeleteRequest,
      openPanel,
      starred,
    ],
  );
}
