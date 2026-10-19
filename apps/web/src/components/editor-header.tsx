import type { ReactNode } from "react";
import { Trans, useLingui } from "@lingui/react/macro";
import { Download, FileArchive, FileText, History, Link2 } from "lucide-react";
import { Button } from "@workspace/ui/components/button";
import { Skeleton } from "@workspace/ui/components/skeleton";
import {
  Breadcrumb,
  BreadcrumbList,
  BreadcrumbSeparator,
} from "@workspace/ui/components/breadcrumb";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@workspace/ui/components/dropdown-menu";
import { capsOf, exportDocumentUrl, isMarkdownFile, type Node } from "@/lib/api";
import { useMe, useSpaces } from "@/lib/queries";
import { useUiStore } from "@/lib/ui-store";
import { useFormatters } from "@/lib/format";
import { NodeIcon } from "@/components/node-icon";
import { ShareDialog } from "@/components/share-dialog";
import { DocActions, HeaderLead } from "@/components/header-slot";
import { ExternalBreadcrumb, ListingBreadcrumb } from "@/components/site-header";

/**
 * An open file's part of the shell's chrome — over OnlyOffice's iframe, the
 * block editor, an app's editor or the viewer. What it carries is what the
 * editor itself cannot know: where the file lives, and the pp-hub actions
 * (share, version history, download) that operate on the node. Where it
 * lives (folder trail, `trail`, name) takes the navigation header's
 * breadcrumb; everything about the file itself goes to the document bar
 * under it, beside the open-file tabs: the version line (`status` ends it),
 * then `children` (presence, save state, badges, the panel toggles), then
 * the actions. `trail` (a sub-page's parent documents) sits before the
 * file's name, and `driveDir` is the folder the trail ends in when it is not
 * the node's parent — a sub-page's parent is a document, which the listing
 * cannot open.
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
  // their trail runs through the outside-shares browser.
  const spaces = useSpaces();
  // Version history is not a guest's to read.
  const guest = useMe().data?.kind === "guest";
  const outsider =
    node !== undefined && spaces.isSuccess && !spaces.data.some((s) => s.id === node.space_id);
  // An extension node has no versions and no bytes to download.
  const hasBytes = node?.kind !== "ext";
  const dir = driveDir === undefined ? node?.parent_id : (driveDir ?? undefined);

  return (
    <>
      <HeaderLead>
        {node && spaces.isSuccess ? (
          // No room for the folders on a narrow screen; the rail has them.
          <Breadcrumb className="hidden min-w-0 shrink md:block">
            <BreadcrumbList className="flex-nowrap gap-1 sm:gap-1.5">
              {outsider ? (
                <ExternalBreadcrumb dir={dir} linkLast />
              ) : (
                <ListingBreadcrumb spaceId={node.space_id} dir={dir} linkLast />
              )}
              <BreadcrumbSeparator />
            </BreadcrumbList>
          </Breadcrumb>
        ) : null}
        {trail}
        {node ? (
          <span className="text-foreground flex min-w-0 shrink-[2] items-center gap-1.5 text-xs/relaxed font-medium">
            <NodeIcon node={node} className="size-4 shrink-0" />
            <span className="truncate">{node.name}</span>
          </span>
        ) : (
          <Skeleton className="h-4 w-40" />
        )}
      </HeaderLead>

      <DocActions>
        {node ? (
          <span className="text-muted-foreground mr-1 hidden truncate text-xs tabular-nums xl:inline">
            {[
              version ? t`version ${version}` : null,
              node.size != null ? f.size(node.size) : null,
              status,
            ]
              .filter(Boolean)
              .join(" · ")}
          </span>
        ) : null}
        {children}
        {node && capsOf(node).share && (
          <Button variant="outline" size="sm" onClick={() => void ShareDialog.show({ node })}>
            <Link2 data-icon="inline-start" /> <Trans>Share</Trans>
          </Button>
        )}
        {hasBytes && !guest && (
          <Button
            variant="ghost"
            size="sm"
            disabled={!node}
            onClick={() => node && openPanel("versions", node)}
          >
            <History data-icon="inline-start" /> <Trans>Versions</Trans>
          </Button>
        )}
        {/* A markdown document downloads two ways: the `.md` alone, whose
            `attachment:` references only resolve inside pp-hub, or a zip
            with its images and files beside it. */}
        {!hasBytes ? null : node && capsOf(node).download && isMarkdownFile(node) ? (
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
            // eslint-disable-next-line lingui/no-unlocalized-strings -- API route path
            render={<a href={`/api/v1/nodes/${nodeId}/download`} />}
          >
            <Download />
          </Button>
        ) : null}
      </DocActions>
    </>
  );
}
