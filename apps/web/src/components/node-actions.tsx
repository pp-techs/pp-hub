import { useMemo } from "react";
import {
  ClipboardCopy,
  Copy,
  Download,
  ExternalLink,
  Eye,
  FileArchive,
  FolderDown,
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
import { msg } from "@lingui/core/macro";
import type { MessageDescriptor } from "@lingui/core";
import {
  canOpenInTab,
  canRestructure,
  capsOf,
  exportDocumentUrl,
  isMarkdownFile,
  openEditor,
  openInTab,
  opensInEditor,
  type Node,
} from "@/lib/api";
import type { OpenPanel } from "@/lib/ui-store";
import { ShareDialog } from "@/components/share-dialog";

/**
 * Menu blocks, in the order they appear: opening the item, facts about it and
 * who reaches it, reshaping it, the clipboard, and throwing it away.
 */
export type NodeActionGroup = "open" | "about" | "organize" | "clipboard" | "trash";

export interface NodeAction {
  key: string;
  /**
   * A message descriptor, not a rendered string. `nodeActions` is a plain
   * function — it is called from `useMemo` and from menus that are not
   * components — so it must not reach a hook or a catalog: rendered text
   * here would freeze whichever language loaded first. The menu that draws
   * the item calls `i18n._(action.label)`, which is what makes it follow a
   * language switch.
   */
  label: MessageDescriptor;
  icon: LucideIcon;
  /**
   * Rendered right-aligned in a menu. Key names, so not translated. Each badge
   * names a key the listing binds to this same action (see the folder route's
   * key handler) — a badge with no binding is a lie the menu repeats.
   */
  shortcut?: string;
  destructive?: boolean;
  /** Menus draw one group per run of equal values, separated from the next. */
  group: NodeActionGroup;
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
  // eslint-disable-next-line lingui/no-unlocalized-strings -- API route path
  return `/api/v1/nodes/${node.id}/download`;
}

/**
 * The single definition of what you can do to a node. Both the row's ⋯ menu
 * and its right-click menu render from this list, so the two can't drift.
 *
 * Every entry is gated on the capabilities the server resolved for this node,
 * because in this model two rows of the same folder can differ: a folder you
 * were granted view on holds files you uploaded and own. An action the caller
 * does not hold is left out rather than disabled — a greyed "Share" invites a
 * click and then explains nothing, while its absence matches what the API
 * would answer.
 */
export function nodeActions(
  node: Node,
  handlers: NodeActionHandlers,
  openPanel: OpenPanel,
  starred = false,
  /**
   * Whether the caller may write into the folder being listed. "Make a copy"
   * lands the duplicate here, so it needs both: the right to read the source
   * and the right to add to this folder.
   */
  canWriteHere = false,
): NodeAction[] {
  const caps = capsOf(node);
  const restructure = canRestructure(node);
  const actions: NodeAction[] = [];

  // An extension node has no bytes, so nothing below that reads them applies:
  // it opens in its extension's editor and has no preview, download or versions.
  if (node.kind === "ext") {
    actions.push({
      key: "edit",
      label: msg`Open`,
      icon: SquarePen,
      group: "open",
      run: () => openEditor(node),
    });
  }

  if (node.kind === "file") {
    actions.push({
      key: "preview",
      label: msg`Preview`,
      icon: Eye,
      group: "open",
      // eslint-disable-next-line lingui/no-unlocalized-strings -- keyboard key name
      shortcut: "Space",
      run: () => handlers.onPreview(node),
    });
    // Opening the bytes in a tab is reading them: `/content` takes view.
    if (canOpenInTab(node) && caps.view) {
      actions.push({
        key: "open-tab",
        label: msg`Open in new tab`,
        icon: ExternalLink,
        group: "open",
        shortcut: "⌘↵",
        run: () => openInTab(node),
      });
    }
    if (opensInEditor(node)) {
      // The editor opens read-only for a reader — OnlyOffice is the only
      // thing here that can render a .docx at all, and a markdown document
      // opens in the block editor either way — so the label has to say
      // which of the two they are getting.
      actions.push({
        key: "edit",
        label: caps.edit ? msg`Edit` : msg`Open`,
        icon: SquarePen,
        group: "open",
        run: () => openEditor(node),
      });
    }
    if (caps.download) {
      actions.push({
        key: "download",
        label: msg`Download`,
        icon: Download,
        group: "open",
        // No ⌘D badge: that key is the browser's own (bookmark), and the
        // listing refuses to take it away. Download stays a menu act.
        // eslint-disable-next-line lingui/no-unlocalized-strings -- window.open target
        run: () => window.open(downloadUrl(node), "_blank"),
      });
    }
    // The `.md` alone keeps `attachment:` references that only resolve in
    // pp-hub; the export carries the images and files beside it.
    if (caps.download && isMarkdownFile(node)) {
      actions.push({
        key: "export",
        label: msg`Export with images (.zip)`,
        icon: FileArchive,
        group: "open",
        // eslint-disable-next-line lingui/no-unlocalized-strings -- window.open target
        run: () => window.open(exportDocumentUrl(node.id), "_blank"),
      });
    }
    actions.push({
      key: "versions",
      label: msg`Versions`,
      icon: History,
      group: "open",
      // eslint-disable-next-line lingui/no-unlocalized-strings -- panel id
      run: () => openPanel("versions", node),
    });
  }

  // A folder downloads as a zip of everything under it; the server streams it
  // (`/download` on a dir) and audits it as a folder download.
  if (node.kind === "dir" && caps.download) {
    actions.push({
      key: "download",
      label: msg`Download as .zip`,
      icon: FolderDown,
      group: "open",
      // eslint-disable-next-line lingui/no-unlocalized-strings -- window.open target
      run: () => window.open(downloadUrl(node), "_blank"),
    });
  }

  actions.push({
    key: "details",
    label: msg`Information`,
    icon: Info,
    group: "about",
    run: () => handlers.onShowDetails(node),
  });
  actions.push({
    key: "star",
    label: starred ? msg`Remove star` : msg`Add star`,
    icon: starred ? StarOff : Star,
    group: "about",
    run: () => handlers.onToggleStar(node, !starred),
  });
  // One entry, because there is one answer: groups, people and the public
  // link all end up as rows in `shares` on this node.
  if (caps.share) {
    actions.push({
      key: "share",
      label: msg`Share`,
      icon: Link2,
      group: "about",
      // eslint-disable-next-line lingui/no-unlocalized-strings -- keyboard key name
      shortcut: "⌘⇧S",
      run: () => void ShareDialog.show({ node }),
    });
  }
  if (restructure) {
    actions.push({
      key: "rename",
      label: msg`Rename`,
      icon: Pencil,
      group: "organize",
      // eslint-disable-next-line lingui/no-unlocalized-strings -- keyboard key name
      shortcut: "F2",
      run: () => handlers.onRename(node),
    });
  }
  if (caps.edit) {
    actions.push({
      key: "move",
      label: msg`Move to…`,
      icon: FolderInput,
      group: "organize",
      run: () => handlers.onMove(node),
    });
  }
  // Cut/Copy stage the node for a Paste in another folder; "Make a copy"
  // duplicates it where it already is. Three neighbours, three different acts
  // — so the duplicate no longer claims the ⌘C badge, and none of the three
  // advertises a shortcut: the listing binds no key for them.
  //
  // Cut is a move, so it takes edit. Copy only reads the source — the
  // destination is where the write happens, and the paste is authorised
  // there — so it takes download, the capability that already allows taking
  // the bytes away.
  if (caps.edit) {
    actions.push({
      key: "cut",
      label: msg`Cut`,
      icon: Scissors,
      group: "clipboard",
      // eslint-disable-next-line lingui/no-unlocalized-strings -- clipboard mode
      run: () => handlers.onClipboard(node, "cut"),
    });
  }
  if (caps.download) {
    actions.push({
      key: "clipboard-copy",
      label: msg`Copy`,
      icon: ClipboardCopy,
      group: "clipboard",
      // eslint-disable-next-line lingui/no-unlocalized-strings -- clipboard mode
      run: () => handlers.onClipboard(node, "copy"),
    });
    if (canWriteHere) {
      actions.push({
        key: "copy",
        label: msg`Make a copy`,
        icon: Copy,
        group: "clipboard",
        run: () => handlers.onCopy(node),
      });
    }
  }
  if (restructure) {
    actions.push({
      key: "trash",
      label: msg`Move to trash`,
      icon: Trash2,
      shortcut: "⌫",
      destructive: true,
      group: "trash",
      run: () => handlers.onDeleteRequest(node),
    });
  }
  return actions;
}

/**
 * Memoized `nodeActions` for the listing rows: the action objects (and their
 * closures) are rebuilt only when the row's own node, capabilities or
 * handlers change — not when an unrelated row's selection state flips.
 *
 * The active language is not an input: the labels are descriptors, resolved
 * by the menu at render, so a language switch needs no new action objects.
 */
export function useNodeActions(
  node: Node,
  handlers: NodeActionHandlers,
  openPanel: OpenPanel,
  starred = false,
  canWriteHere = false,
): NodeAction[] {
  const {
    onPreview,
    onShowDetails,
    onToggleStar,
    onRename,
    onMove,
    onCopy,
    onClipboard,
    onDeleteRequest,
  } = handlers;
  return useMemo(
    () => nodeActions(node, handlers, openPanel, starred, canWriteHere),
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
      onClipboard,
      onDeleteRequest,
      openPanel,
      starred,
      canWriteHere,
    ],
  );
}
