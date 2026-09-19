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
import type { NodeView, SortField, SortOrder } from "@/lib/api";
import type { DriveWrite } from "@/components/drive-write";

export type FolderActionGroup = "create" | "clipboard" | "selection" | "display" | "info";

/** One choice in a radio submenu — sort field, sort order or view. */
export interface FolderActionChoice {
  value: string;
  label: string;
}

export interface FolderAction {
  key: string;
  label: string;
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
      label: "New folder",
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
            label: "New team folder",
            icon: Users,
            group: "create",
            run: write.newTeamFolder,
          } satisfies FolderAction,
        ]
      : []),
    {
      key: "upload-files",
      label: "Upload files",
      icon: CloudUpload,
      group: "create",
      disabled: !write.canWrite,
      run: write.uploadFiles,
    },
    {
      key: "upload-folder",
      label: "Upload folder",
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
      label: "Select all",
      icon: SquareDashedMousePointer,
      group: "selection",
      disabled: listing.count === 0,
      run: listing.onSelectAll,
    },
    {
      key: "sort",
      label: "Sort by",
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
      label: "View",
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
      label: "Refresh",
      icon: RefreshCw,
      group: "info",
      run: listing.onRefresh,
    },
  );

  // At the space root there is no folder to describe; the space's own
  // information lives in its settings.
  if (write.dir !== undefined) {
    actions.push({
      key: "folder-info",
      label: "Folder information",
      icon: Info,
      group: "info",
      disabled: !listing.canDescribeFolder,
      run: listing.onFolderInfo,
    });
  }

  return actions;
}

/** What the listing contributes: its own view state and the acts on it. */
export interface ListingContext {
  sort: SortField;
  order: SortOrder;
  view: NodeView;
  /** Rows currently listed, so Select all can be disabled when there are none. */
  count: number;
  /** False until the current folder's own row has been fetched. */
  canDescribeFolder: boolean;
  onSort: (sort: SortField, order: SortOrder) => void;
  onView: (view: NodeView) => void;
  onSelectAll: () => void;
  onRefresh: () => void;
  onFolderInfo: () => void;
}

const SORT_CHOICES: FolderActionChoice[] = [
  { value: "name:asc", label: "Name (A–Z)" },
  { value: "name:desc", label: "Name (Z–A)" },
  { value: "modified:desc", label: "Modified (newest)" },
  { value: "modified:asc", label: "Modified (oldest)" },
  { value: "size:desc", label: "Size (largest)" },
  { value: "size:asc", label: "Size (smallest)" },
];

const VIEW_CHOICES: FolderActionChoice[] = [
  { value: "grid", label: "Grid" },
  { value: "list", label: "List" },
];

/** Says what is on the clipboard, because a bare "Paste" does not. */
function pasteLabel(write: DriveWrite): string {
  const n = write.pasteable.length;
  if (n === 0) return "Paste";
  return n === 1 ? "Paste 1 item" : `Paste ${n} items`;
}
