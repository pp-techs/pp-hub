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
import { Tooltip, TooltipContent, TooltipTrigger } from "@workspace/ui/components/tooltip";
import { NodeIcon } from "@/components/node-icon";
import { ShareDialog } from "@/components/share-dialog";
import { HeaderActions, HeaderLead } from "@/components/header-slot";
import { ExternalBreadcrumb, ListingBreadcrumb, type Crumb } from "@/components/site-header";

/**
 * An open file's part of the shell's chrome — over OnlyOffice's iframe, the
 * block editor, an app's editor or the viewer. What it carries is what the
 * editor itself cannot know: where the file lives, and the pp-hub actions
 * (share, version history, download) that operate on the node. Where it
 * lives (folder trail, `crumbs`, name) takes the navigation header's
 * breadcrumb, folded like every other: what does not fit goes into one `…`
 * menu, on a phone all of it but the name. The file's own controls go to the
 * same header's trailing actions: the version line (`status` ends it), then
 * `children` (presence, save state, badges, the panel toggles), then the
 * actions. `crumbs` (a sub-page's parent documents) sit before the file's
 * name, and `driveDir` is the folder the trail ends in when it is not the
 * node's parent — a sub-page's parent is a document, which the listing
 * cannot open.
 */
export function EditorHeader({
  nodeId,
  node,
  status,
  crumbs,
  driveDir,
  children,
}: {
  nodeId: string;
  node: Node | undefined;
  status?: string | null;
  crumbs?: Crumb[];
  /** `null`: the space's top level. Omitted: the node's parent. */
  driveDir?: string | null;
  children?: ReactNode;
}) {
  const { t } = useLingui();
  const openPanel = useUiStore((s) => s.openPanel);
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
          <Breadcrumb className="min-w-0 shrink">
            <BreadcrumbList className="flex-nowrap gap-1 sm:gap-1.5">
              {outsider ? (
                <ExternalBreadcrumb dir={dir} after={crumbs} linkLast />
              ) : (
                <ListingBreadcrumb spaceId={node.space_id} dir={dir} after={crumbs} linkLast />
              )}
              <BreadcrumbSeparator />
            </BreadcrumbList>
          </Breadcrumb>
        ) : null}
        {node ? (
          <span className="text-foreground flex min-w-0 shrink-[2] items-center gap-1.5 text-xs/relaxed font-medium">
            <NodeIcon node={node} className="size-4 shrink-0" />
            <span className="truncate">{node.name}</span>
          </span>
        ) : (
          <Skeleton className="h-4 w-40" />
        )}
      </HeaderLead>

      <HeaderActions>
        {status ? (
          <span className="text-muted-foreground mr-1 hidden truncate text-xs xl:inline">
            {status}
          </span>
        ) : null}
        {children}
        {node && capsOf(node).share && (
          <Tooltip>
            <TooltipTrigger
              render={
                <Button
                  variant="ghost"
                  size="icon-sm"
                  aria-label={t`Share`}
                  onClick={() => void ShareDialog.show({ node })}
                />
              }
            >
              <Link2 />
            </TooltipTrigger>
            <TooltipContent>
              <Trans>Share</Trans>
            </TooltipContent>
          </Tooltip>
        )}
        {hasBytes && !guest && (
          <Tooltip>
            <TooltipTrigger
              render={
                <Button
                  variant="ghost"
                  size="icon-sm"
                  aria-label={t`Versions`}
                  disabled={!node}
                  onClick={() => node && openPanel("versions", node)}
                />
              }
            >
              <History />
            </TooltipTrigger>
            <TooltipContent>
              <Trans>Versions</Trans>
            </TooltipContent>
          </Tooltip>
        )}
        {/* A markdown document downloads two ways: the `.md` alone, whose
            `attachment:` references only resolve inside pp-hub, or a zip
            with its images and files beside it. */}
        {!hasBytes ? null : node && capsOf(node).download && isMarkdownFile(node) ? (
          <DropdownMenu>
            <Tooltip>
              <TooltipTrigger
                render={
                  <DropdownMenuTrigger
                    render={<Button variant="ghost" size="icon-sm" aria-label={t`Download`} />}
                  />
                }
              >
                <Download />
              </TooltipTrigger>
              <TooltipContent>
                <Trans>Download</Trans>
              </TooltipContent>
            </Tooltip>
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
          <Tooltip>
            <TooltipTrigger
              render={
                <Button
                  variant="ghost"
                  size="icon-sm"
                  aria-label={t`Download`}
                  // eslint-disable-next-line lingui/no-unlocalized-strings -- API route path
                  render={<a href={`/api/v1/nodes/${nodeId}/download`} />}
                />
              }
            >
              <Download />
            </TooltipTrigger>
            <TooltipContent>
              <Trans>Download</Trans>
            </TooltipContent>
          </Tooltip>
        ) : null}
      </HeaderActions>
    </>
  );
}
