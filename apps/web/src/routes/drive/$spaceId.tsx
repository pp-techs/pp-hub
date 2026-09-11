import {
  createFileRoute,
  Link,
  notFound,
  useNavigate,
  useParams,
  useSearch,
} from "@tanstack/react-router";
import { useCallback, useEffect, useRef, useState } from "react";
import type { FormEvent } from "react";
import {
  Bell,
  ChevronDown,
  CloudUpload,
  Download,
  Ellipsis,
  Eye,
  FileSpreadsheet,
  FileText,
  Folder,
  FolderOpen,
  FolderPlus,
  History,
  LayoutGrid,
  Link2,
  List,
  Pencil,
  Plus,
  Search,
  SquarePen,
  Trash2,
} from "lucide-react";
import { Button } from "@workspace/ui/components/button";
import { Input } from "@workspace/ui/components/input";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@workspace/ui/components/table";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@workspace/ui/components/dialog";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@workspace/ui/components/tooltip";
import { Avatar, AvatarFallback } from "@workspace/ui/components/avatar";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@workspace/ui/components/dropdown-menu";
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@workspace/ui/components/empty";
import { ButtonGroup } from "@workspace/ui/components/button-group";
import { Separator } from "@workspace/ui/components/separator";
import { Card } from "@workspace/ui/components/card";
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuSeparator,
  ContextMenuTrigger,
} from "@workspace/ui/components/context-menu";
import { cn } from "@workspace/ui/lib/utils";
import { ConfirmDialog } from "@/components/confirm-dialog";
import {
  fmtSize,
  fmtDate,
  isOfficeFile,
  openEditor,
  sortNodes,
  type Node,
  type SortField,
  type SortOrder,
  type NodeView,
} from "@/lib/api";
import {
  childrenOptions,
  meOptions,
  spacesOptions,
  useCreateFolder,
  useDeleteNode,
  useRenameNode,
  useUpload,
} from "@/lib/queries";
import { useUiStore } from "@/lib/ui-store";
import { NodePanels } from "@/components/node-panels";
import { PreviewDialog } from "@/components/preview-dialog";
import { SortBar } from "@/components/sort-bar";
import { DetailsPanel } from "@/components/preview-panel";
import { SpaceMembersPanel } from "@/components/space-members-panel";

export const Route = createFileRoute("/drive/$spaceId")({
  head: () => ({ meta: [{ title: "Drive · pp-hub" }] }),
  validateSearch: (
    search: Record<string, unknown>,
  ): { dir?: string; sort?: SortField; order?: SortOrder; view?: NodeView; members?: boolean } => ({
    dir: typeof search.dir === "string" && search.dir ? search.dir : undefined,
    sort:
      search.sort === "name" || search.sort === "modified" || search.sort === "size"
        ? (search.sort as SortField)
        : "name",
    order: search.order === "asc" ? "asc" : "desc",
    view: search.view === "list" ? "list" : "grid",
    members: search.members === true,
  }),
  notFoundComponent: SpaceGone,
  component: FolderView,
});

function SpaceGone() {
  const spaces = spacesOptions();
  const first = spaces.data?.[0];
  return (
    <div className="flex flex-1 flex-col items-center justify-center gap-3 p-6">
      <p className="text-muted-foreground text-sm">This space no longer exists.</p>
      {first ? (
        <Link to="/drive/$spaceId" params={{ spaceId: first.id }} search={{ dir: undefined }}>
          <Button variant="outline" size="sm">
            Open &quot;{first.name}&quot;
          </Button>
        </Link>
      ) : null}
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
    members = false,
  } = useSearch({ from: "/drive/$spaceId" });
  const navigate = useNavigate();
  const spaces = spacesOptions();
  const me = meOptions();
  const rememberDir = useUiStore((s) => s.rememberDir);
  const setLastSpaceId = useUiStore((s) => s.setLastSpaceId);
  const setNewSpaceOpen = useUiStore((s) => s.setNewSpaceOpen);
  const [searchQuery, setSearchQuery] = useState("");
  const [selectedNode, setSelectedNode] = useState<Node | null>(null);
  const [newFolderOpen, setNewFolderOpen] = useState(false);
  const [newFolderName, setNewFolderName] = useState("");
  const [renameNode, setRenameNode] = useState<Node | null>(null);
  const [renameName, setRenameName] = useState("");
  const [confirmNode, setConfirmNode] = useState<Node | null>(null);
  const [dialogNode, setDialogNode] = useState<Node | null>(null);

  const fileRef = useRef<HTMLInputElement>(null);
  const upload = useUpload(spaceId, dir);
  const mkdir = useCreateFolder(spaceId, dir);
  const del = useDeleteNode(spaceId);

  useEffect(() => {
    setLastSpaceId(spaceId);
  }, [spaceId, setLastSpaceId]);

  useEffect(() => {
    setSelectedNode(null);
  }, [spaceId, dir]);

  if (spaces.isSuccess && !spaces.data.some((s) => s.id === spaceId)) {
    throw notFound();
  }

  const children = childrenOptions(spaceId, dir);

  const filtered = searchQuery.trim()
    ? (children.data ?? []).filter((n) =>
        n.name.toLowerCase().includes(searchQuery.trim().toLowerCase()),
      )
    : (children.data ?? []);

  const sorted = sortNodes(filtered, sort, order);
  const sortedDirs = sorted.filter((n) => n.kind === "dir");
  const sortedFiles = sorted.filter((n) => n.kind === "file");

  const activeSelected = selectedNode
    ? (children.data?.find((n) => n.id === selectedNode.id) ?? selectedNode)
    : null;

  // Single click / select
  const handleClick = useCallback((node: Node) => {
    setSelectedNode(node);
  }, []);

  // Select from ⋯ button (same as single click)
  const handleSelect = useCallback((node: Node) => {
    setSelectedNode(node);
  }, []);

  // Double click: navigate into folder, open dialog for file
  const handleDoubleClick = useCallback(
    (node: Node) => {
      if (node.kind === "dir") {
        rememberDir(node);
        void navigate({
          to: "/drive/$spaceId",
          params: { spaceId },
          search: { dir: node.id },
        });
      } else {
        setDialogNode(node);
      }
    },
    [navigate, spaceId, rememberDir],
  );

  const handleDeleteRequest = useCallback((node: Node) => {
    setConfirmNode(node);
  }, []);

  const handleDelete = useCallback(
    (nodeId: string) => {
      del.mutate(nodeId);
      if (selectedNode?.id === nodeId) setSelectedNode(null);
      if (dialogNode?.id === nodeId) setDialogNode(null);
    },
    [del, selectedNode, dialogNode],
  );

  const handleRename = useCallback((node: Node) => {
    setSelectedNode(node);
    setRenameNode(node);
    setRenameName(node.name);
  }, []);

  // Open preview dialog (from context menu)
  const handlePreview = useCallback((node: Node) => {
    setDialogNode(node);
  }, []);

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      {/* ── Top bar Row 1 ── */}
      <div className="flex items-center gap-2 border-b px-4 py-2">
        <div className="relative max-w-xs flex-1">
          <Search className="text-muted-foreground pointer-events-none absolute top-1/2 left-2 size-3.5 -translate-y-1/2" />
          <Input
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
            placeholder="Search"
            className="pl-7"
          />
        </div>
        <div className="flex-1" />
        <Button variant="ghost" size="icon-sm" disabled aria-label="Notifications">
          <Bell />
        </Button>
        <TooltipProvider>
          <Tooltip>
            <TooltipTrigger
              render={
                <span className="inline-flex cursor-default rounded-full">
                  <Avatar size="sm">
                    <AvatarFallback>
                      {(me.data?.name || me.data?.email || "?").charAt(0).toUpperCase()}
                    </AvatarFallback>
                  </Avatar>
                </span>
              }
            />
            <TooltipContent>{me.data?.email}</TooltipContent>
          </Tooltip>
        </TooltipProvider>
        <DropdownMenu>
          <DropdownMenuTrigger>
            <Button>
              New
              <ChevronDown data-icon="inline-end" />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end">
            <DropdownMenuGroup>
              <DropdownMenuItem onClick={() => fileRef.current?.click()}>
                <CloudUpload /> Upload file
              </DropdownMenuItem>
              <DropdownMenuItem onClick={() => setNewFolderOpen(true)}>
                <FolderPlus /> New folder
              </DropdownMenuItem>
              <DropdownMenuItem onClick={() => setNewSpaceOpen(true)}>
                <Plus /> New space
              </DropdownMenuItem>
            </DropdownMenuGroup>
          </DropdownMenuContent>
        </DropdownMenu>

        <input
          ref={fileRef}
          type="file"
          hidden
          onChange={(e) => {
            const f = e.target.files?.[0];
            if (f) upload.mutate(f);
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
                if (trimmed && trimmed !== renameNode?.name && renameNode) {
                  useRenameNode(spaceId).mutate({ nodeId: renameNode.id, name: trimmed });
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
      </div>

      {/* ── Top bar Row 2: Crumbs, Sort, View toggle ── */}
      <div className="flex items-center justify-between gap-2 border-b px-4 py-2">
        <div className="flex items-center gap-2">
          <Crumbs spaceId={spaceId} dir={dir} />
          <Button
            variant={members ? "secondary" : "ghost"}
            size="sm"
            onClick={() =>
              void navigate({
                to: "/drive/$spaceId",
                params: { spaceId },
                search: { dir, sort, order, view, members: !members },
              })
            }
          >
            Members
          </Button>
        </div>
        <div className="flex items-center gap-2">
          {!members && (
            <>
              <SortBar
                dir={dir}
                sort={sort}
                order={order}
                view={view}
                navigate={(s) =>
                  void navigate({
                    to: "/drive/$spaceId",
                    params: { spaceId },
                    search: { ...s, members },
                  })
                }
              />
              <Separator orientation="vertical" className="h-4" />
              <ButtonGroup orientation="horizontal" className="rounded-md border">
                <Button
                  variant="ghost"
                  size="icon-xs"
                  aria-label="Grid view"
                  className={view === "grid" ? "bg-muted" : ""}
                  onClick={() =>
                    void navigate({
                      to: "/drive/$spaceId",
                      params: { spaceId },
                      search: { dir, sort, order, view: "grid", members },
                    })
                  }
                >
                  <LayoutGrid />
                </Button>
                <Button
                  variant="ghost"
                  size="icon-xs"
                  aria-label="List view"
                  className={view === "list" ? "bg-muted" : ""}
                  onClick={() =>
                    void navigate({
                      to: "/drive/$spaceId",
                      params: { spaceId },
                      search: { dir, sort, order, view: "list", members },
                    })
                  }
                >
                  <List />
                </Button>
              </ButtonGroup>
            </>
          )}
        </div>
      </div>

      {/* ── Content Area + Details Panel ── */}
      <div className="flex min-h-0 flex-1">
        <div className="min-h-0 flex-1 overflow-auto p-4">
          {members ? (
            <SpaceMembersPanel
              spaceId={spaceId}
              currentUserRole={spaces.data?.find((s) => s.id === spaceId)?.role ?? "viewer"}
            />
          ) : children.isPending ? (
            <p className="text-muted-foreground py-8 text-sm">Loading…</p>
          ) : sortedDirs.length === 0 && sortedFiles.length === 0 ? (
            <Empty className="py-12">
              <EmptyHeader>
                <EmptyMedia>
                  <FolderOpen className="size-10 text-muted-foreground" />
                </EmptyMedia>
                <EmptyTitle>
                  {searchQuery.trim() ? "No matching files or folders" : "This folder is empty"}
                </EmptyTitle>
                <EmptyDescription>
                  {searchQuery.trim()
                    ? "Try adjusting your search query."
                    : "Upload a file or create a folder to get started."}
                </EmptyDescription>
              </EmptyHeader>
            </Empty>
          ) : view === "grid" ? (
            <div className="flex flex-col gap-6">
              {sortedDirs.length > 0 && (
                <section>
                  <div className="mb-2 flex items-center gap-1.5">
                    <Folder className="text-muted-foreground size-4" />
                    <span className="text-xs font-medium uppercase text-muted-foreground">
                      Folders
                    </span>
                  </div>
                  <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-4 xl:grid-cols-5">
                    {sortedDirs.map((n) => (
                      <NodeCard
                        key={n.id}
                        node={n}
                        selected={activeSelected?.id === n.id}
                        onClick={handleClick}
                        onDoubleClick={handleDoubleClick}
                        onDeleteRequest={handleDeleteRequest}
                        onRename={handleRename}
                        onSelect={handleSelect}
                        onPreview={handlePreview}
                      />
                    ))}
                  </div>
                </section>
              )}
              {sortedFiles.length > 0 && (
                <section>
                  <div className="mb-2 flex items-center gap-1.5">
                    <FileText className="text-muted-foreground size-4" />
                    <span className="text-xs font-medium uppercase text-muted-foreground">
                      Files
                    </span>
                  </div>
                  <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-4 xl:grid-cols-5">
                    {sortedFiles.map((n) => (
                      <NodeCard
                        key={n.id}
                        node={n}
                        selected={activeSelected?.id === n.id}
                        onClick={handleClick}
                        onDoubleClick={handleDoubleClick}
                        onDeleteRequest={handleDeleteRequest}
                        onRename={handleRename}
                        onSelect={handleSelect}
                        onPreview={handlePreview}
                      />
                    ))}
                  </div>
                </section>
              )}
            </div>
          ) : (
            <TooltipProvider>
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Name</TableHead>
                    <TableHead className="w-36">Modified</TableHead>
                    <TableHead className="w-24">Size</TableHead>
                    <TableHead className="w-36">Owner</TableHead>
                    <TableHead className="w-16" />
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {sorted.map((n) => (
                    <NodeRow
                      key={n.id}
                      node={n}
                      selected={activeSelected?.id === n.id}
                      onClick={handleClick}
                      onDoubleClick={handleDoubleClick}
                      onDeleteRequest={handleDeleteRequest}
                      onRename={handleRename}
                      onSelect={handleSelect}
                      onPreview={handlePreview}
                    />
                  ))}
                </TableBody>
              </Table>
            </TooltipProvider>
          )}
        </div>

        {/* Details panel (right sidebar) */}
        {activeSelected && (
          <aside className="border-border flex w-80 shrink-0 flex-col border-l lg:flex">
            <DetailsPanel node={activeSelected} onClose={() => setSelectedNode(null)} />
          </aside>
        )}
      </div>

      {/* Full-screen preview dialog (on double-click) */}
      <PreviewDialog node={dialogNode} onClose={() => setDialogNode(null)} />
      <NodePanels />

      {/* Delete confirmation */}
      <ConfirmDialog
        open={!!confirmNode}
        onOpenChange={(v) => !v && setConfirmNode(null)}
        title="Move to trash"
        description={
          confirmNode
            ? `Are you sure you want to move "${confirmNode.name}" to trash?`
            : "Are you sure you want to delete this item?"
        }
        confirmLabel="Move to trash"
        confirmVariant="destructive"
        onConfirm={() => {
          if (confirmNode) handleDelete(confirmNode.id);
        }}
        node={confirmNode}
      />
    </div>
  );
}

function Crumbs({ spaceId, dir }: { spaceId: string; dir?: string }) {
  const spaces = spacesOptions();
  const dirs = useUiStore((s) => s.dirs);
  const navigate = useNavigate();

  const space = spaces.data?.find((s) => s.id === spaceId);
  if (!space) return <div className="flex-1" />;

  const trail: { id: string; name: string }[] = [];
  let cursor = dir;
  const guard = new Set<string>();
  while (cursor && dirs[cursor] && !guard.has(cursor)) {
    guard.add(cursor);
    trail.unshift({ id: cursor, name: dirs[cursor].name });
    cursor = dirs[cursor].parent;
  }

  return (
    <div className="flex flex-wrap items-center gap-1 text-sm">
      <Link
        to="/drive/$spaceId"
        params={{ spaceId }}
        search={{ dir: undefined }}
        className="text-muted-foreground hover:text-foreground"
      >
        {space.name}
      </Link>
      {trail.map((c) => (
        <span key={c.id} className="flex items-center gap-1">
          <span className="text-muted-foreground">/</span>
          {c.id === dir ? (
            <span className="font-medium">{c.name}</span>
          ) : (
            <button
              type="button"
              className="text-muted-foreground hover:text-foreground"
              onClick={() =>
                void navigate({
                  to: "/drive/$spaceId",
                  params: { spaceId },
                  search: { dir: c.id },
                })
              }
            >
              {c.name}
            </button>
          )}
        </span>
      ))}
    </div>
  );
}

interface NodeCardProps {
  node: Node;
  selected: boolean;
  onClick: (n: Node) => void;
  onDoubleClick: (n: Node) => void;
  onDeleteRequest: (node: Node) => void;
  onRename: (n: Node) => void;
  onSelect: (n: Node) => void;
  onPreview: (n: Node) => void;
}

function NodeCard({
  node,
  selected,
  onClick,
  onDoubleClick,
  onDeleteRequest,
  onRename,
  onSelect,
  onPreview,
}: NodeCardProps) {
  const openPanel = useUiStore((s) => s.openPanel);
  const isSpreadsheet = /\.(xlsx|xls|ods)$/i.test(node.name);
  const isDir = node.kind === "dir";

  return (
    <ContextMenu>
      <ContextMenuTrigger>
        <Card
          className={cn(
            "group relative flex cursor-pointer p-3 transition-colors hover:border-foreground/20",
            isDir ? "flex-row items-center gap-2 bg-muted/50" : "flex-col",
            selected && "ring-ring border-primary ring-2",
          )}
          onClick={() => onClick(node)}
          onDoubleClick={() => onDoubleClick(node)}
        >
          {isDir ? (
            <>
              <Folder className="text-muted-foreground size-5 shrink-0" />
              <p className="truncate text-sm font-medium" title={node.name}>
                {node.name}
              </p>
            </>
          ) : (
            <>
              <div className="flex size-10 items-center justify-center">
                {isSpreadsheet ? (
                  <FileSpreadsheet className="text-muted-foreground size-8" />
                ) : (
                  <FileText className="text-muted-foreground size-8" />
                )}
              </div>
              <p className="truncate text-sm font-medium" title={node.name}>
                {node.name}
              </p>
            </>
          )}
          <div className="absolute right-1 top-1">
            <DropdownMenu>
              <DropdownMenuTrigger>
                <Button
                  variant="ghost"
                  size="icon-xs"
                  className={cn(
                    "hover:bg-foreground/10",
                    !selected && "opacity-0 group-hover:opacity-100",
                  )}
                  aria-label="Actions"
                  onClick={(e) => e.stopPropagation()}
                >
                  <Ellipsis />
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end">
                <DropdownMenuGroup>
                  {node.kind === "file" && (
                    <>
                      <DropdownMenuItem
                        onClick={(e) => {
                          e.stopPropagation();
                          onSelect(node);
                        }}
                      >
                        <Eye /> Preview
                      </DropdownMenuItem>
                      {isOfficeFile(node.name) && (
                        <DropdownMenuItem
                          onClick={(e) => {
                            e.stopPropagation();
                            openEditor(node);
                          }}
                        >
                          <SquarePen /> Edit
                        </DropdownMenuItem>
                      )}
                      <DropdownMenuItem
                        onClick={(e) => {
                          e.stopPropagation();
                          window.open(`/api/v1/nodes/${node.id}/download`, "_blank");
                        }}
                      >
                        <Download /> Download
                      </DropdownMenuItem>
                      <DropdownMenuItem
                        onClick={(e) => {
                          e.stopPropagation();
                          openPanel("versions", node);
                        }}
                      >
                        <History /> Versions
                      </DropdownMenuItem>
                    </>
                  )}
                  <DropdownMenuItem
                    onClick={(e) => {
                      e.stopPropagation();
                      openPanel("share", node);
                    }}
                  >
                    <Link2 /> Share
                  </DropdownMenuItem>
                  <DropdownMenuItem
                    onClick={(e) => {
                      e.stopPropagation();
                      onRename(node);
                    }}
                  >
                    <Pencil /> Rename
                  </DropdownMenuItem>
                  <DropdownMenuItem
                    variant="destructive"
                    onClick={(e) => {
                      e.stopPropagation();
                      onDeleteRequest(node);
                    }}
                  >
                    <Trash2 /> Move to trash
                  </DropdownMenuItem>
                </DropdownMenuGroup>
              </DropdownMenuContent>
            </DropdownMenu>
          </div>
        </Card>
      </ContextMenuTrigger>

      <ContextMenuContent>
        {node.kind === "file" && (
          <>
            <ContextMenuItem onClick={() => onPreview(node)}>
              <Eye data-icon="inline-start" /> Preview
            </ContextMenuItem>
            {isOfficeFile(node.name) && (
              <ContextMenuItem onClick={() => openEditor(node)}>
                <SquarePen data-icon="inline-start" /> Edit
              </ContextMenuItem>
            )}
            <ContextMenuItem
              onClick={() => window.open(`/api/v1/nodes/${node.id}/download`, "_blank")}
            >
              <Download data-icon="inline-start" /> Download
            </ContextMenuItem>
            <ContextMenuItem onClick={() => openPanel("versions", node)}>
              <History data-icon="inline-start" /> Versions
            </ContextMenuItem>
            <ContextMenuSeparator />
          </>
        )}
        <ContextMenuItem onClick={() => openPanel("share", node)}>
          <Link2 data-icon="inline-start" /> Share
        </ContextMenuItem>
        <ContextMenuItem onClick={() => onRename(node)}>
          <Pencil data-icon="inline-start" /> Rename
        </ContextMenuItem>
        <ContextMenuSeparator />
        <ContextMenuItem variant="destructive" onClick={() => onDeleteRequest(node)}>
          <Trash2 data-icon="inline-start" /> Move to trash
        </ContextMenuItem>
      </ContextMenuContent>
    </ContextMenu>
  );
}

// ── List Row ─────────────────────────────────────────────────────────────────

interface NodeRowProps {
  node: Node;
  selected: boolean;
  onClick: (n: Node) => void;
  onDoubleClick: (n: Node) => void;
  onDeleteRequest: (node: Node) => void;
  onRename: (n: Node) => void;
  onSelect: (n: Node) => void;
  onPreview: (n: Node) => void;
}

function NodeRow({
  node,
  selected,
  onClick,
  onDoubleClick,
  onDeleteRequest,
  onRename,
  onSelect,
  onPreview,
}: NodeRowProps) {
  const openPanel = useUiStore((s) => s.openPanel);
  const isSpreadsheet = /\.(xlsx|xls|ods)$/i.test(node.name);

  return (
    <ContextMenu>
      <ContextMenuTrigger className="contents">
        <TableRow
          className={cn("group", selected && "bg-muted/50")}
          onClick={() => onClick(node)}
          onDoubleClick={() => onDoubleClick(node)}
        >
          <TableCell>
            <div className="flex items-center gap-2">
              {node.kind === "dir" ? (
                <Folder className="size-4 shrink-0 text-muted-foreground" />
              ) : isSpreadsheet ? (
                <FileSpreadsheet className="size-4 shrink-0 text-muted-foreground" />
              ) : (
                <FileText className="size-4 shrink-0 opacity-70" />
              )}
              <span className="truncate">{node.name}</span>
            </div>
          </TableCell>
          <TableCell className="text-muted-foreground">
            {node.kind === "file" ? fmtDate(node.modified_at) : "—"}
          </TableCell>
          <TableCell className="text-muted-foreground">
            {node.size != null ? fmtSize(node.size) : "—"}
          </TableCell>
          <TableCell>
            {node.owner ? (
              <div className="flex items-center gap-1.5">
                <Avatar size="sm">
                  <AvatarFallback>{node.owner.charAt(0).toUpperCase()}</AvatarFallback>
                </Avatar>
                <span className="text-muted-foreground truncate text-xs">{node.owner}</span>
              </div>
            ) : (
              <span className="text-muted-foreground">—</span>
            )}
          </TableCell>
          <TableCell>
            <div className="flex justify-end">
              <DropdownMenu>
                <DropdownMenuTrigger>
                  <Button
                    variant="ghost"
                    size="icon-xs"
                    className="opacity-0 group-hover:opacity-100"
                    aria-label="Actions"
                    onClick={(e) => e.stopPropagation()}
                  >
                    <Ellipsis />
                  </Button>
                </DropdownMenuTrigger>
                <DropdownMenuContent align="end">
                  <DropdownMenuGroup>
                    {node.kind === "file" && (
                      <>
                        <DropdownMenuItem
                          onClick={(e) => {
                            e.stopPropagation();
                            onSelect(node);
                          }}
                        >
                          <Eye /> Preview
                        </DropdownMenuItem>
                        {isOfficeFile(node.name) && (
                          <DropdownMenuItem
                            onClick={(e) => {
                              e.stopPropagation();
                              openEditor(node);
                            }}
                          >
                            <SquarePen /> Edit
                          </DropdownMenuItem>
                        )}
                        <DropdownMenuItem
                          onClick={(e) => {
                            e.stopPropagation();
                            window.open(`/api/v1/nodes/${node.id}/download`, "_blank");
                          }}
                        >
                          <Download /> Download
                        </DropdownMenuItem>
                        <DropdownMenuItem
                          onClick={(e) => {
                            e.stopPropagation();
                            openPanel("versions", node);
                          }}
                        >
                          <History /> Versions
                        </DropdownMenuItem>
                      </>
                    )}
                    <DropdownMenuItem
                      onClick={(e) => {
                        e.stopPropagation();
                        openPanel("share", node);
                      }}
                    >
                      <Link2 /> Share
                    </DropdownMenuItem>
                    <DropdownMenuItem
                      onClick={(e) => {
                        e.stopPropagation();
                        onRename(node);
                      }}
                    >
                      <Pencil /> Rename
                    </DropdownMenuItem>
                    <DropdownMenuItem
                      variant="destructive"
                      onClick={(e) => {
                        e.stopPropagation();
                        onDeleteRequest(node);
                      }}
                    >
                      <Trash2 /> Move to trash
                    </DropdownMenuItem>
                  </DropdownMenuGroup>
                </DropdownMenuContent>
              </DropdownMenu>
            </div>
          </TableCell>
        </TableRow>
      </ContextMenuTrigger>

      <ContextMenuContent>
        {node.kind === "file" && (
          <>
            <ContextMenuItem onClick={() => onPreview(node)}>
              <Eye data-icon="inline-start" /> Preview
            </ContextMenuItem>
            {isOfficeFile(node.name) && (
              <ContextMenuItem onClick={() => openEditor(node)}>
                <SquarePen data-icon="inline-start" /> Edit
              </ContextMenuItem>
            )}
            <ContextMenuItem
              onClick={() => window.open(`/api/v1/nodes/${node.id}/download`, "_blank")}
            >
              <Download data-icon="inline-start" /> Download
            </ContextMenuItem>
            <ContextMenuItem onClick={() => openPanel("versions", node)}>
              <History data-icon="inline-start" /> Versions
            </ContextMenuItem>
            <ContextMenuSeparator />
          </>
        )}
        <ContextMenuItem onClick={() => openPanel("share", node)}>
          <Link2 data-icon="inline-start" /> Share
        </ContextMenuItem>
        <ContextMenuItem onClick={() => onRename(node)}>
          <Pencil data-icon="inline-start" /> Rename
        </ContextMenuItem>
        <ContextMenuSeparator />
        <ContextMenuItem
          variant="destructive"
          onClick={() => {
            onDeleteRequest(node);
          }}
        >
          <Trash2 data-icon="inline-start" /> Move to trash
        </ContextMenuItem>
      </ContextMenuContent>
    </ContextMenu>
  );
}
