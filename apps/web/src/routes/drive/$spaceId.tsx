import {
  createFileRoute,
  Link,
  notFound,
  useNavigate,
  useParams,
  useSearch,
} from "@tanstack/react-router";
import { useEffect, useRef, useState } from "react";
import type { FormEvent, ReactNode } from "react";
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
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@workspace/ui/components/tabs";
import { ToggleGroup, ToggleGroupItem } from "@workspace/ui/components/toggle-group";
import { Separator } from "@workspace/ui/components/separator";
import { Card } from "@workspace/ui/components/card";
import { cn } from "@workspace/ui/lib/utils";
import { fmtSize, isOfficeFile, type Node } from "@/lib/api";
import {
  childrenOptions,
  meOptions,
  spacesOptions,
  useCreateFolder,
  useDeleteNode,
  useRenameNode,
  useUpload,
} from "@/lib/queries";
import { useUiStore, type OpenPanel } from "@/lib/ui-store";
import { NodePanels } from "@/components/node-panels";
import { PreviewContent } from "@/components/preview-panel";

export const Route = createFileRoute("/drive/$spaceId")({
  head: () => ({ meta: [{ title: "Drive · pp-hub" }] }),
  validateSearch: (search: Record<string, unknown>): { dir?: string } =>
    typeof search.dir === "string" && search.dir ? { dir: search.dir } : {},
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
            Open “{first.name}”
          </Button>
        </Link>
      ) : null}
    </div>
  );
}

function FolderView() {
  const { spaceId } = useParams({ from: "/drive/$spaceId" });
  const { dir } = useSearch({ from: "/drive/$spaceId" });
  const navigate = useNavigate();
  const spaces = spacesOptions();
  const me = meOptions();
  const openPanel = useUiStore((s) => s.openPanel);
  const rememberDir = useUiStore((s) => s.rememberDir);
  const setLastSpaceId = useUiStore((s) => s.setLastSpaceId);
  const setNewSpaceOpen = useUiStore((s) => s.setNewSpaceOpen);

  const [searchQuery, setSearchQuery] = useState("");
  const [view, setView] = useState<"grid" | "list">("grid");
  const [selectedNode, setSelectedNode] = useState<Node | null>(null);
  const [newFolderOpen, setNewFolderOpen] = useState(false);
  const [newFolderName, setNewFolderName] = useState("");

  const fileRef = useRef<HTMLInputElement>(null);
  const upload = useUpload(spaceId, dir);
  const mkdir = useCreateFolder(spaceId, dir);

  useEffect(() => {
    setLastSpaceId(spaceId);
  }, [spaceId, setLastSpaceId]);

  useEffect(() => {
    setSelectedNode(null);
  }, [spaceId, dir]);

  // Space deleted elsewhere -> throw caught by this route's notFoundComponent.
  if (spaces.isSuccess && !spaces.data.some((s) => s.id === spaceId)) {
    throw notFound();
  }

  const children = childrenOptions(spaceId, dir);

  const filtered = searchQuery.trim()
    ? (children.data ?? []).filter((n) =>
        n.name.toLowerCase().includes(searchQuery.trim().toLowerCase()),
      )
    : (children.data ?? []);

  const activeSelected = selectedNode
    ? (children.data?.find((n) => n.id === selectedNode.id) ?? selectedNode)
    : null;

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      {/* Top bar Row 1: Search, Notifications, Avatar, New menu */}
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
          <DropdownMenuTrigger
            render={
              <Button>
                New
                <ChevronDown data-icon="inline-end" />
              </Button>
            }
          />
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
      </div>

      {/* Top bar Row 2: Crumbs & View toggle */}
      <div className="flex items-center justify-between gap-2 border-b px-4 py-2">
        <Crumbs spaceId={spaceId} dir={dir} />
        <ToggleGroup
          size="sm"
          variant="outline"
          value={[view]}
          onValueChange={(val) => {
            if (val.length > 0) setView(val[0] as "grid" | "list");
          }}
        >
          <ToggleGroupItem value="grid" aria-label="Grid view">
            <LayoutGrid />
          </ToggleGroupItem>
          <ToggleGroupItem value="list" aria-label="List view">
            <List />
          </ToggleGroupItem>
        </ToggleGroup>
      </div>

      {/* Content Area + Right Side Panel */}
      <div className="flex min-h-0 flex-1">
        <div className="min-h-0 flex-1 overflow-auto p-4">
          {children.isPending ? (
            <p className="text-muted-foreground py-8 text-sm">Loading…</p>
          ) : children.isError ? (
            <p className="text-destructive py-8 text-sm">{(children.error as Error).message}</p>
          ) : filtered.length === 0 ? (
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
            <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-4 xl:grid-cols-5">
              {filtered.map((n) => (
                <NodeCard
                  key={n.id}
                  node={n}
                  spaceId={spaceId}
                  selected={activeSelected?.id === n.id}
                  onOpenFolder={(node) => {
                    rememberDir(node);
                    setSelectedNode(null);
                    void navigate({
                      to: "/drive/$spaceId",
                      params: { spaceId },
                      search: { dir: node.id },
                    });
                  }}
                  onSelect={(node) => setSelectedNode(node)}
                  openPanel={openPanel}
                />
              ))}
            </div>
          ) : (
            <TooltipProvider>
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Name</TableHead>
                    <TableHead className="w-24">Size</TableHead>
                    <TableHead className="w-12">Ver</TableHead>
                    <TableHead className="w-60 text-right">Actions</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {filtered.map((n) => (
                    <NodeRow
                      key={n.id}
                      node={n}
                      selected={activeSelected?.id === n.id}
                      onOpenFolder={(node) => {
                        rememberDir(node);
                        setSelectedNode(null);
                        void navigate({
                          to: "/drive/$spaceId",
                          params: { spaceId },
                          search: { dir: node.id },
                        });
                      }}
                      onSelect={(node) => setSelectedNode(node)}
                      openPanel={openPanel}
                    />
                  ))}
                </TableBody>
              </Table>
            </TooltipProvider>
          )}
        </div>

        <RightSidePanel node={activeSelected} openPanel={openPanel} />
      </div>

      <NodePanels />
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

function NodeCard({
  node,
  spaceId,
  selected,
  onOpenFolder,
  onSelect,
  openPanel,
}: {
  node: Node;
  spaceId: string;
  selected: boolean;
  onOpenFolder: (n: Node) => void;
  onSelect: (n: Node) => void;
  openPanel: OpenPanel;
}) {
  const [renameOpen, setRenameOpen] = useState(false);
  const [name, setName] = useState(node.name);
  const rename = useRenameNode(spaceId);
  const del = useDeleteNode(spaceId);

  const isSpreadsheet = /\.(xlsx|xls|ods)$/i.test(node.name);

  return (
    <>
      <Card
        className={cn(
          "relative flex flex-col p-3 transition-colors hover:border-foreground/20",
          selected && "ring-ring border-primary ring-2",
        )}
      >
        <div className="flex items-start justify-between gap-1">
          <button
            type="button"
            className="flex flex-1 flex-col items-start gap-2 text-left focus-visible:outline-none"
            onClick={() => {
              if (node.kind === "dir") {
                onOpenFolder(node);
              } else {
                onSelect(node);
              }
            }}
          >
            <div className="flex size-10 items-center justify-center">
              {node.kind === "dir" ? (
                <Folder className="text-muted-foreground size-8" />
              ) : isSpreadsheet ? (
                <FileSpreadsheet className="text-muted-foreground size-8" />
              ) : (
                <FileText className="text-muted-foreground size-8" />
              )}
            </div>
            <div className="w-full min-w-0">
              <p className="truncate text-sm font-medium" title={node.name}>
                {node.name}
              </p>
              <p className="text-muted-foreground text-xs">
                {node.kind === "dir" ? "Folder" : node.size != null ? fmtSize(node.size) : "—"}
              </p>
            </div>
          </button>

          <DropdownMenu>
            <DropdownMenuTrigger
              render={
                <Button variant="ghost" size="icon-xs" aria-label="Actions" className="shrink-0">
                  <Ellipsis />
                </Button>
              }
            />
            <DropdownMenuContent align="end">
              <DropdownMenuGroup>
                {node.kind === "file" && isOfficeFile(node.name) && (
                  <DropdownMenuItem onClick={() => openPanel("edit", node)}>
                    <SquarePen /> Edit
                  </DropdownMenuItem>
                )}
                {node.kind === "file" && (
                  <DropdownMenuItem
                    onClick={() => window.open(`/api/v1/nodes/${node.id}/download`, "_blank")}
                  >
                    <Download /> Download
                  </DropdownMenuItem>
                )}
                {node.kind === "file" && (
                  <DropdownMenuItem onClick={() => openPanel("preview", node)}>
                    <Eye /> Preview
                  </DropdownMenuItem>
                )}
                {node.kind === "file" && (
                  <DropdownMenuItem onClick={() => openPanel("versions", node)}>
                    <History /> Versions
                  </DropdownMenuItem>
                )}
                <DropdownMenuItem onClick={() => openPanel("share", node)}>
                  <Link2 /> Share
                </DropdownMenuItem>
                <DropdownMenuItem
                  onClick={() => {
                    setName(node.name);
                    setRenameOpen(true);
                  }}
                >
                  <Pencil /> Rename
                </DropdownMenuItem>
                <DropdownMenuItem
                  variant="destructive"
                  onClick={() => {
                    if (confirm(`Delete "${node.name}"?`)) del.mutate(node.id);
                  }}
                >
                  <Trash2 /> Delete
                </DropdownMenuItem>
              </DropdownMenuGroup>
            </DropdownMenuContent>
          </DropdownMenu>
        </div>
      </Card>

      <Dialog open={renameOpen} onOpenChange={setRenameOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Rename</DialogTitle>
            <DialogDescription>Enter a new name for this item.</DialogDescription>
          </DialogHeader>
          <form
            onSubmit={(e: FormEvent) => {
              e.preventDefault();
              const trimmed = name.trim();
              if (trimmed && trimmed !== node.name) {
                rename.mutate({ nodeId: node.id, name: trimmed });
              }
              setRenameOpen(false);
            }}
          >
            <Input autoFocus value={name} onChange={(e) => setName(e.target.value)} required />
            <DialogFooter className="mt-4">
              <Button type="submit" disabled={!name.trim() || rename.isPending}>
                Save
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
    </>
  );
}

function NodeRow({
  node,
  selected,
  onOpenFolder,
  onSelect,
  openPanel,
}: {
  node: Node;
  selected: boolean;
  onOpenFolder: (n: Node) => void;
  onSelect: (n: Node) => void;
  openPanel: OpenPanel;
}) {
  const { spaceId } = useParams({ from: "/drive/$spaceId" });
  const rename = useRenameNode(spaceId);
  const del = useDeleteNode(spaceId);
  const [renaming, setRenaming] = useState(false);
  const [name, setName] = useState(node.name);

  const act = (label: string, icon: ReactNode, onClick: () => void, danger = false) => (
    <Tooltip>
      <TooltipTrigger
        render={
          <Button
            variant="ghost"
            size="icon-xs"
            aria-label={label}
            className={danger ? "text-destructive hover:text-destructive" : undefined}
            onClick={onClick}
          >
            {icon}
          </Button>
        }
      />
      <TooltipContent>{label}</TooltipContent>
    </Tooltip>
  );

  return (
    <TableRow className={cn(selected && "bg-muted/50")}>
      <TableCell>
        {node.kind === "dir" ? (
          <button
            type="button"
            className="flex items-center gap-2 hover:underline"
            onClick={() => onOpenFolder(node)}
          >
            <Folder className="size-4 shrink-0" />
            {node.name}
          </button>
        ) : (
          <button
            type="button"
            className="flex items-center gap-2 text-left hover:underline"
            onClick={() => onSelect(node)}
          >
            <FileText className="size-4 shrink-0 opacity-70" />
            {node.name}
          </button>
        )}
      </TableCell>
      <TableCell className="text-muted-foreground">
        {node.size != null ? fmtSize(node.size) : "—"}
      </TableCell>
      <TableCell className="text-muted-foreground">{node.version ?? "—"}</TableCell>
      <TableCell className="text-right">
        {renaming ? (
          <form
            className="flex justify-end gap-1"
            onSubmit={(e: FormEvent) => {
              e.preventDefault();
              const trimmed = name.trim();
              if (trimmed && trimmed !== node.name) {
                rename.mutate({ nodeId: node.id, name: trimmed });
              }
              setRenaming(false);
            }}
          >
            <Input
              autoFocus
              className="h-6 w-44"
              value={name}
              onChange={(e) => setName(e.target.value)}
              onBlur={() => setRenaming(false)}
            />
          </form>
        ) : (
          <div className="flex justify-end gap-0.5">
            {node.kind === "file" && isOfficeFile(node.name)
              ? act("Edit", <SquarePen />, () => openPanel("edit", node))
              : null}
            {node.kind === "file"
              ? act("Download", <Download />, () =>
                  window.open(`/api/v1/nodes/${node.id}/download`, "_blank"),
                )
              : null}
            {node.kind === "file"
              ? act("Preview", <Eye />, () => openPanel("preview", node))
              : null}
            {node.kind === "file"
              ? act("Versions", <History />, () => openPanel("versions", node))
              : null}
            {act("Share", <Link2 />, () => openPanel("share", node))}
            {act("Rename", <Pencil />, () => {
              setName(node.name);
              setRenaming(true);
            })}
            {act(
              "Delete",
              <Trash2 />,
              () => {
                if (confirm(`Delete "${node.name}"?`)) del.mutate(node.id);
              },
              true,
            )}
          </div>
        )}
      </TableCell>
    </TableRow>
  );
}

function RightSidePanel({ node, openPanel }: { node: Node | null; openPanel: OpenPanel }) {
  if (!node) {
    return (
      <aside className="border-border hidden w-72 shrink-0 flex-col items-center justify-center border-l p-4 text-center lg:flex">
        <p className="text-muted-foreground text-sm">Select a file to preview</p>
      </aside>
    );
  }

  const isSpreadsheet = /\.(xlsx|xls|ods)$/i.test(node.name);

  return (
    <aside className="border-border hidden w-72 shrink-0 flex-col gap-3 overflow-y-auto border-l p-3 lg:flex">
      <Tabs defaultValue="preview" className="flex flex-col gap-3">
        <TabsList className="w-full">
          <TabsTrigger value="preview" className="flex-1">
            Preview
          </TabsTrigger>
          <TabsTrigger value="activity" className="flex-1">
            Activity
          </TabsTrigger>
        </TabsList>

        <TabsContent value="preview" className="flex flex-col gap-3">
          <div className="flex items-center gap-3">
            <div className="flex size-10 shrink-0 items-center justify-center">
              {node.kind === "dir" ? (
                <Folder className="text-muted-foreground size-8" />
              ) : isSpreadsheet ? (
                <FileSpreadsheet className="text-muted-foreground size-8" />
              ) : (
                <FileText className="text-muted-foreground size-8" />
              )}
            </div>
            <div className="min-w-0 flex-1">
              <p className="truncate text-sm font-medium" title={node.name}>
                {node.name}
              </p>
              <p className="text-muted-foreground truncate text-xs">
                {node.kind === "dir"
                  ? "Folder"
                  : `${node.mime || "file"} · ${node.size != null ? fmtSize(node.size) : "—"}`}
              </p>
            </div>
          </div>
          {node.kind === "file" && <PreviewContent node={node} />}
        </TabsContent>

        <TabsContent value="activity" className="flex flex-col gap-3">
          <p className="text-muted-foreground text-xs">No activity yet.</p>
          {node.kind === "file" && (
            <Button
              variant="outline"
              size="sm"
              className="justify-start"
              onClick={() => openPanel("versions", node)}
            >
              <History data-icon="inline-start" /> Versions
            </Button>
          )}
        </TabsContent>
      </Tabs>

      <Separator />

      <div className="flex flex-col gap-1">
        {node.kind === "file" && (
          <Button
            variant="ghost"
            size="sm"
            className="w-full justify-start"
            onClick={() => openPanel("preview", node)}
          >
            <Eye data-icon="inline-start" /> Preview
          </Button>
        )}
        {node.kind === "file" && (
          <Button
            variant="ghost"
            size="sm"
            className="w-full justify-start"
            onClick={() => openPanel("versions", node)}
          >
            <History data-icon="inline-start" /> Versions
          </Button>
        )}
        <Button
          variant="ghost"
          size="sm"
          className="w-full justify-start"
          onClick={() => openPanel("share", node)}
        >
          <Link2 data-icon="inline-start" /> Share
        </Button>
      </div>
    </aside>
  );
}
