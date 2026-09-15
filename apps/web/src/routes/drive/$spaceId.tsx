import {
  createFileRoute,
  notFound,
  useNavigate,
  useParams,
  useSearch,
} from "@tanstack/react-router";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { FormEvent } from "react";
import { FolderOpen } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@workspace/ui/components/button";
import { Input } from "@workspace/ui/components/input";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@workspace/ui/components/dialog";
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@workspace/ui/components/empty";
import {
  canEdit,
  openEditor,
  sortNodes,
  type DrivePanel,
  type Node,
  type NodeView,
  type SortField,
  type SortOrder,
} from "@/lib/api";
import {
  childrenOptions,
  spacesOptions,
  useCreateFolder,
  useDeleteNode,
  useCopyNode,
  useMoveNode,
  useRenameNode,
  useStarred,
  useToggleStar,
} from "@/lib/queries";
import { useUploadStore } from "@/lib/upload-store";
import { collectInputFiles } from "@/lib/drop-items";
import { useUiStore } from "@/lib/ui-store";
import { SpaceMembersPanel } from "@/components/space-members-panel";
import { SharedPanel } from "@/components/drive-panels";
import { TrashPanel } from "@/components/trash-panel";
import { SpaceSettingsPanel } from "@/components/space-settings-panel";
import { NodePanels } from "@/components/node-panels";
import { PreviewModal } from "@/components/preview-modal";
import { DetailsPanel } from "@/components/preview-panel";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { DriveHeader } from "@/components/drive-header";
import { DriveToolbar } from "@/components/drive-toolbar";
import { NodeGrid } from "@/components/node-grid";
import { NodeList } from "@/components/node-list";
import { SelectionBar } from "@/components/selection-bar";
import { MoveDialog } from "@/components/move-dialog";
import { DropZone } from "@/components/drop-zone";
import { downloadUrl } from "@/components/node-actions";

export const Route = createFileRoute("/drive/$spaceId")({
  head: () => ({ meta: [{ title: "Drive · pp-hub" }] }),
  validateSearch: (
    search: Record<string, unknown>,
  ): {
    dir?: string;
    sort?: SortField;
    order?: SortOrder;
    view?: NodeView;
    panel?: DrivePanel;
  } => ({
    dir: typeof search.dir === "string" ? search.dir : undefined,
    sort: ["name", "modified", "size"].includes(search.sort as string)
      ? (search.sort as SortField)
      : undefined,
    order: ["asc", "desc"].includes(search.order as string)
      ? (search.order as SortOrder)
      : undefined,
    view: ["grid", "list"].includes(search.view as string) ? (search.view as NodeView) : undefined,
    panel: ["members", "trash", "shared", "settings"].includes(search.panel as string)
      ? (search.panel as DrivePanel)
      : undefined,
  }),
  component: FolderView,
  notFoundComponent: SpaceGone,
});

function SpaceGone() {
  const navigate = useNavigate();
  return (
    <div className="flex flex-1 flex-col items-center justify-center gap-3 p-8">
      <h1 className="text-sm font-medium">Space not found</h1>
      <p className="text-muted-foreground text-xs">
        It may have been deleted, or you no longer have access.
      </p>
      <Button variant="outline" onClick={() => void navigate({ to: "/drive" })}>
        Back to Drive
      </Button>
    </div>
  );
}

function FolderView() {
  const { spaceId } = useParams({ from: "/drive/$spaceId" });
  const {
    dir,
    sort = "name",
    order = "asc",
    view = "grid",
    panel,
  } = useSearch({ from: "/drive/$spaceId" });
  // Any panel replaces the file listing, so the browsing controls hide with it.
  const browsing = panel === undefined;
  const navigate = useNavigate();
  const spaces = spacesOptions();
  const rememberDir = useUiStore((s) => s.rememberDir);
  const setLastSpaceId = useUiStore((s) => s.setLastSpaceId);
  const openPanel = useUiStore((s) => s.openPanel);
  const filterQuery = useUiStore((s) => s.filterQuery);
  const dirs = useUiStore((s) => s.dirs);

  /** Rows ticked via checkbox — drives the floating action bar. */
  const [selectedIds, setSelectedIds] = useState<Set<string>>(new Set());
  /** Row last clicked — drives the details panel. */
  const [focusedNode, setFocusedNode] = useState<Node | null>(null);
  const [detailsOpen, setDetailsOpen] = useState(true);

  const [newFolderOpen, setNewFolderOpen] = useState(false);
  const [newFolderName, setNewFolderName] = useState("");
  const [renameNode, setRenameNode] = useState<Node | null>(null);
  const [renameName, setRenameName] = useState("");
  const [confirmNodes, setConfirmNodes] = useState<Node[] | null>(null);

  const fileRef = useRef<HTMLInputElement>(null);
  const folderRef = useRef<HTMLInputElement>(null);
  const enqueueUploads = useUploadStore((s) => s.enqueue);
  const mkdir = useCreateFolder(spaceId, dir);
  const rename = useRenameNode(spaceId);
  const move = useMoveNode(spaceId);
  const copy = useCopyNode(spaceId);
  const toggleStar = useToggleStar();
  // One request for the whole starred set beats one per visible row.
  const starred = useStarred(200);
  const del = useDeleteNode(spaceId);

  const clickTimer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);

  useEffect(() => {
    setLastSpaceId(spaceId);
  }, [spaceId, setLastSpaceId]);

  // Moving to another folder or space invalidates both kinds of selection.
  useEffect(() => {
    setFocusedNode(null);
    setSelectedIds(new Set());
  }, [spaceId, dir]);

  useEffect(() => () => clearTimeout(clickTimer.current), []);

  const children = childrenOptions(spaceId, dir);

  const sorted = useMemo(() => {
    const q = filterQuery.trim().toLowerCase();
    const base = children.data ?? [];
    const filtered = q ? base.filter((n) => n.name.toLowerCase().includes(q)) : base;
    return sortNodes(filtered, sort, order);
  }, [children.data, filterQuery, sort, order]);

  const sortedDirs = useMemo(() => sorted.filter((n) => n.kind === "dir"), [sorted]);
  const sortedFiles = useMemo(() => sorted.filter((n) => n.kind === "file"), [sorted]);

  // Re-read the focused node from the listing so it reflects renames.
  const activeFocused = focusedNode
    ? (children.data?.find((n) => n.id === focusedNode.id) ?? focusedNode)
    : null;

  const selectedNodes = useMemo(
    () => sorted.filter((n) => selectedIds.has(n.id)),
    [sorted, selectedIds],
  );

  const goTo = useCallback(
    (next: Partial<{ dir?: string; sort: SortField; order: SortOrder; view: NodeView }>) =>
      void navigate({
        to: "/drive/$spaceId",
        params: { spaceId },
        search: { dir, sort, order, view, panel, ...next },
      }),
    [navigate, spaceId, dir, sort, order, view, panel],
  );

  // Single click focuses — delayed so a double-click doesn't flash the panel.
  const handleClick = useCallback((node: Node) => {
    clearTimeout(clickTimer.current);
    clickTimer.current = setTimeout(() => setFocusedNode(node), 250);
  }, []);

  const handleDoubleClick = useCallback(
    (node: Node) => {
      clearTimeout(clickTimer.current);
      clickTimer.current = undefined;
      if (node.kind === "dir") {
        rememberDir(node);
        void navigate({
          to: "/drive/$spaceId",
          params: { spaceId },
          search: { dir: node.id, sort, order, view },
        });
      } else if (canEdit(node)) {
        openEditor(node);
      } else {
        void PreviewModal.show({ node, siblings: sortedFiles });
      }
    },
    [navigate, spaceId, rememberDir, sort, order, view, sortedFiles],
  );

  const handleToggle = useCallback((node: Node, checked: boolean) => {
    setSelectedIds((prev) => {
      const next = new Set(prev);
      if (checked) next.add(node.id);
      else next.delete(node.id);
      return next;
    });
  }, []);

  const handleToggleAll = useCallback(
    (checked: boolean) => setSelectedIds(checked ? new Set(sorted.map((n) => n.id)) : new Set()),
    [sorted],
  );

  const handleRename = useCallback((node: Node) => {
    setRenameNode(node);
    setRenameName(node.name);
  }, []);

  // Siblings let the modal page through the folder without closing.
  const handlePreview = useCallback(
    (node: Node) => {
      void PreviewModal.show({ node, siblings: sortedFiles });
    },
    [sortedFiles],
  );

  const handleDeleteRequest = useCallback((node: Node) => setConfirmNodes([node]), []);

  /**
   * Opens the destination picker and moves everything the user picked for.
   * Each node moves independently, so one rejection (a name already taken in
   * the destination) does not hold up the rest.
   */
  const handleMove = useCallback(
    async (nodes: Node[]) => {
      if (nodes.length === 0) return;
      const spaceName = spaces.data?.find((s) => s.id === spaceId)?.name ?? "this space";
      const target = await MoveDialog.show({ spaceId, spaceName, nodes });
      if (target === null) return; // cancelled — undefined means the space root

      let moved = 0;
      await Promise.all(
        nodes.map((node) =>
          move
            .mutateAsync({ nodeId: node.id, parentId: target })
            .then(() => {
              moved += 1;
            })
            .catch((e: Error) =>
              toast.error(`Could not move “${node.name}”`, { description: e.message }),
            ),
        ),
      );
      if (moved > 0) {
        toast.success(moved === 1 ? "Moved 1 item" : `Moved ${moved} items`);
        setSelectedIds(new Set());
      }
    },
    [move, spaceId, spaces.data],
  );

  const handleDeleteConfirmed = useCallback(() => {
    for (const node of confirmNodes ?? []) del.mutate(node.id);
    const removed = new Set((confirmNodes ?? []).map((n) => n.id));
    setSelectedIds((prev) => new Set([...prev].filter((id) => !removed.has(id))));
    if (focusedNode && removed.has(focusedNode.id)) setFocusedNode(null);
  }, [confirmNodes, del, focusedNode]);

  const starredIds = useMemo(() => new Set((starred.data ?? []).map((n) => n.id)), [starred.data]);

  const nodeHandlers = {
    onPreview: handlePreview,
    onCopy: (node: Node) =>
      copy.mutate(
        { nodeId: node.id, parentId: dir },
        {
          onSuccess: (made) => toast.success(`Copied as “${made.name}”`),
          onError: (e) => toast.error(`Could not copy “${node.name}”`, { description: e.message }),
        },
      ),
    onToggleStar: (node: Node, next: boolean) =>
      toggleStar.mutate(
        { nodeId: node.id, starred: next },
        { onError: (e) => toast.error(e.message) },
      ),
    onRename: handleRename,
    onMove: (node: Node) => void handleMove([node]),
    onDeleteRequest: handleDeleteRequest,
  };

  if (spaces.isSuccess && !spaces.data.some((s) => s.id === spaceId)) {
    throw notFound();
  }

  const showDetails = browsing && detailsOpen && activeFocused !== null;

  const space = spaces.data?.find((s) => s.id === spaceId);
  // Uploading needs editor rank; the server rejects a viewer, so the toolbar
  // and the drop overlay stay out of their way entirely.
  const canUpload = space?.role === "editor" || space?.role === "owner";
  // Destination for the drop overlay: the space name plus the open folder.
  const destination = [space?.name ?? "this space", dir ? dirs[dir]?.name : undefined]
    .filter(Boolean)
    .join(" / ");

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <DriveHeader
        spaceId={spaceId}
        dir={dir}
        panel={panel}
        detailsOpen={detailsOpen}
        onToggleDetails={() => setDetailsOpen((v) => !v)}
        onTogglePanel={(next) =>
          void navigate({
            to: "/drive/$spaceId",
            params: { spaceId },
            search: { dir, sort, order, view, panel: panel === next ? undefined : next },
          })
        }
      />

      {browsing && (
        <DriveToolbar
          sort={sort}
          order={order}
          view={view}
          canUpload={canUpload}
          onUploadFiles={() => fileRef.current?.click()}
          onUploadFolder={() => folderRef.current?.click()}
          onNewFolder={() => setNewFolderOpen(true)}
          onSortChange={(s) => goTo({ sort: s })}
          onOrderToggle={() => goTo({ order: order === "asc" ? "desc" : "asc" })}
          onViewChange={(v) => goTo({ view: v })}
        />
      )}

      <div className="flex min-h-0 flex-1">
        <DropZone
          spaceId={spaceId}
          parentId={dir}
          destination={destination}
          disabled={!browsing || !canUpload}
        >
          <div className="relative min-h-0 flex-1 overflow-auto">
            {panel === "members" ? (
              <div className="p-4">
                <SpaceMembersPanel
                  spaceId={spaceId}
                  currentUserRole={spaces.data?.find((s) => s.id === spaceId)?.role ?? "viewer"}
                />
              </div>
            ) : panel === "trash" ? (
              <div className="p-4">
                <TrashPanel spaceId={spaceId} />
              </div>
            ) : panel === "settings" ? (
              <div className="p-4">{space && <SpaceSettingsPanel space={space} />}</div>
            ) : panel === "shared" ? (
              <div className="p-4">
                <SharedPanel />
              </div>
            ) : children.isPending ? (
              <p className="text-muted-foreground p-4 text-xs">Loading…</p>
            ) : sorted.length === 0 ? (
              <Empty className="py-12">
                <EmptyHeader>
                  <EmptyMedia>
                    <FolderOpen className="text-muted-foreground size-10" />
                  </EmptyMedia>
                  <EmptyTitle>
                    {filterQuery.trim() ? "No matching files or folders" : "This folder is empty"}
                  </EmptyTitle>
                  <EmptyDescription>
                    {filterQuery.trim()
                      ? "Try a different search."
                      : "Upload a file or create a folder to get started."}
                  </EmptyDescription>
                </EmptyHeader>
              </Empty>
            ) : view === "grid" ? (
              <NodeGrid
                dirs={sortedDirs}
                files={sortedFiles}
                selectedIds={selectedIds}
                starredIds={starredIds}
                focusedId={activeFocused?.id}
                onClick={handleClick}
                onDoubleClick={handleDoubleClick}
                {...nodeHandlers}
              />
            ) : (
              <NodeList
                nodes={sorted}
                selectedIds={selectedIds}
                starredIds={starredIds}
                focusedId={activeFocused?.id}
                onToggle={handleToggle}
                onToggleAll={handleToggleAll}
                onClick={handleClick}
                onDoubleClick={handleDoubleClick}
                {...nodeHandlers}
              />
            )}

            {browsing && (
              <SelectionBar
                selected={selectedNodes}
                onClear={() => setSelectedIds(new Set())}
                onDownload={(node) => window.open(downloadUrl(node), "_blank")}
                onShare={(node) => openPanel("share", node)}
                onRename={handleRename}
                onMove={(nodes) => void handleMove(nodes)}
                onDelete={(nodes) => setConfirmNodes(nodes)}
              />
            )}
          </div>
        </DropZone>

        {showDetails && (
          <aside className="border-border w-80 shrink-0 border-l">
            <DetailsPanel node={activeFocused} onClose={() => setFocusedNode(null)} />
          </aside>
        )}
      </div>

      {/* Two inputs rather than one: `webkitdirectory` is a property of the
          element, not of the click, so picking files and picking a folder
          cannot share a control. */}
      <input
        ref={fileRef}
        type="file"
        multiple
        hidden
        onChange={(e) => {
          enqueueUploads(collectInputFiles(e.target.files), spaceId, dir);
          e.target.value = "";
        }}
      />
      <input
        ref={folderRef}
        type="file"
        multiple
        hidden
        // @ts-expect-error — non-standard, but the only way to pick a folder.
        webkitdirectory=""
        onChange={(e) => {
          enqueueUploads(collectInputFiles(e.target.files), spaceId, dir);
          e.target.value = "";
        }}
      />

      <Dialog
        open={newFolderOpen}
        onOpenChange={(v) => {
          setNewFolderOpen(v);
          if (!v) setNewFolderName("");
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>New folder</DialogTitle>
            <DialogDescription>Create a folder in the current location.</DialogDescription>
          </DialogHeader>
          <form
            onSubmit={(e: FormEvent) => {
              e.preventDefault();
              mkdir.mutate(newFolderName.trim(), {
                onSuccess: () => {
                  setNewFolderOpen(false);
                  setNewFolderName("");
                },
              });
            }}
          >
            <Input
              autoFocus
              value={newFolderName}
              onChange={(e) => setNewFolderName(e.target.value)}
              placeholder="Folder name"
              required
            />
            <DialogFooter className="mt-4">
              <Button type="submit" disabled={!newFolderName.trim() || mkdir.isPending}>
                Create
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      <Dialog open={!!renameNode} onOpenChange={(v) => !v && setRenameNode(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Rename</DialogTitle>
            <DialogDescription>Enter a new name for this item.</DialogDescription>
          </DialogHeader>
          <form
            onSubmit={(e: FormEvent) => {
              e.preventDefault();
              const trimmed = renameName.trim();
              if (trimmed && renameNode && trimmed !== renameNode.name) {
                rename.mutate({ nodeId: renameNode.id, name: trimmed });
              }
              setRenameNode(null);
            }}
          >
            <Input
              autoFocus
              value={renameName}
              onChange={(e) => setRenameName(e.target.value)}
              required
            />
            <DialogFooter className="mt-4">
              <Button
                type="submit"
                disabled={!renameName.trim() || renameNode?.name === renameName.trim()}
              >
                Save
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      <NodePanels />

      <ConfirmDialog
        open={!!confirmNodes}
        onOpenChange={(v) => !v && setConfirmNodes(null)}
        title="Move to trash"
        description={
          confirmNodes?.length === 1
            ? `Are you sure you want to move "${confirmNodes[0].name}" to trash?`
            : `Are you sure you want to move ${confirmNodes?.length ?? 0} items to trash?`
        }
        confirmLabel="Move to trash"
        confirmVariant="destructive"
        onConfirm={handleDeleteConfirmed}
        node={confirmNodes?.length === 1 ? confirmNodes[0] : null}
      />
    </div>
  );
}
