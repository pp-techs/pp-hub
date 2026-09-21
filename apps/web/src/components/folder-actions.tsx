import {
  ArrowDownAZ,
  ClipboardPaste,
  CloudUpload,
  FolderPlus,
  FolderUp,
  Info,
  LayoutGrid,
  RefreshCw,
  SquareDashedMousePointer,
  Users,
} from "lucide-react";
import type { LucideIcon } from "lucide-react";
import { msg, plural } from "@lingui/core/macro";
import type { MessageDescriptor } from "@lingui/core";
import type { NodeView, SortField, SortOrder } from "@/lib/api";
import type { DriveWrite } from "@/components/drive-write";

export type FolderActionGroup = "create" | "clipboard" | "selection" | "display" | "info";

/** One choice in a radio submenu — sort field, sort order or view. */
export interface FolderActionChoice {
  value: string;
  label: MessageDescriptor;
}

export interface FolderAction {
  key: string;
  /**
   * A message descriptor, not a rendered string. `folderActions` is a plain
   * function and the choice lists below are module scope, so neither may
   * reach a hook or a catalog: rendered text here would freeze whichever
   * language loaded first. The menu that draws the item calls
   * `i18n._(action.label)`, which is what makes it follow a language switch.
   */
  label: MessageDescriptor;
  icon: LucideIcon;
  group: FolderActionGroup;
  disabled?: boolean;
  /** A radio submenu. Mutually exclusive with `run`. */
  choices?: {
    value: string;
    items: FolderActionChoice[];
    onSelect: (value: string) => void;
  };
  run?: () => void;
}

/**
 * Everything a folder — as opposed to a node in it — can be asked to do.
 *
 * The counterpart of `nodeActions`, and for the same reason: the rail's New
 * menu and the listing's background menu both render from this list, so the
 * two cannot drift. They already had, which is how the rail's upload came to
 * ignore the folder the user was looking at.
 *
 * `listing` is absent for callers that are not a folder listing (the rail),
 * and its groups are simply not produced — there is no selection to select
 * and no sort order to set.
 */
export function folderActions(write: DriveWrite, listing?: ListingContext): FolderAction[] {
  const actions: FolderAction[] = [
    {
      key: "new-folder",
      label: msg`New folder`,
      icon: FolderPlus,
      group: "create",
      disabled: !write.canWrite,
      run: write.newFolder,
    },
    // Admins only, and never disabled by the folder being browsed: a team
    // folder is always created at the space's top level, so the edit rights
    // that gate "New folder" here do not apply to it.
    ...(write.canCreateTeamFolder
      ? [
          {
            key: "new-team-folder",
            label: msg`New team folder`,
            icon: Users,
            group: "create",
            run: write.newTeamFolder,
          } satisfies FolderAction,
        ]
      : []),
    {
      key: "upload-files",
      label: msg`Upload files`,
      icon: CloudUpload,
      group: "create",
      disabled: !write.canWrite,
      run: write.uploadFiles,
    },
    {
      key: "upload-folder",
      label: msg`Upload folder`,
      icon: FolderUp,
      group: "create",
      disabled: !write.canWrite,
      run: write.uploadFolder,
    },
    {
      key: "paste",
      label: pasteLabel(write),
      icon: ClipboardPaste,
      group: "clipboard",
      disabled: write.pasteable.length === 0,
      run: write.paste,
    },
  ];

  if (!listing) return actions;

  actions.push(
    {
      key: "select-all",
      label: msg`Select all`,
      icon: SquareDashedMousePointer,
      group: "selection",
      disabled: listing.count === 0,
      run: listing.onSelectAll,
    },
    {
      key: "sort",
      label: msg`Sort by`,
      icon: ArrowDownAZ,
      group: "display",
      choices: {
        value: `${listing.sort}:${listing.order}`,
        items: SORT_CHOICES,
        onSelect: (v) => {
          const [sort, order] = v.split(":");
          listing.onSort(sort as SortField, order as SortOrder);
        },
      },
    },
    {
      key: "view",
      label: msg`View`,
      icon: LayoutGrid,
      group: "display",
      choices: {
        value: listing.view,
        items: VIEW_CHOICES,
        onSelect: (v) => listing.onView(v as NodeView),
      },
    },
    {
      key: "refresh",
      label: msg`Refresh`,
      icon: RefreshCw,
      group: "info",
      run: listing.onRefresh,
    },
  );

  // At the space root the subject is the space itself, which the details
  // panel describes; inside a folder it is that folder, once its own row has
  // arrived.
  actions.push({
    key: "folder-info",
    label: listing.infoSubject === "space" ? msg`Space information` : msg`Folder information`,
    icon: Info,
    group: "info",
    disabled: listing.infoSubject === null,
    run: listing.onFolderInfo,
  });

  return actions;
}

/** What the listing contributes: its own view state and the acts on it. */
export interface ListingContext {
  sort: SortField;
  order: SortOrder;
  view: NodeView;
  /** Rows currently listed, so Select all can be disabled when there are none. */
  count: number;
  /**
   * What the (i) panel would describe here: the space at a root listing, the
   * current folder once its own row has been fetched, nothing until then.
   */
  infoSubject: "space" | "folder" | null;
  onSort: (sort: SortField, order: SortOrder) => void;
  onView: (view: NodeView) => void;
  onSelectAll: () => void;
  onRefresh: () => void;
  onFolderInfo: () => void;
}

const SORT_CHOICES: FolderActionChoice[] = [
  { value: "name:asc", label: msg`Name (A–Z)` },
  { value: "name:desc", label: msg`Name (Z–A)` },
  { value: "modified:desc", label: msg`Modified (newest)` },
  { value: "modified:asc", label: msg`Modified (oldest)` },
  { value: "size:desc", label: msg`Size (largest)` },
  { value: "size:asc", label: msg`Size (smallest)` },
];

const VIEW_CHOICES: FolderActionChoice[] = [
  { value: "grid", label: msg`Grid` },
  { value: "list", label: msg`List` },
];

/** Says what is on the clipboard, because a bare "Paste" does not. */
function pasteLabel(write: DriveWrite): MessageDescriptor {
  const n = write.pasteable.length;
  if (n === 0) return msg`Paste`;
  // One ICU message rather than a singular/plural pair picked here: the count
  // categories a language needs are not the two English has.
  return msg({ message: plural(n, { one: "Paste # item", other: "Paste # items" }) });
}
