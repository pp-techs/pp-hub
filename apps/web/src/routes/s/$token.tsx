import { createFileRoute, useParams } from "@tanstack/react-router";
import { useEffect, useRef, useState } from "react";
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
  createShareEditSession,
  isMarkdownFile,
  isOfficeFile,
  shareDownloadAllUrl,
  shareNodeContentUrl,
  shareNodeDownloadUrl,
  type EditSession,
  type Node,
} from "@/lib/api";
import { shareInfoOptions, useShareChildren, useShareInfo } from "@/lib/queries";
import { usePageStore } from "@/lib/collab/page-store";
import { PageTrail } from "@/components/page-trail";
import { useLocale } from "@/components/locale-provider";
import { useTheme } from "@/components/theme-provider";
import { useFormatters } from "@/lib/format";
import { PRODUCT_NAME, pageTitle } from "@/lib/i18n";
import { BrandMark } from "@/components/brand-mark";
import { NodeIcon } from "@/components/node-icon";
import { LoadingStatus, SkeletonTableRows } from "@/components/loading";
import { SharedMarkdownView } from "@/components/markdown-document-view";

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
 *
 * A markdown document — the shared file, or one opened from a shared folder —
 * is rendered in the page, the way the block editor lays it out, when the
 * link allows downloads (the server gates `/document` exactly as it gates
 * the bytes). Without that the page keeps to names and details.
 */
function ShareLanding() {
  const { token } = useParams({ from: "/s/$token" });
  const { t } = useLingui();
  const f = useFormatters();
  const info = useShareInfo(token);
  const [trail, setTrail] = useState<Crumb[]>([]);
  // The element the page scrolls in. A rendered document's outline follows it.
  const contentRef = useRef<HTMLDivElement>(null);
  // A markdown file opened from the folder listing, shown in place of it.
  const [openDoc, setOpenDoc] = useState<Node | null>(null);
  // A sub-page opened from the document shown (`base`), shown in its place.
  const [page, setPage] = useState<{ base: string; id: string } | null>(null);
  // eslint-disable-next-line lingui/no-unlocalized-strings -- source discriminant
  const pageStore = usePageStore({ kind: "share", token });

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
  const crumbs: Crumb[] = [
    { name: root.name },
    ...trail,
    ...(openDoc ? [{ id: openDoc.id, name: openDoc.name }] : []),
  ];
  const isDoc = isMarkdownFile(root);
  const isOffice = isOfficeFile(root.name);
  const rendersRoot = root.kind === "file" && isDoc;
  const rendersOfficeRoot = root.kind === "file" && isOffice;
  // The markdown document this view shows, and which of its pages is open.
  const baseDoc = openDoc && isMarkdownFile(openDoc) ? openDoc.id : rendersRoot ? root.id : null;
  const shownPage = baseDoc && page?.base === baseDoc ? page.id : baseDoc;
  const openPage = (pageId: string) => {
    if (baseDoc) setPage({ base: baseDoc, id: pageId });
  };
  const pageTrail =
    shownPage && shownPage !== baseDoc ? (
      <PageTrail store={pageStore} pageId={shownPage} onNavigate={openPage} />
    ) : null;
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
 
            A link always grants reading it; `can_download` adds the right to
            save a copy. Saying "view only" is honest: a streamed render is
            still the file's bytes, but the page offers no way to take them. */}
        <span className="truncate">
          {banner} · {canDownload ? <Trans>view and download</Trans> : <Trans>view only</Trans>}
        </span>
        {expiresOn && (
          <span className="text-muted-foreground ml-auto shrink-0 text-[10px] tabular-nums">
            <Trans>Link expires {expiresOn}</Trans>
          </span>
        )}
      </div>

      <div ref={contentRef} className="flex min-h-0 flex-1 flex-col gap-4 overflow-auto p-4">
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
                    openDoc
                      ? shareNodeDownloadUrl(token, shownPage ?? openDoc.id)
                      : isFolder
                        ? shareDownloadAllUrl(token, currentDir)
                        : shareNodeDownloadUrl(token, shownPage ?? root.id)
                  }
                />
              }
            >
              <Download data-icon="inline-start" />
              {isFolder && !openDoc ? <Trans>Download all</Trans> : <Trans>Download</Trans>}
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
                      onClick={() => {
                        setOpenDoc(null);
                        setTrail((prev) => prev.slice(0, i));
                      }}
                    >
                      {crumb.name}
                    </button>
                  )}
                </span>
              ))}
            </nav>

            {openDoc ? (
              isMarkdownFile(openDoc) ? (
                <>
                  {pageTrail}
                  <SharedMarkdownView
                    token={token}
                    nodeId={shownPage ?? openDoc.id}
                    className="py-2"
                    outlineScrollRef={contentRef}
                    onNavigatePage={openPage}
                  />
                </>
              ) : isOfficeFile(openDoc.name) ? (
                <SharedOfficeView token={token} node={openDoc} />
              ) : (
                <SharedFileView token={token} node={openDoc} canDownload={canDownload} />
              )
            ) : (
              <ShareTable
                token={token}
                nodes={children.data ?? []}
                isPending={children.isPending}
                canDownload={canDownload}
                onOpenFolder={(node) =>
                  setTrail((prev) => [...prev, { id: node.id, name: node.name }])
                }
                onOpenDocument={setOpenDoc}
              />
            )}
          </>
        )}

        {rendersRoot && (
          <>
            {pageTrail}
            <SharedMarkdownView
              token={token}
              nodeId={shownPage ?? root.id}
              className="py-2"
              outlineScrollRef={contentRef}
              onNavigatePage={openPage}
            />
          </>
        )}
        {rendersOfficeRoot && <SharedOfficeView token={token} node={root} />}

        {root.kind === "file" && !rendersRoot && !rendersOfficeRoot && (
          <SharedFileView token={token} node={root} canDownload={canDownload} />
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
                  Anyone with this link can view these files. Downloads are off, so there is no
                  button to save a copy. You cannot edit or upload, and you cannot see who else has
                  access.
                </Trans>
              )}
            </p>
          </div>
        </div>
      </div>
    </Shell>
  );
}

/**
 * A file shared on its own, rendered in the page. The dispatch is the
 * authenticated preview's — the browser draws what it can from the link's
 * content URL — with one addition: text is fetched and shown as text, because
 * the browser would otherwise download a `.csv` rather than display it.
 *
 * What cannot be drawn here — an archive — is named and explained.
 */
function SharedFileView({
  token,
  node,
  canDownload,
}: {
  token: string;
  node: Node;
  canDownload: boolean;
}) {
  const { t } = useLingui();
  const mime = node.mime?.split(";")[0]?.trim() ?? "";
  const url = shareNodeContentUrl(token, node.id);

  if (mime === "application/pdf") {
    // Named local so the extracted message reads `{nodeName}` rather than `{0}`.
    const nodeName = node.name;
    return (
      <object
        data={url}
        type="application/pdf"
        className="min-h-[70vh] w-full"
        aria-label={t`Preview ${nodeName}`}
      >
        <SharedFileFallback node={node} canDownload={canDownload} />
      </object>
    );
  }
  // eslint-disable-next-line lingui/no-unlocalized-strings -- MIME type prefix
  if (mime.startsWith("image/")) {
    return <img src={url} alt={node.name} className="max-h-[70vh] w-full object-contain" />;
  }
  // eslint-disable-next-line lingui/no-unlocalized-strings -- MIME type prefix
  if (mime.startsWith("video/")) {
    return <video controls src={url} className="max-h-[70vh] w-full" />;
  }
  // eslint-disable-next-line lingui/no-unlocalized-strings -- MIME type prefix
  if (mime.startsWith("audio/")) {
    return <audio controls src={url} className="w-full" />;
  }
  // eslint-disable-next-line lingui/no-unlocalized-strings -- MIME type prefix
  if (mime.startsWith("text/")) {
    return <SharedTextView url={url} node={node} canDownload={canDownload} />;
  }
  if (isOfficeFile(node.name)) {
    return <SharedOfficeView token={token} node={node} />;
  }
  return <SharedFileFallback node={node} canDownload={canDownload} />;
}

/** Text fetched and shown verbatim: a `.csv`, a `.txt`, a note. */
function SharedTextView({
  url,
  node,
  canDownload,
}: {
  url: string;
  node: Node;
  canDownload: boolean;
}) {
  const [text, setText] = useState<string | null>(null);
  const [failed, setFailed] = useState(false);

  useEffect(() => {
    let cancelled = false;
    setFailed(false);
    fetch(url)
      .then((r) => (r.ok ? r.text() : Promise.reject(new Error(String(r.status)))))
      .then((t) => {
        if (!cancelled) setText(t);
      })
      .catch(() => {
        if (!cancelled) setFailed(true);
      });
    return () => {
      cancelled = true;
    };
  }, [url]);

  if (failed) return <SharedFileFallback node={node} canDownload={canDownload} />;
  if (text === null) return <Skeleton className="h-40 w-full" />;
  return (
    <pre className="bg-muted overflow-auto rounded-md p-3 text-xs whitespace-pre-wrap">{text}</pre>
  );
}

/** Nothing to draw here: say so rather than offer a control that cannot work. */
function SharedFileFallback({ node, canDownload }: { node: Node; canDownload: boolean }) {
  return (
    <Empty className="py-12">
      <EmptyHeader>
        <EmptyMedia>
          <NodeIcon node={node} className="size-10" />
        </EmptyMedia>
        <EmptyTitle>{node.name}</EmptyTitle>
        <EmptyDescription>
          {canDownload ? (
            <Trans>This kind of file cannot be shown here. Download it to open it.</Trans>
          ) : (
            <Trans>
              This kind of file cannot be shown here, and this link does not allow downloads.
            </Trans>
          )}
        </EmptyDescription>
      </EmptyHeader>
    </Empty>
  );
}

/** An office file inside a share link, rendered via OnlyOffice in an iframe. */
function SharedOfficeView({ token, node }: { token: string; node: Node }) {
  const locale = useLocale();
  const { resolvedTheme } = useTheme();
  const [session, setSession] = useState<EditSession | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    createShareEditSession(token, node.id, locale, resolvedTheme)
      .then((s) => {
        if (!cancelled) setSession(s);
      })
      .catch((e) => {
        if (!cancelled) setError(e instanceof Error ? e.message : String(e));
      });
    return () => {
      cancelled = true;
    };
  }, [token, node.id, locale, resolvedTheme]);

  if (error) {
    return (
      <Empty className="py-12">
        <EmptyHeader>
          <EmptyMedia>
            <NodeIcon node={node} className="size-10" />
          </EmptyMedia>
          <EmptyTitle>{node.name}</EmptyTitle>
          <EmptyDescription>{error}</EmptyDescription>
        </EmptyHeader>
      </Empty>
    );
  }

  if (!session) {
    return (
      <div className="flex h-[75vh] min-h-[500px] w-full flex-col gap-2 rounded-lg border p-4">
        <Skeleton className="h-8 w-1/3" />
        <Skeleton className="h-full w-full" />
      </div>
    );
  }

  return (
    <div className="relative flex h-[75vh] min-h-[500px] w-full flex-col overflow-hidden rounded-lg border">
      <ShareOfficeFrame session={session} title={node.name} />
    </div>
  );
}

function ShareOfficeFrame({ session, title }: { session: EditSession; title: string }) {
  const formRef = useRef<HTMLFormElement>(null);
  // eslint-disable-next-line lingui/no-unlocalized-strings -- DOM frame ID
  const frameId = `pp-share-office-${session.token}`;

  useEffect(() => {
    formRef.current?.submit();
  }, []);

  return (
    <>
      <form
        ref={formRef}
        action={session.editor_url}
        method="post"
        target={frameId}
        className="hidden"
      >
        <input type="hidden" name="access_token" value={session.token} />
        <input type="hidden" name="access_token_ttl" value={session.access_token_ttl} />
      </form>
      <iframe
        name={frameId}
        title={title}
        className="size-full border-0"
        allow="clipboard-write"
        allowFullScreen
      />
    </>
  );
}

interface ShareTableProps {
  token: string;
  nodes: Node[];
  isPending: boolean;
  canDownload: boolean;
  onOpenFolder: (node: Node) => void;
  /** Renders a markdown or office file in the page; a link always grants reading it. */
  onOpenDocument: (node: Node) => void;
}

function ShareTable({
  token,
  nodes,
  isPending,
  canDownload,
  onOpenFolder,
  onOpenDocument,
}: ShareTableProps) {
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
            const isDoc = isMarkdownFile(n);
            const isOffice = isOfficeFile(n.name);
            const readable = isDoc || isOffice;
            return (
              <TableRow key={n.id}>
                <TableCell>
                  {n.kind === "dir" || readable ? (
                    <button
                      type="button"
                      className="flex min-w-0 items-center gap-2 text-left"
                      onClick={() => (readable ? onOpenDocument(n) : onOpenFolder(n))}
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
                      {/* Preview reads the file, and view is what a link
                        grants, so it is always offered. Download is the one
                        action that hangs off `can_download`. */}
                      {/* A markdown file previews rendered, in the page;
                        its raw `/content` is source text. */}
                      {readable ? (
                        <Button
                          variant="ghost"
                          size="icon-xs"
                          aria-label={t`Preview ${nodeName}`}
                          onClick={() => onOpenDocument(n)}
                        >
                          <Eye />
                        </Button>
                      ) : (
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
                      )}
                      {canDownload && (
                        <Button
                          variant="ghost"
                          size="icon-xs"
                          aria-label={t`Download ${nodeName}`}
                          render={<a href={shareNodeDownloadUrl(token, n.id)} />}
                        >
                          <Download />
                        </Button>
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
        <BrandMark className="size-6" />
        <span className="text-sm font-medium">{PRODUCT_NAME}</span>
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
        <Trans>Served by {PRODUCT_NAME} · self-hosted</Trans>
      </footer>
    </div>
  );
}
