import { createFileRoute, useParams } from "@tanstack/react-router";
import { useState } from "react";
import { Plural, Trans, useLingui } from "@lingui/react/macro";
import { msg } from "@lingui/core/macro";
import { ChevronRight, Download, Eye, Info, Shield } from "lucide-react";
import { Button } from "@workspace/ui/components/button";
import { Skeleton } from "@workspace/ui/components/skeleton";
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
  shareDownloadAllUrl,
  shareNodeContentUrl,
  shareNodeDownloadUrl,
  type Node,
} from "@/lib/api";
import { shareInfoOptions, useShareChildren, useShareInfo } from "@/lib/queries";
import { useFormatters } from "@/lib/format";
import { pageTitle } from "@/lib/i18n";
import { NodeIcon } from "@/components/node-icon";
import { LoadingStatus, SkeletonTableRows } from "@/components/loading";

export const Route = createFileRoute("/s/$token")({
  head: () => ({ meta: [{ title: pageTitle(msg`Shared`) }] }),
  loader: ({ context, params }) => {
    void context.queryClient.ensureQueryData(shareInfoOptions(params.token));
  },
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
  const { t } = useLingui();
  const f = useFormatters();
  const info = useShareInfo(token);
  const [trail, setTrail] = useState<Crumb[]>([]);

  const root = info.data?.node;
  const isFolder = root?.kind === "dir";
  const currentDir = trail.at(-1)?.id;
  const children = useShareChildren(token, currentDir, !!isFolder);

  if (info.isPending) {
    return (
      <Shell>
        <LoadingStatus className="flex min-h-0 flex-1 flex-col">
          <div className="bg-muted border-border flex h-8 shrink-0 items-center gap-2 border-b px-4">
            <Skeleton className="bg-background h-3 w-56" />
          </div>
          <div className="flex min-h-0 flex-1 flex-col gap-4 p-4">
            <div className="flex min-w-0 flex-col gap-1">
              <div className="flex items-center gap-2">
                <Skeleton className="size-5 shrink-0" />
                <Skeleton className="h-7 w-48" />
              </div>
              <Skeleton className="my-0.5 h-3 w-16" />
            </div>
            <div className="flex flex-col gap-2.5">
              <Skeleton className="h-3.5 w-3/4" />
              <Skeleton className="h-3.5 w-1/2" />
              <Skeleton className="h-3.5 w-2/3" />
              <Skeleton className="h-3.5 w-3/5" />
            </div>
          </div>
        </LoadingStatus>
      </Shell>
    );
  }

  if (info.isError || !root) {
    return (
      <Shell>
        <Empty className="py-16">
          <EmptyHeader>
            <EmptyMedia>
              <Shield className="size-10" />
            </EmptyMedia>
            <EmptyTitle>
              <Trans>This link is not available</Trans>
            </EmptyTitle>
            <EmptyDescription>
              <Trans>
                It may have expired, been revoked, or never existed. Ask whoever sent it for a new
                one.
              </Trans>
            </EmptyDescription>
          </EmptyHeader>
        </Empty>
      </Shell>
    );
  }

  const canDownload = info.data.can_download;
  const crumbs: Crumb[] = [{ name: root.name }, ...trail];
  const sharedBy = info.data.shared_by;
  const expiresOn = info.data.expires_at ? f.date(info.data.expires_at) : null;
  const itemCount = children.data?.length ?? 0;
  // One message per case rather than a sentence spliced from "shared this"
  // and "folder": no other language is obliged to put those words in that
  // order, and half a sentence cannot be translated.
  const banner = sharedBy ? (
    isFolder ? (
      <Trans>{sharedBy} shared this folder</Trans>
    ) : (
      <Trans>{sharedBy} shared this file</Trans>
    )
  ) : isFolder ? (
    <Trans>This folder was shared with you</Trans>
  ) : (
    <Trans>This file was shared with you</Trans>
  );

  return (
    <Shell>
      <div className="bg-muted border-border flex h-8 shrink-0 items-center gap-2 border-b px-4 text-xs">
        <Info className="text-muted-foreground size-3.5 shrink-0" />
        {/* What the link actually permits, in the visitor's words.

            A link without `can_download` no longer means "look but do not
            save": the server refuses `/content` as well as `/node-download`,
            so no file bytes cross at all. Calling that "view only" promised a
            preview this page cannot deliver and the server will not serve. */}
        <span className="truncate">
          {banner} ·{" "}
          {canDownload ? (
            <Trans>view and download</Trans>
          ) : (
            <Trans>file names and details only</Trans>
          )}
        </span>
        {expiresOn && (
          <span className="text-muted-foreground ml-auto shrink-0 text-[10px] tabular-nums">
            <Trans>Link expires {expiresOn}</Trans>
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
              {root.kind === "file" && root.size != null ? (
                f.size(root.size)
              ) : (
                <Plural value={itemCount} one="# item" other="# items" />
              )}
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
              {isFolder ? <Trans>Download all</Trans> : <Trans>Download</Trans>}
            </Button>
          )}
        </div>

        {isFolder && (
          <>
            <nav aria-label={t`Breadcrumb`} className="flex min-w-0 items-center gap-1.5 text-xs">
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
                      onClick={() => setTrail((prev) => prev.slice(0, i))}
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
              onOpenFolder={(node) =>
                setTrail((prev) => [...prev, { id: node.id, name: node.name }])
              }
            />
          </>
        )}

        <div className="border-border flex items-start gap-2 rounded-lg border p-3">
          <Shield className="text-muted-foreground mt-0.5 size-4 shrink-0" />
          <div className="flex flex-col gap-0.5">
            <p className="text-xs font-medium">
              <Trans>You are viewing a shared link</Trans>
            </p>
            <p className="text-muted-foreground text-xs">
              {canDownload ? (
                <Trans>
                  Anyone with this link can view and download these files. You cannot edit or
                  upload, and you cannot see who else has access.
                </Trans>
              ) : (
                <Trans>
                  This link does not allow downloads, so it shows the names, sizes and dates of
                  these files but cannot open their contents.
                </Trans>
              )}
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
  const { t } = useLingui();
  const f = useFormatters();

  if (!isPending && nodes.length === 0) {
    return (
      <Empty className="py-12">
        <EmptyHeader>
          <EmptyMedia>
            {/* eslint-disable-next-line lingui/no-unlocalized-strings -- node kind discriminator */}
            <NodeIcon node={{ kind: "dir" } as Node} className="size-10" />
          </EmptyMedia>
          <EmptyTitle>
            <Trans>This folder is empty</Trans>
          </EmptyTitle>
          <EmptyDescription>
            <Trans>There is nothing shared here yet.</Trans>
          </EmptyDescription>
        </EmptyHeader>
      </Empty>
    );
  }

  return (
    <Table className="text-xs">
      <TableHeader>
        <TableRow>
          <TableHead>
            <Trans>Name</Trans>
          </TableHead>
          <TableHead className="w-36">
            <Trans>Modified</Trans>
          </TableHead>
          <TableHead className="w-24">
            <Trans>Size</Trans>
          </TableHead>
          <TableHead className="w-24" />
        </TableRow>
      </TableHeader>
      <TableBody>
        {isPending ? (
          <SkeletonTableRows columns={4} />
        ) : (
          nodes.map((n) => {
            // Named local so the extracted message reads `{nodeName}` rather than `{0}`.
            const nodeName = n.name;
            return (
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
                  {f.date(n.modified_at)}
                </TableCell>
                <TableCell className="text-muted-foreground tabular-nums">
                  {n.kind === "file" && n.size != null ? f.size(n.size) : "—"}
                </TableCell>
                <TableCell>
                  {n.kind === "file" && (
                    <div className="flex items-center justify-end gap-1">
                      {/* Both actions hang off the download capability, because
                        both endpoints do: the server enforces `can_download` on
                        `/content` as well as `/node-download`. Offering Preview
                        on a view-only link was a button whose only possible
                        outcome was a 403. */}
                      {canDownload && (
                        <>
                          <Button
                            variant="ghost"
                            size="icon-xs"
                            aria-label={t`Preview ${nodeName}`}
                            render={
                              <a
                                href={shareNodeContentUrl(token, n.id)}
                                target="_blank"
                                rel="noreferrer"
                              />
                            }
                          >
                            <Eye />
                          </Button>
                          <Button
                            variant="ghost"
                            size="icon-xs"
                            aria-label={t`Download ${nodeName}`}
                            render={<a href={shareNodeDownloadUrl(token, n.id)} />}
                          >
                            <Download />
                          </Button>
                        </>
                      )}
                    </div>
                  )}
                </TableCell>
              </TableRow>
            );
          })
        )}
      </TableBody>
    </Table>
  );
}

/** Chrome shared by every state of the page. */
function Shell({ children }: { children: React.ReactNode }) {
  const { t } = useLingui();
  return (
    <div className="bg-background text-foreground flex min-h-svh flex-col">
      <header className="border-border flex h-12 shrink-0 items-center gap-2 border-b px-4">
        <span className="bg-primary text-primary-foreground grid size-5 shrink-0 place-content-center rounded text-[10px] font-semibold">
          {/* eslint-disable-next-line lingui/no-unlocalized-strings -- product monogram */}
          pp
        </span>
        {/* eslint-disable-next-line lingui/no-unlocalized-strings -- product name */}
        <span className="text-sm font-medium">pp-hub</span>
        <span className="bg-border mx-1 h-4 w-px" />
        <span className="text-muted-foreground text-[10px] font-medium tracking-wider uppercase">
          {t`Shared with you`}
        </span>
        <Button variant="outline" size="sm" className="ml-auto" render={<a href="/login" />}>
          <Trans>Sign in</Trans>
        </Button>
      </header>

      {children}

      <footer className="border-border text-muted-foreground mt-auto flex h-8 shrink-0 items-center border-t px-4 text-[10px]">
        <Trans>Served by pp-hub · self-hosted</Trans>
      </footer>
    </div>
  );
}
