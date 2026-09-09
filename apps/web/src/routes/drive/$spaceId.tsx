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
  CloudUpload,
  Download,
  Eye,
  FileText,
  Folder,
  FolderPlus,
  History,
  Link2,
  Pencil,
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
import { fmtSize, isOfficeFile, type Node } from "@/lib/api";
import {
  childrenOptions,
  spacesOptions,
  useCreateFolder,
  useDeleteNode,
  useRenameNode,
  useUpload,
} from "@/lib/queries";
import { useUiStore, type OpenPanel } from "@/lib/ui-store";
import { NodePanels } from "@/components/node-panels";

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
  const openPanel = useUiStore((s) => s.openPanel);
  const rememberDir = useUiStore((s) => s.rememberDir);
  const setLastSpaceId = useUiStore((s) => s.setLastSpaceId);

  useEffect(() => {
    setLastSpaceId(spaceId);
  }, [spaceId, setLastSpaceId]);

  // Space deleted elsewhere -> throw caught by this route's notFoundComponent.
  if (spaces.isSuccess && !spaces.data.some((s) => s.id === spaceId)) {
    throw notFound();
  }

  const children = childrenOptions(spaceId, dir);

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex items-center gap-2 border-b px-4 py-2">
        <UploadButton spaceId={spaceId} dir={dir} />
        <NewFolderButton spaceId={spaceId} dir={dir} />
      </div>
      <Crumbs spaceId={spaceId} dir={dir} />
      <div className="min-h-0 flex-1 overflow-auto px-4 pb-4">
        {children.isPending ? (
          <p className="text-muted-foreground py-8 text-sm">Loading…</p>
        ) : children.isError ? (
          <p className="text-destructive py-8 text-sm">{(children.error as Error).message}</p>
        ) : children.data.length === 0 ? (
          <p className="text-muted-foreground py-8 text-sm">This folder is empty.</p>
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
                {children.data.map((n) => (
                  <NodeRow
                    key={n.id}
                    node={n}
                    onOpenFolder={(node) => {
                      rememberDir(node);
                      void navigate({
                        to: "/drive/$spaceId",
                        params: { spaceId },
                        search: { dir: node.id },
                      });
                    }}
                    openPanel={openPanel}
                  />
                ))}
              </TableBody>
            </Table>
          </TooltipProvider>
        )}
      </div>
      <NodePanels />
    </div>
  );
}

/**
 * Breadcrumb trail. The API has no parent-chain endpoint, so ancestors come
 * from dirs we've navigated through (persisted in the UI store); the trail
 * stops at the first unknown id.
 */
function Crumbs({ spaceId, dir }: { spaceId: string; dir?: string }) {
  const spaces = spacesOptions();
  const dirs = useUiStore((s) => s.dirs);
  const navigate = useNavigate();

  const space = spaces.data?.find((s) => s.id === spaceId);
  if (!space) return <div className="px-4 py-2" />;

  const trail: { id: string; name: string }[] = [];
  let cursor = dir;
  const guard = new Set<string>();
  while (cursor && dirs[cursor] && !guard.has(cursor)) {
    guard.add(cursor);
    trail.unshift({ id: cursor, name: dirs[cursor].name });
    cursor = dirs[cursor].parent;
  }

  return (
    <div className="flex flex-wrap items-center gap-1 px-4 py-2 text-sm">
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

function UploadButton({ spaceId, dir }: { spaceId: string; dir?: string }) {
  const fileRef = useRef<HTMLInputElement>(null);
  const upload = useUpload(spaceId, dir);

  return (
    <>
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
      <Button
        size="sm"
        variant="outline"
        disabled={upload.isPending}
        onClick={() => fileRef.current?.click()}
      >
        <CloudUpload /> {upload.isPending ? "Uploading…" : "Upload"}
      </Button>
    </>
  );
}

function NewFolderButton({ spaceId, dir }: { spaceId: string; dir?: string }) {
  const [open, setOpen] = useState(false);
  const [name, setName] = useState("");
  const mkdir = useCreateFolder(spaceId, dir);

  return (
    <Dialog
      open={open}
      onOpenChange={(v) => {
        setOpen(v);
        if (!v) setName("");
      }}
    >
      <Button size="sm" variant="outline" onClick={() => setOpen(true)}>
        <FolderPlus /> New folder
      </Button>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>New folder</DialogTitle>
          <DialogDescription>Create a folder in the current location.</DialogDescription>
        </DialogHeader>
        <form
          onSubmit={(e: FormEvent) => {
            e.preventDefault();
            mkdir.mutate(name.trim(), {
              onSuccess: () => setOpen(false),
            });
          }}
        >
          <Input
            autoFocus
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="Folder name"
            required
          />
          <DialogFooter className="mt-4">
            <Button type="submit" disabled={!name.trim() || mkdir.isPending}>
              Create
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

function NodeRow({
  node,
  onOpenFolder,
  openPanel,
}: {
  node: Node;
  onOpenFolder: (n: Node) => void;
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
    <TableRow>
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
          <span className="flex items-center gap-2">
            <FileText className="size-4 shrink-0 opacity-70" />
            {node.name}
          </span>
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
