import type { ReactNode } from "react";
import { Link } from "@tanstack/react-router";
import { Trans, useLingui } from "@lingui/react/macro";
import { ArrowLeft, Download, FileArchive, FileText, History, Link2 } from "lucide-react";
import { Button } from "@workspace/ui/components/button";
import { Skeleton } from "@workspace/ui/components/skeleton";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@workspace/ui/components/dropdown-menu";
import { capsOf, exportDocumentUrl, isMarkdownFile, type Node } from "@/lib/api";
import { useSpaces } from "@/lib/queries";
import { useUiStore } from "@/lib/ui-store";
import { useFormatters } from "@/lib/format";
import { NodeIcon } from "@/components/node-icon";
import { NotificationsBell } from "@/components/notifications-bell";
import { ShareDialog } from "@/components/share-dialog";

/**
 * The chrome above either editor — OnlyOffice's iframe or the block editor.
 * What it carries is what the editor itself cannot know: where the file
 * lives, and the pp-hub actions (share, version history, download) that
 * operate on the node. `status` ends the subtitle line; `children` sit
 * before the actions (presence, save state, badges). `trail` (a sub-page's
 * parent documents) sits before the file's name, and `driveDir` is the
 * folder "Back to Drive" opens when it is not the node's parent — a
 * sub-page's parent is a document, which the listing cannot open.
 */
export function EditorHeader({
  nodeId,
  node,
  status,
  trail,
  driveDir,
  children,
}: {
  nodeId: string;
  node: Node | undefined;
  status?: string | null;
  trail?: ReactNode;
  /** `null`: the space's top level. Omitted: the node's parent. */
  driveDir?: string | null;
  children?: ReactNode;
}) {
  const { t } = useLingui();
  const f = useFormatters();
  const openPanel = useUiStore((s) => s.openPanel);
  // Named so the extracted message reads `version {version}` rather than
  // `version {0}`: the placeholder name is all a translator gets.
  const version = node?.version;
  // Somebody reached through an outside share has no way into the space;
  // their way back is the outside-shares browser.
  const spaces = useSpaces();
  const outsider =
    node !== undefined && spaces.isSuccess && !spaces.data.some((s) => s.id === node.space_id);
  const dir = driveDir === undefined ? node?.parent_id : (driveDir ?? undefined);

  return (
    <header className="border-border flex h-12 shrink-0 items-center gap-2 border-b px-3">
      <Button
        variant="ghost"
        size="icon-sm"
        aria-label={t`Back to Drive`}
        render={
          !node ? (
            <Link to="/drive" />
          ) : outsider ? (
            <Link to="/drive/shared" search={{ dir }} />
          ) : (
            <Link to="/drive/$spaceId" params={{ spaceId: node.space_id }} search={{ dir }} />
          )
        }
      >
        <ArrowLeft />
      </Button>

      {trail}
      {node ? (
        <NodeIcon node={node} className="size-4 shrink-0" />
      ) : (
        <Skeleton className="size-4 shrink-0" />
      )}
      {node ? (
        <div className="flex min-w-0 flex-col">
          <span className="truncate text-sm font-medium">{node.name}</span>
          <span className="text-muted-foreground truncate text-[10px] tabular-nums">
            {[
              version ? t`version ${version}` : null,
              node.size != null ? f.size(node.size) : null,
              status,
            ]
              .filter(Boolean)
              .join(" · ")}
          </span>
        </div>
      ) : (
        <div className="flex min-w-0 flex-col gap-1">
          <Skeleton className="h-4 w-40" />
          <Skeleton className="h-2.5 w-28" />
        </div>
      )}

      <div className="ml-auto flex shrink-0 items-center gap-1">
        {children}
        {node && capsOf(node).share && (
          <Button variant="outline" size="sm" onClick={() => void ShareDialog.show({ node })}>
            <Link2 data-icon="inline-start" /> <Trans>Share</Trans>
          </Button>
        )}
        <Button
          variant="ghost"
          size="sm"
          disabled={!node}
          onClick={() => node && openPanel("versions", node)}
        >
          <History data-icon="inline-start" /> <Trans>Versions</Trans>
        </Button>
        {/* A markdown document downloads two ways: the `.md` alone, whose
            `attachment:` references only resolve inside pp-hub, or a zip
            with its images and files beside it. */}
        {node && capsOf(node).download && isMarkdownFile(node) ? (
          <DropdownMenu>
            <DropdownMenuTrigger
              render={<Button variant="ghost" size="icon-sm" aria-label={t`Download`} />}
            >
              <Download />
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end">
              <DropdownMenuItem
                // eslint-disable-next-line lingui/no-unlocalized-strings -- API route path and window.open target
                onClick={() => window.open(`/api/v1/nodes/${nodeId}/download`, "_blank")}
              >
                <FileText />
                <Trans>Markdown (.md)</Trans>
              </DropdownMenuItem>
              <DropdownMenuItem
                // eslint-disable-next-line lingui/no-unlocalized-strings -- window.open target
                onClick={() => window.open(exportDocumentUrl(nodeId), "_blank")}
              >
                <FileArchive />
                <Trans>Export with images (.zip)</Trans>
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        ) : node && capsOf(node).download ? (
          <Button
            variant="ghost"
            size="icon-sm"
            aria-label={t`Download`}
            render={<a href={`/api/v1/nodes/${nodeId}/download`} />}
          >
            <Download />
          </Button>
        ) : null}
        <NotificationsBell />
      </div>
    </header>
  );
}
