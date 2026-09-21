import { useQueries } from "@tanstack/react-query";
import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { FormEvent, MouseEvent } from "react";
import { Trans, useLingui } from "@lingui/react/macro";
import { msg, plural } from "@lingui/core/macro";
import { FolderOpen } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@workspace/ui/components/button";
import { Input } from "@workspace/ui/components/input";
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuRadioGroup,
  ContextMenuRadioItem,
  ContextMenuSeparator,
  ContextMenuSub,
  ContextMenuSubContent,
  ContextMenuSubTrigger,
  ContextMenuTrigger,
} from "@workspace/ui/components/context-menu";
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
  opensInEditor,
  openEditor,
  type Node,
  type NodeView,
  type SortField,
  type SortOrder,
} from "@/lib/api";
import {
  childrenOptions,
  nodeOptions,
  useChildren,
  useInvalidateChildren,
  useNode,
  useDeleteNode,
  useCopyNode,
  useMoveNode,
  useRenameNode,
  useSpace,
  useStarred,
  useToggleStar,
} from "@/lib/queries";
import { nextSelection } from "@/lib/selection";
import { useMarqueeSelection } from "@/lib/use-marquee-selection";
import { useKeyedState } from "@/lib/use-keyed-state";
import { useUiStore } from "@/lib/ui-store";
import { useFormatters } from "@/lib/format";
import { pageTitle } from "@/lib/i18n";
import { NodePanels } from "@/components/node-panels";
import { PreviewModal } from "@/components/preview-modal";
import { DetailsPanel } from "@/components/preview-panel";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { DropZone } from "@/components/drop-zone";
import { NodeGrid } from "@/components/node-grid";
import { NodeList } from "@/components/node-list";
import { SelectionBar } from "@/components/selection-bar";
import { MoveDialog } from "@/components/move-dialog";
import { ShareDialog } from "@/components/share-dialog";
import { HeaderActions } from "@/components/header-slot";
import { ListingControls } from "@/components/listing-controls";
import { useDriveWrite } from "@/components/drive-write";
import { folderActions, type FolderAction, type ListingContext } from "@/components/folder-actions";
import { downloadUrl } from "@/components/node-actions";
const EMPTY_SET = new Set<string>();

export const Route = createFileRoute("/drive/$spaceId/")({
  head: () => ({ meta: [{ title: pageTitle(msg`Drive`) }] }),
  validateSearch: (
    search: Record<string, unknown>,
  ): {
    dir?: string;
    sort?: SortField;
    order?: SortOrder;
    view?: NodeView;
  } => ({
    dir: typeof search.dir === "string" ? search.dir : undefined,
    // eslint-disable-next-line lingui/no-unlocalized-strings -- SortField ids
    sort: ["name", "modified", "size"].includes(search.sort as string)
      ? (search.sort as SortField)
      : undefined,
    // eslint-disable-next-line lingui/no-unlocalized-strings -- SortOrder ids
    order: ["asc", "desc"].includes(search.order as string)
      ? (search.order as SortOrder)
      : undefined,
    // eslint-disable-next-line lingui/no-unlocalized-strings -- NodeView ids
    view: ["grid", "list"].includes(search.view as string) ? (search.view as NodeView) : undefined,
  }),
  loaderDeps: ({ search: { dir } }) => ({ dir }),
  loader: ({ context, params, deps: { dir } }) => {
    void context.queryClient.ensureQueryData(childrenOptions(params.spaceId, dir));
  },
  component: FolderView,
});

function FolderView() {
  const { t } = useLingui();
  const f = useFormatters();
  const { spaceId } = Route.useParams();
  const { dir, sort: sortParam, order: orderParam, view: viewParam } = Route.useSearch();
  const navigate = useNavigate();
  const space = useSpace(spaceId);
  const rememberDir = useUiStore((s) => s.rememberDir);
  const filterQuery = useUiStore((s) => s.filterQuery);
  // The URL wins so a copied link keeps the view it was copied with; the
  // preference is only the fallback for a link that says nothing.
  const prefs = useUiStore((s) => s.listingPrefs);
  const sort = sortParam ?? prefs.sort;
  const order = orderParam ?? prefs.order;
  const view = viewParam ?? prefs.view;

  /** The clicked nodes — drives the action bar and the details panel alike. */
  const selectionKey = `${spaceId}:${dir ?? ""}`;
  const [selectedIds, setSelectedIds] = useKeyedState<string, Set<string>>(selectionKey, EMPTY_SET);
  /** Anchor for shift-click ranges: the last node clicked without a modifier. */
  const anchorId = useRef<string | null>(null);
  const prevKeyRef = useRef(selectionKey);
  if (prevKeyRef.current !== selectionKey) {
    prevKeyRef.current = selectionKey;
    anchorId.current = null;
  }
  const detailsOpen = useUiStore((s) => s.detailsOpen);
  const setDetailsOpen = useUiStore((s) => s.setDetailsOpen);
  const [renameNode, setRenameNode] = useState<Node | null>(null);
  const [renameName, setRenameName] = useState("");
  const [confirmNodes, setConfirmNodes] = useState<Node[] | null>(null);
  /** The scrolling listing, which the marquee bands over. */
  const listingRef = useRef<HTMLDivElement>(null);
  // Where a create, an upload or a paste lands — derived from the URL by the
  // shell, so the rail's New menu and this surface cannot disagree about it.
  const write = useDriveWrite();
  const setClipboard = useUiStore((s) => s.setClipboard);
  const invalidateChildren = useInvalidateChildren(spaceId);
  const rename = useRenameNode(spaceId);
  const move = useMoveNode(spaceId);
  const copy = useCopyNode(spaceId);
  const toggleStar = useToggleStar();
  // One request for the whole starred set beats one per visible row.
  const starred = useStarred(spaceId, 200);
  // The share badge reads `node.shared` straight off each listing row now —
  // set server-side alongside caps — so this surface needs no separate
  // "shared with me" query just to paint an icon.
  const del = useDeleteNode(spaceId);

  const children = useChildren(spaceId, dir);

  // Record every listed folder in the dirs store so the header breadcrumb can
  // resolve names and parents. Clicking a folder in the sidebar tree — or
  // opening a copied link — never passes through the grid's double-click,
  // which was previously the only place that recorded them.
  useEffect(() => {
    for (const node of children.data ?? []) {
      if (node.kind === "dir") rememberDir(node);
    }
  }, [children.data, rememberDir]);

  const currentFolder = useNode(dir);
  const sorted = useMemo(() => {
    const q = filterQuery.trim().toLowerCase();
    const base = children.data ?? [];
    // The archive is a real entry point (an admin holds a grant on it) but it
    // is not an ordinary file or folder — it should not show up mixed in with
    // them at the space root. It stays reachable through the Quick Access
    // shortcut, and browsing into it (dir === archive_root_id) renders its
    // contents exactly like any other folder.
    const withoutArchive =
      dir === undefined && space?.archive_root_id
        ? base.filter((n) => n.id !== space.archive_root_id)
        : base;
    const filtered = q
      ? withoutArchive.filter((n) => n.name.toLowerCase().includes(q))
      : withoutArchive;
    return f.sort(filtered, sort, order);
  }, [children.data, filterQuery, sort, order, dir, space?.archive_root_id, f]);
  const { sortedDirs, sortedFiles } = useMemo(() => {
    const dirs: Node[] = [];
    const files: Node[] = [];
    for (const n of sorted) {
      if (n.kind === "dir") dirs.push(n);
      else files.push(n);
    }
    return { sortedDirs: dirs, sortedFiles: files };
  }, [sorted]);

  // The listing is the flat order the grid and list both draw from, so it is
  // also what a shift-click range walks.
  const selectedNodes = useMemo(
    () => sorted.filter((n) => selectedIds.has(n.id)),
    [sorted, selectedIds],
  );

  /**
   * A click only ever changes the selection — the details panel is opened by
   * hand, from the header's (i) or the node's Information action.
   */
  const handleClick = useCallback(
    (node: Node, event: MouseEvent) => {
      setSelectedIds((prev) => {
        const next = nextSelection(sorted, { ids: prev, anchorId: anchorId.current }, node.id, {
          toggle: event.metaKey || event.ctrlKey,
          range: event.shiftKey,
        });
        anchorId.current = next.anchorId;
        return next.ids;
      });
    },
    [sorted, setSelectedIds],
  );

  /**
   * Dragging across the listing bands nodes in; a press that never moves is
   * an ordinary background click, which arrives here as an empty band and so
   * clears the selection. Holding ⌘/Ctrl/Shift adds to what was already
   * selected when the drag began.
   */
  const dragBase = useRef<Set<string>>(new Set());
  const marquee = useMarqueeSelection(listingRef, {
    enabled: true,
    onStart: () => {
      dragBase.current = selectedIds;
    },
    onChange: (ids, additive) => {
      anchorId.current = null;
      setSelectedIds((prev) => {
        const next = additive ? new Set([...dragBase.current, ...ids]) : ids;
        return prev.size === 0 && next.size === 0 ? prev : next;
      });
    },
  });

  const handleDoubleClick = useCallback(
    (node: Node) => {
      if (node.kind === "dir") {
        rememberDir(node);
        void navigate({
          to: "/drive/$spaceId",
          params: { spaceId },
          search: { dir: node.id, sort, order, view },
        });
      } else if (opensInEditor(node)) {
        openEditor(node);
      } else {
        void PreviewModal.show({ node, siblings: sortedFiles });
      }
    },
    [navigate, spaceId, rememberDir, sort, order, view, sortedFiles],
  );

  /** The node's Information action: select it alone, then show the panel. */
  const handleShowDetails = useCallback((node: Node) => {
    setSelectedIds(new Set([node.id]));
    anchorId.current = node.id;
    setDetailsOpen(true);
  }, []);

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
      const target = await MoveDialog.show({
        spaceId,
        spaceName: space?.name ?? t`this space`,
        nodes,
      });
      if (target === null) return; // cancelled — undefined means the space root

      let moved = 0;
      await Promise.all(
        nodes.map((node) =>
          move
            .mutateAsync({ nodeId: node.id, parentId: target })
            .then(() => {
              moved += 1;
            })
            .catch((e: Error) => {
              const nodeName = node.name;
              toast.error(t`Could not move “${nodeName}”`, { description: e.message });
            }),
        ),
      );
      if (moved > 0) {
        toast.success(plural(moved, { one: "Moved # item", other: "Moved # items" }));
        setSelectedIds(new Set());
      }
    },
    [move, spaceId, space?.name, t],
  );

  const handleDeleteConfirmed = useCallback(() => {
    for (const node of confirmNodes ?? []) del.mutate(node.id);
    const removed = new Set((confirmNodes ?? []).map((n) => n.id));
    setSelectedIds((prev) => new Set([...prev].filter((id) => !removed.has(id))));
  }, [confirmNodes, del]);

  /**
   * How much of other people's work the pending trash would take with it.
   * `foreign_owned` is only resolved by the single-node GET — a listing would
   * need a subtree walk per row — so the folders in the confirmation are
   * fetched on the spot. The queries only exist while the dialog is open, and
   * land in the same cache entry the details panel already uses.
   */
  const confirmFolders = useMemo(
    () => (confirmNodes ?? []).filter((n) => n.kind === "dir"),
    [confirmNodes],
  );
  // Named locals so the trash confirmation reaches a translator as
  // `{nodeName}` and `{count}` rather than as bare indexes.
  const count = confirmNodes?.length ?? 0;
  const nodeName = count === 1 ? (confirmNodes?.[0]?.name ?? "") : "";
  const confirmFolderDetails = useQueries({
    queries: confirmFolders.map((n) => nodeOptions(n.id)),
  });
  const foreignOwned = confirmFolderDetails.reduce(
    (sum, result) => sum + (result.data?.foreign_owned ?? 0),
    0,
  );

  const starredIds = useMemo(() => new Set((starred.data ?? []).map((n) => n.id)), [starred.data]);

  const nodeHandlers = useMemo(
    () => ({
      onPreview: handlePreview,
      onShowDetails: handleShowDetails,
      onCopy: (node: Node) => {
        const nodeName = node.name;
        copy.mutate(
          { nodeId: node.id, parentId: dir },
          {
            onSuccess: (made) => {
              const copyName = made.name;
              toast.success(t`Copied as “${copyName}”`);
            },
            onError: (e) =>
              toast.error(t`Could not copy “${nodeName}”`, { description: e.message }),
          },
        );
      },
      onClipboard: (node: Node, mode: "cut" | "copy") => {
        const nodeName = node.name;
        setClipboard({ mode, spaceId, nodes: [node] });
        toast.success(
          mode === "cut"
            ? t`“${nodeName}” cut — paste it in another folder`
            : t`“${nodeName}” copied — paste it in another folder`,
        );
      },
      onToggleStar: (node: Node, next: boolean) =>
        toggleStar.mutate(
          { nodeId: node.id, starred: next },
          { onError: (e) => toast.error(e.message) },
        ),
      onRename: handleRename,
      onMove: (node: Node) => void handleMove([node]),
      onDeleteRequest: handleDeleteRequest,
    }),
    [
      handlePreview,
      handleShowDetails,
      copy,
      dir,
      setClipboard,
      spaceId,
      toggleStar,
      handleRename,
      handleMove,
      handleDeleteRequest,
      t,
    ],
  );

  /** Writes the listing's own view state to the URL, as the header does. */
  const setSearch = useCallback(
    (next: Record<string, unknown>) =>
      void navigate({
        to: "/drive/$spaceId",
        params: { spaceId },
        search: (prev) => ({ ...prev, ...next }),
      }),
    [navigate, spaceId],
  );

  const listing = useMemo<ListingContext>(
    () => ({
      sort,
      order,
      view,
      count: sorted.length,
      canDescribeFolder: Boolean(currentFolder.data),
      onSort: (nextSort, nextOrder) => setSearch({ sort: nextSort, order: nextOrder }),
      onView: (nextView) => setSearch({ view: nextView }),
      onSelectAll: () => {
        anchorId.current = null;
        setSelectedIds(new Set(sorted.map((n) => n.id)));
      },
      onRefresh: () => void invalidateChildren(),
      // With nothing selected the details panel describes the folder itself,
      // which is what this asks for.
      onFolderInfo: () => {
        setSelectedIds(new Set());
        setDetailsOpen(true);
      },
    }),
    [
      sort,
      order,
      view,
      sorted,
      currentFolder.data,
      setSearch,
      setSelectedIds,
      invalidateChildren,
      setDetailsOpen,
    ],
  );

  const backgroundActions = folderActions(write, listing);

  // The panel follows the selection, and with none it describes the folder the
  // listing is showing — at the space root there is no such node, and it says
  // so itself.
  const panelNodes =
    selectedNodes.length > 0 ? selectedNodes : currentFolder.data ? [currentFolder.data] : [];

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      {/* Sort/view/details live in the shell's one header, put there from here
          so the header never has to sniff the pathname for a listing. */}
      <HeaderActions>
        <ListingControls spaceId={spaceId} />
      </HeaderActions>
      <div className="flex min-h-0 flex-1 overflow-hidden rounded-b-xl">
        <DropZone
          spaceId={spaceId}
          parentId={dir}
          destination={write.label}
          disabled={!write.canWrite}
        >
          <ContextMenu>
            {/* The trigger is the scroller itself, so the menu answers on the
                gaps between rows, below the last one and over the empty state
                — everywhere the folder itself is what was clicked. Rows stop
                the event at their own trigger, so only one menu ever opens. */}
            <ContextMenuTrigger
              render={
                <div
                  ref={listingRef}
                  className="relative min-h-0 flex-1 overflow-auto"
                  onMouseDown={marquee.onMouseDown}
                  onContextMenu={(e: MouseEvent) => {
                    if (
                      (e.target as HTMLElement).closest("[data-node-id],[data-keeps-selection]")
                    ) {
                      return;
                    }
                    // The menu acts on the folder, so it must not read as
                    // acting on whatever was selected a moment ago.
                    setSelectedIds(new Set());
                  }}
                />
              }
            >
              {children.isPending ? (
                <p className="text-muted-foreground p-4 text-xs">
                  <Trans>Loading…</Trans>
                </p>
              ) : sorted.length === 0 ? (
                <Empty className="py-12">
                  <EmptyHeader>
                    <EmptyMedia>
                      <FolderOpen className="text-muted-foreground size-10" />
                    </EmptyMedia>
                    <EmptyTitle>
                      {filterQuery.trim() ? (
                        <Trans>No matching files or folders</Trans>
                      ) : (
                        <Trans>This folder is empty</Trans>
                      )}
                    </EmptyTitle>
                    <EmptyDescription>
                      {filterQuery.trim() ? (
                        <Trans>Try a different search.</Trans>
                      ) : (
                        <Trans>Right-click here, or use New, to add something.</Trans>
                      )}
                    </EmptyDescription>
                  </EmptyHeader>
                </Empty>
              ) : view === "grid" ? (
                <NodeGrid
                  dirs={sortedDirs}
                  canWriteHere={write.canWrite}
                  files={sortedFiles}
                  selectedIds={selectedIds}
                  starredIds={starredIds}
                  onClick={handleClick}
                  onDoubleClick={handleDoubleClick}
                  {...nodeHandlers}
                />
              ) : (
                <NodeList
                  nodes={sorted}
                  canWriteHere={write.canWrite}
                  selectedIds={selectedIds}
                  starredIds={starredIds}
                  onClick={handleClick}
                  onDoubleClick={handleDoubleClick}
                  {...nodeHandlers}
                />
              )}

              {marquee.rect && (
                <div
                  aria-hidden
                  className="border-primary/50 bg-primary/10 pointer-events-none absolute z-20 rounded-xs border"
                  style={marquee.rect}
                />
              )}

              <SelectionBar
                selected={selectedNodes}
                onClear={() => setSelectedIds(new Set())}
                // eslint-disable-next-line lingui/no-unlocalized-strings -- window.open target
                onDownload={(node) => window.open(downloadUrl(node), "_blank")}
                onShare={(node) => void ShareDialog.show({ node })}
                onRename={handleRename}
                onMove={(nodes) => void handleMove(nodes)}
                onDelete={(nodes) => setConfirmNodes(nodes)}
              />
            </ContextMenuTrigger>

            <ContextMenuContent className="w-56">
              {backgroundActions.map((action, i) => (
                <BackgroundMenuItem
                  key={action.key}
                  action={action}
                  startsGroup={i > 0 && backgroundActions[i - 1]!.group !== action.group}
                />
              ))}
            </ContextMenuContent>
          </ContextMenu>
        </DropZone>

        {detailsOpen && (
          <aside className="border-border w-80 shrink-0 border-l">
            <DetailsPanel nodes={panelNodes} onClose={() => setDetailsOpen(false)} />
          </aside>
        )}
      </div>

      <Dialog open={!!renameNode} onOpenChange={(v) => !v && setRenameNode(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>
              <Trans>Rename</Trans>
            </DialogTitle>
            <DialogDescription>
              <Trans>Enter a new name for this item.</Trans>
            </DialogDescription>
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
                <Trans>Save</Trans>
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      <NodePanels />

      <ConfirmDialog
        open={!!confirmNodes}
        onOpenChange={(v) => !v && setConfirmNodes(null)}
        title={t`Move to trash`}
        description={
          confirmNodes?.length === 1
            ? t`Are you sure you want to move "${nodeName}" to trash?`
            : plural(count, {
                one: "Are you sure you want to move # item to trash?",
                other: "Are you sure you want to move # items to trash?",
              })
        }
        confirmLabel={t`Move to trash`}
        // eslint-disable-next-line lingui/no-unlocalized-strings -- Button variant id
        confirmVariant="destructive"
        onConfirm={handleDeleteConfirmed}
        node={confirmNodes?.length === 1 ? confirmNodes[0] : null}
        warning={foreignOwnedWarning(foreignOwned, confirmFolders.length)}
      />
    </div>
  );
}

/**
 * The line that turns "move to trash" from a private act into a shared one:
 * trashing a folder pulls everything inside it out of reach, including work
 * the people who made it can still be using. It never blocks the action — the
 * caller is allowed to do this — it only refuses to let it happen silently.
 */
function foreignOwnedWarning(count: number, folders: number): string | undefined {
  if (count <= 0) return undefined;
  // One whole sentence per case rather than a count spliced into a fragment:
  // "N items" is not a phrase every language assembles the same way, and the
  // plural categories it needs are not the two English has.
  return folders === 1
    ? plural(count, {
        one: "This folder holds # item other people created. They lose access until you restore it.",
        other:
          "This folder holds # items other people created. They lose access until you restore it.",
      })
    : plural(count, {
        one: "These folders hold # item other people created. They lose access until you restore them.",
        other:
          "These folders hold # items other people created. They lose access until you restore them.",
      });
}

/**
 * One background action, with the separator that opens its group. Submenus are
 * radio groups because sort and view are one-of-many, not toggles.
 */
function BackgroundMenuItem({
  action,
  startsGroup,
}: {
  action: FolderAction;
  startsGroup: boolean;
}) {
  const { i18n } = useLingui();
  const choices = action.choices;
  return (
    <>
      {startsGroup && <ContextMenuSeparator />}
      {choices ? (
        <ContextMenuSub>
          <ContextMenuSubTrigger>
            <action.icon /> {i18n._(action.label)}
          </ContextMenuSubTrigger>
          <ContextMenuSubContent className="w-48">
            <ContextMenuRadioGroup value={choices.value} onValueChange={choices.onSelect}>
              {choices.items.map((item) => (
                <ContextMenuRadioItem key={item.value} value={item.value}>
                  {i18n._(item.label)}
                </ContextMenuRadioItem>
              ))}
            </ContextMenuRadioGroup>
          </ContextMenuSubContent>
        </ContextMenuSub>
      ) : (
        <ContextMenuItem disabled={action.disabled} onClick={action.run}>
          <action.icon /> {i18n._(action.label)}
        </ContextMenuItem>
      )}
    </>
  );
}
