import {
  createFileRoute,
  Link,
  notFound,
  useNavigate,
  useParams,
  useSearch,
} from "@tanstack/react-router";
import { useCallback, useEffect, useState } from "react";
import {
  Ellipsis,
  Eye,
  FileSpreadsheet,
  FileText,
  Folder,
  FolderOpen,
  LayoutGrid,
  List,
  Pencil,
  Trash2,
} from "lucide-react";
import { Button } from "@workspace/ui/components/button";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@workspace/ui/components/table";
import { TooltipProvider } from "@workspace/ui/components/tooltip";
import {
  DropdownMenu,
  DropdownMenuContent,
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
  ContextMenuItem,
  ContextMenuSeparator,
  ContextMenuTrigger,
} from "@workspace/ui/components/context-menu";
import { ConfirmDialog } from "@/components/confirm-dialog";
import {
  fmtSize,
  fmtDate,
  canEdit,
  isOfficeFile,
  openEditor,
  sortNodes,
  type Node,
  type NodeView,
  type SortField,
  type SortOrder,
} from "@/lib/api";
import { childrenOptions, spacesOptions, useDeleteNode } from "@/lib/queries";
import { useUiStore } from "@/lib/ui-store";
import { NodePanels } from "@/components/node-panels";
import { PreviewModal } from "@/components/preview-modal";
import { SortBar } from "@/components/sort-bar";
import { DetailsPanel } from "@/components/preview-panel";

export const Route = createFileRoute("/drive/$spaceId/_index")({
  validateSearch: (
    search: Record<string, unknown>,
  ): { dir?: string; sort?: SortField; order?: SortOrder; view?: NodeView } => ({
    dir: typeof search.dir === "string" && search.dir ? search.dir : undefined,
    sort:
      search.sort === "name" || search.sort === "modified" || search.sort === "size"
        ? (search.sort as SortField)
        : "name",
    order: search.order === "asc" ? "asc" : "desc",
    view: search.view === "list" ? "list" : "grid",
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
            Go to your first space
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
  } = useSearch({ from: "/drive/$spaceId/_index" });
  const navigate = useNavigate();
  const spaces = spacesOptions();
  const rememberDir = useUiStore((s) => s.rememberDir);
  const setLastSpaceId = useUiStore((s) => s.setLastSpaceId);
  const [selectedNode, setSelectedNode] = useState<Node | null>(null);
  const [confirmNode, setConfirmNode] = useState<Node | null>(null);

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

  const sorted = sortNodes(children.data ?? [], sort, order);
  const sortedDirs = sorted.filter((n) => n.kind === "dir");
  const sortedFiles = sorted.filter((n) => n.kind === "file");

  const activeSelected = selectedNode
    ? (children.data?.find((n) => n.id === selectedNode.id) ?? selectedNode)
    : null;

  const handleClick = useCallback((node: Node) => {
    setSelectedNode(node);
  }, []);

  const handleSelect = useCallback((node: Node) => {
    setSelectedNode(node);
  }, []);

  const handleDoubleClick = useCallback(
    (node: Node) => {
      if (node.kind === "dir") {
        rememberDir(node);
        void navigate({
          to: "/drive/$spaceId",
          params: { spaceId },
          search: { dir: node.id },
        });
      } else if (canEdit(node)) {
        openEditor(node);
      } else {
        void PreviewModal.show({ node });
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
    },
    [del, selectedNode],
  );
  const handlePreview = useCallback((node: Node) => {
    void PreviewModal.show({ node });
  }, []);

  return (
    <>
      {/* ── Top bar: Crumbs, Settings, Sort, View toggle ── */}
      <div className="flex items-center justify-between gap-2 border-b px-4 py-2">
        <div className="flex items-center gap-2">
          <Crumbs spaceId={spaceId} dir={dir} />
          <Link to="/drive/$spaceId/settings" params={{ spaceId }}>
            <Button variant="ghost" size="sm">
              Settings
            </Button>
          </Link>
        </div>
        <div className="flex items-center gap-2">
          <SortBar
            dir={dir}
            sort={sort}
            order={order}
            view={view}
            navigate={(s) =>
              void navigate({
                to: "/drive/$spaceId",
                params: { spaceId },
                search: s,
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
                  search: { dir, sort, order, view: "grid" },
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
                  search: { dir, sort, order, view: "list" },
                })
              }
            >
              <List />
            </Button>
          </ButtonGroup>
        </div>
      </div>

      {/* ── Content Area + Details Panel ── */}
      <div className="flex min-h-0 flex-1">
        <div className="min-h-0 flex-1 overflow-auto p-4">
          {children.isPending ? (
            <p className="text-muted-foreground py-8 text-sm">Loading…</p>
          ) : sortedDirs.length === 0 && sortedFiles.length === 0 ? (
            <Empty className="py-12">
              <EmptyHeader>
                <EmptyMedia>
                  <FolderOpen className="size-10 text-muted-foreground" />
                </EmptyMedia>
                <EmptyTitle>This folder is empty</EmptyTitle>
                <EmptyDescription>
                  Upload a file or create a folder to get started.
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
    </>
  );
}

// ── Crumbs ─────────────────────────────────────────────────────────────────

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
    <nav className="flex items-center gap-1 text-sm">
      <button
        className="text-muted-foreground hover:text-foreground flex items-center gap-0.5 transition-colors"
        onClick={() =>
          void navigate({
            to: "/drive/$spaceId",
            params: { spaceId },
            search: { dir: undefined },
          })
        }
      >
        <span className="truncate max-w-[120px]">{space.name}</span>
      </button>
      {trail.map((t) => (
        <span key={t.id} className="flex items-center gap-1">
          <span className="text-muted-foreground">/</span>
          <button
            className="text-muted-foreground hover:text-foreground transition-colors"
            onClick={() =>
              void navigate({
                to: "/drive/$spaceId",
                params: { spaceId },
                search: { dir: t.id },
              })
            }
          >
            {t.name}
          </button>
        </span>
      ))}
    </nav>
  );
}

// ── Node Card ──────────────────────────────────────────────────────────────

interface NodeCardProps {
  node: Node;
  selected: boolean;
  onClick: (node: Node) => void;
  onDoubleClick: (node: Node) => void;
  onDeleteRequest: (node: Node) => void;
  onSelect: (node: Node) => void;
  onPreview: (node: Node) => void;
}

function NodeCard({
  node,
  selected,
  onClick,
  onDoubleClick,
  onDeleteRequest,
  onSelect,
  onPreview,
}: NodeCardProps) {
  const [menuOpen, setMenuOpen] = useState(false);

  return (
    <ContextMenu>
      <ContextMenuTrigger>
        <Card
          className={
            "group relative flex cursor-pointer flex-col gap-2 p-3 transition-shadow" +
            (selected ? " ring-2 ring-primary" : "")
          }
          onClick={() => onClick(node)}
          onDoubleClick={() => onDoubleClick(node)}
        >
          <div className="flex items-start justify-between gap-2">
            <div className="flex size-10 items-center justify-center rounded-lg bg-muted">
              {node.kind === "dir" ? (
                <Folder className="size-5 text-muted-foreground" />
              ) : isOfficeFile(node.name) ? (
                <FileSpreadsheet className="size-5 text-green-600" />
              ) : (
                <FileText className="size-5 text-muted-foreground" />
              )}
            </div>
            <DropdownMenu open={menuOpen} onOpenChange={setMenuOpen}>
              <DropdownMenuTrigger
                render={
                  <Button
                    variant="ghost"
                    size="icon-xs"
                    className="opacity-0 group-hover:opacity-100"
                    onClick={(e) => {
                      e.stopPropagation();
                      onSelect(node);
                    }}
                  />
                }
              >
                <Ellipsis className="size-4" />
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end">
                <ContextMenuItem
                  onSelect={() => {
                    onSelect(node);
                    setMenuOpen(false);
                  }}
                >
                  <Pencil className="mr-2 size-4" /> Rename
                </ContextMenuItem>
                {node.kind === "file" && (
                  <ContextMenuItem
                    onSelect={() => {
                      onPreview(node);
                      setMenuOpen(false);
                    }}
                  >
                    <Eye className="mr-2 size-4" /> Preview
                  </ContextMenuItem>
                )}
                <ContextMenuSeparator />
                <ContextMenuItem
                  className="text-destructive"
                  onSelect={() => {
                    onDeleteRequest(node);
                    setMenuOpen(false);
                  }}
                >
                  <Trash2 className="mr-2 size-4" /> Delete
                </ContextMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          </div>
          <div>
            <p className="truncate text-sm font-medium">{node.name}</p>
            {node.size != null && (
              <p className="text-muted-foreground text-xs">{fmtSize(node.size)}</p>
            )}
          </div>
        </Card>
      </ContextMenuTrigger>
    </ContextMenu>
  );
}

// ── List Row ───────────────────────────────────────────────────────────────

interface NodeRowProps {
  node: Node;
  selected: boolean;
  onClick: (node: Node) => void;
  onDoubleClick: (node: Node) => void;
  onDeleteRequest: (node: Node) => void;
  onPreview: (node: Node) => void;
}

function NodeRow({
  node,
  selected,
  onClick,
  onDoubleClick,
  onDeleteRequest,
  onPreview,
}: NodeRowProps) {
  const [menuOpen, setMenuOpen] = useState(false);

  return (
    <ContextMenu>
      <ContextMenuTrigger>
        <TableRow
          className={"group cursor-pointer" + (selected ? " bg-muted" : "")}
          onClick={() => onClick(node)}
          onDoubleClick={() => onDoubleClick(node)}
        >
          <TableCell>
            <div className="flex items-center gap-2">
              {node.kind === "dir" ? (
                <Folder className="text-muted-foreground size-4" />
              ) : isOfficeFile(node.name) ? (
                <FileSpreadsheet className="text-green-600 size-4" />
              ) : (
                <FileText className="text-muted-foreground size-4" />
              )}
              <span className="truncate">{node.name}</span>
            </div>
          </TableCell>
          <TableCell className="text-muted-foreground">
            {node.modified_at ? fmtDate(node.modified_at) : "—"}
          </TableCell>
          <TableCell className="text-muted-foreground">
            {node.size != null ? fmtSize(node.size) : "—"}
          </TableCell>
          <TableCell className="text-muted-foreground">{node.owner ?? "—"}</TableCell>
          <TableCell>
            <DropdownMenu open={menuOpen} onOpenChange={setMenuOpen}>
              <DropdownMenuTrigger
                render={
                  <Button
                    variant="ghost"
                    size="icon-xs"
                    className="opacity-0 group-hover:opacity-100"
                  />
                }
              >
                <Ellipsis className="size-4" />
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end">
                {node.kind === "file" && (
                  <ContextMenuItem
                    onSelect={() => {
                      onPreview(node);
                      setMenuOpen(false);
                    }}
                  >
                    <Eye className="mr-2 size-4" /> Preview
                  </ContextMenuItem>
                )}
                <ContextMenuSeparator />
                <ContextMenuItem
                  className="text-destructive"
                  onSelect={() => {
                    onDeleteRequest(node);
                    setMenuOpen(false);
                  }}
                >
                  <Trash2 className="mr-2 size-4" /> Delete
                </ContextMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          </TableCell>
        </TableRow>
      </ContextMenuTrigger>
    </ContextMenu>
  );
}
