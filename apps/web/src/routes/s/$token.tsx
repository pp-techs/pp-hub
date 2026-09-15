import { createFileRoute, useParams } from "@tanstack/react-router";
import { useState } from "react";
import { ChevronRight, Download, Eye, Info, Shield } from "lucide-react";
import { Button } from "@workspace/ui/components/button";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@workspace/ui/components/table";
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@workspace/ui/components/empty";
import {
  fmtDate,
  fmtSize,
  shareDownloadAllUrl,
  shareNodeContentUrl,
  shareNodeDownloadUrl,
  type Node,
  type SharePerm,
} from "@/lib/api";
import { useShareChildren, useShareInfo } from "@/lib/queries";
import { NodeIcon } from "@/components/node-icon";

export const Route = createFileRoute("/s/$token")({
  head: () => ({ meta: [{ title: "Shared · pp-hub" }] }),
  component: ShareLanding,
});

/** A crumb in the visitor's path through the shared subtree. */
interface Crumb {
  id?: string;
  name: string;
}

/**
 * What an anonymous visitor sees. No rail, no account, no app navigation —
 * they have a link, not a session.
 *
 * A shared *folder* is browsable: the server lists its children and verifies
 * that every node id requested sits inside the shared subtree. A shared file
 * shows a single card. Nothing here offers rename, upload, delete or share,
 * and owner names are stripped server-side, so the page cannot leak who works
 * at the organisation.
 */
function ShareLanding() {
  const { token } = useParams({ from: "/s/$token" });
  const info = useShareInfo(token);
  const [trail, setTrail] = useState<Crumb[]>([]);

  const root = info.data?.node;
  const isFolder = root?.kind === "dir";
  const currentDir = trail.at(-1)?.id;
  const children = useShareChildren(token, currentDir, !!isFolder);

  if (info.isPending) {
    return <Shell>{null}</Shell>;
  }

  if (info.isError || !root) {
    return (
      <Shell>
        <Empty className="py-16">
          <EmptyHeader>
            <EmptyMedia>
              <Shield className="size-10" />
            </EmptyMedia>
            <EmptyTitle>This link is not available</EmptyTitle>
            <EmptyDescription>
              It may have expired, been revoked, or never existed. Ask whoever sent it for a new
              one.
            </EmptyDescription>
          </EmptyHeader>
        </Empty>
      </Shell>
    );
  }

  const perm = info.data.perm;
  const canDownload = perm === "download" || perm === "edit";
  const crumbs: Crumb[] = [{ name: root.name }, ...trail];

  return (
    <Shell>
      <div className="bg-muted border-border flex h-8 shrink-0 items-center gap-2 border-b px-4 text-xs">
        <Info className="text-muted-foreground size-3.5 shrink-0" />
        <span className="truncate">
          {info.data.shared_by ? `${info.data.shared_by} shared this` : "Shared with you"}{" "}
          {root.kind === "dir" ? "folder" : "file"} · {describePerm(perm)}
        </span>
        {info.data.expires_at && (
          <span className="text-muted-foreground ml-auto shrink-0 text-[10px] tabular-nums">
            Link expires {fmtDate(info.data.expires_at)}
          </span>
        )}
      </div>

      <div className="flex min-h-0 flex-1 flex-col gap-4 overflow-auto p-4">
        <div className="flex flex-wrap items-start justify-between gap-2">
          <div className="flex min-w-0 flex-col gap-1">
            <div className="flex items-center gap-2">
              <NodeIcon node={root} className="size-5 shrink-0" />
              <h1 className="truncate text-xl font-semibold tracking-tight">{root.name}</h1>
            </div>
            <p className="text-muted-foreground text-xs tabular-nums">
              {root.kind === "file" && root.size != null
                ? fmtSize(root.size)
                : `${children.data?.length ?? 0} items`}
            </p>
          </div>

          {canDownload && (
            <Button
              render={
                <a
                  href={
                    isFolder
                      ? shareDownloadAllUrl(token, currentDir)
                      : shareNodeDownloadUrl(token, root.id)
                  }
                />
              }
            >
              <Download data-icon="inline-start" />
              {isFolder ? "Download all" : "Download"}
            </Button>
          )}
        </div>

        {isFolder && (
          <>
            <nav aria-label="Breadcrumb" className="flex min-w-0 items-center gap-1.5 text-xs">
              {crumbs.map((crumb, i) => (
                <span key={crumb.id ?? "root"} className="flex min-w-0 items-center gap-1.5">
                  {i > 0 && <ChevronRight className="text-border size-3.5 shrink-0" />}
                  {i === crumbs.length - 1 ? (
                    <span className="truncate font-medium" aria-current="page">
                      {crumb.name}
                    </span>
                  ) : (
                    <button
                      type="button"
                      className="text-muted-foreground hover:text-foreground truncate transition-colors"
                      onClick={() => setTrail((t) => t.slice(0, i))}
                    >
                      {crumb.name}
                    </button>
                  )}
                </span>
              ))}
            </nav>

            <ShareTable
              token={token}
              nodes={children.data ?? []}
              isPending={children.isPending}
              canDownload={canDownload}
              onOpenFolder={(node) => setTrail((t) => [...t, { id: node.id, name: node.name }])}
            />
          </>
        )}

        <div className="border-border flex items-start gap-2 rounded-lg border p-3">
          <Shield className="text-muted-foreground mt-0.5 size-4 shrink-0" />
          <div className="flex flex-col gap-0.5">
            <p className="text-xs font-medium">You are viewing a shared link</p>
            <p className="text-muted-foreground text-xs">
              Anyone with this link can {canDownload ? "view and download" : "view"} these files.
              You cannot edit or upload, and you cannot see who else has access.
            </p>
          </div>
        </div>
      </div>
    </Shell>
  );
}

interface ShareTableProps {
  token: string;
  nodes: Node[];
  isPending: boolean;
  canDownload: boolean;
  onOpenFolder: (node: Node) => void;
}

function ShareTable({ token, nodes, isPending, canDownload, onOpenFolder }: ShareTableProps) {
  if (isPending) return <p className="text-muted-foreground py-8 text-xs">Loading…</p>;

  if (nodes.length === 0) {
    return (
      <Empty className="py-12">
        <EmptyHeader>
          <EmptyMedia>
            <NodeIcon node={{ kind: "dir" } as Node} className="size-10" />
          </EmptyMedia>
          <EmptyTitle>This folder is empty</EmptyTitle>
          <EmptyDescription>There is nothing shared here yet.</EmptyDescription>
        </EmptyHeader>
      </Empty>
    );
  }

  return (
    <Table className="text-xs">
      <TableHeader>
        <TableRow>
          <TableHead>Name</TableHead>
          <TableHead className="w-36">Modified</TableHead>
          <TableHead className="w-24">Size</TableHead>
          <TableHead className="w-24" />
        </TableRow>
      </TableHeader>
      <TableBody>
        {nodes.map((n) => (
          <TableRow key={n.id}>
            <TableCell>
              {n.kind === "dir" ? (
                <button
                  type="button"
                  className="flex min-w-0 items-center gap-2 text-left"
                  onClick={() => onOpenFolder(n)}
                >
                  <NodeIcon node={n} />
                  <span className="truncate font-medium hover:underline">{n.name}</span>
                </button>
              ) : (
                <div className="flex min-w-0 items-center gap-2">
                  <NodeIcon node={n} />
                  <span className="truncate font-medium">{n.name}</span>
                </div>
              )}
            </TableCell>
            <TableCell className="text-muted-foreground tabular-nums">
              {fmtDate(n.modified_at)}
            </TableCell>
            <TableCell className="text-muted-foreground tabular-nums">
              {n.kind === "file" && n.size != null ? fmtSize(n.size) : "—"}
            </TableCell>
            <TableCell>
              {n.kind === "file" && (
                <div className="flex items-center justify-end gap-1">
                  <Button
                    variant="ghost"
                    size="icon-xs"
                    aria-label={`Preview ${n.name}`}
                    render={
                      <a href={shareNodeContentUrl(token, n.id)} target="_blank" rel="noreferrer" />
                    }
                  >
                    <Eye />
                  </Button>
                  {canDownload && (
                    <Button
                      variant="ghost"
                      size="icon-xs"
                      aria-label={`Download ${n.name}`}
                      render={<a href={shareNodeDownloadUrl(token, n.id)} />}
                    >
                      <Download />
                    </Button>
                  )}
                </div>
              )}
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
}

/** Chrome shared by every state of the page. */
function Shell({ children }: { children: React.ReactNode }) {
  return (
    <div className="bg-background text-foreground flex min-h-svh flex-col">
      <header className="border-border flex h-12 shrink-0 items-center gap-2 border-b px-4">
        <span className="bg-primary text-primary-foreground grid size-5 shrink-0 place-content-center rounded text-[10px] font-semibold">
          pp
        </span>
        <span className="text-sm font-medium">pp-hub</span>
        <span className="bg-border mx-1 h-4 w-px" />
        <span className="text-muted-foreground text-[10px] font-medium tracking-wider uppercase">
          Shared with you
        </span>
        <Button variant="outline" size="sm" className="ml-auto" render={<a href="/login" />}>
          Sign in
        </Button>
      </header>

      {children}

      <footer className="border-border text-muted-foreground mt-auto flex h-8 shrink-0 items-center border-t px-4 text-[10px]">
        Served by pp-hub · self-hosted
      </footer>
    </div>
  );
}

function describePerm(perm: SharePerm): string {
  return perm === "view" ? "view only" : "view and download only";
}
