import {
  Download,
  ExternalLink,
  Eye,
  History,
  Link2,
  Pencil,
  SquarePen,
  Trash2,
} from "lucide-react";
import type { LucideIcon } from "lucide-react";
import { canOpenInTab, isOfficeFile, openEditor, openInTab, type Node } from "@/lib/api";
import type { OpenPanel } from "@/lib/ui-store";

export interface NodeAction {
  key: string;
  label: string;
  icon: LucideIcon;
  destructive?: boolean;
  /** Separator is drawn before this item when rendering a menu. */
  startsGroup?: boolean;
  run: () => void;
}

export interface NodeActionHandlers {
  onPreview: (node: Node) => void;
  onRename: (node: Node) => void;
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
): NodeAction[] {
  const actions: NodeAction[] = [];

  if (node.kind === "file") {
    actions.push({
      key: "preview",
      label: "Preview",
      icon: Eye,
      run: () => handlers.onPreview(node),
    });
    if (canOpenInTab(node)) {
      actions.push({
        key: "open-tab",
        label: "Open in new tab",
        icon: ExternalLink,
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
    key: "share",
    label: "Share",
    icon: Link2,
    startsGroup: node.kind === "file",
    run: () => openPanel("share", node),
  });
  actions.push({
    key: "rename",
    label: "Rename",
    icon: Pencil,
    run: () => handlers.onRename(node),
  });
  actions.push({
    key: "trash",
    label: "Move to trash",
    icon: Trash2,
    destructive: true,
    startsGroup: true,
    run: () => handlers.onDeleteRequest(node),
  });

  return actions;
}
