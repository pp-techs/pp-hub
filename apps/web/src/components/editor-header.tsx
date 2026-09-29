import type { ReactNode } from "react";
import { Link } from "@tanstack/react-router";
import { Trans, useLingui } from "@lingui/react/macro";
import { ArrowLeft, Download, History, Link2 } from "lucide-react";
import { Button } from "@workspace/ui/components/button";
import { Skeleton } from "@workspace/ui/components/skeleton";
import { capsOf, type Node } from "@/lib/api";
import { useUiStore } from "@/lib/ui-store";
import { useFormatters } from "@/lib/format";
import { NodeIcon } from "@/components/node-icon";
import { ShareDialog } from "@/components/share-dialog";

/**
 * The chrome above either editor — OnlyOffice's iframe or the block editor.
 * What it carries is what the editor itself cannot know: where the file
 * lives, and the pp-hub actions (share, version history, download) that
 * operate on the node. `status` ends the subtitle line; `children` sit
 * before the actions (presence, save state, badges).
 */
export function EditorHeader({
  nodeId,
  node,
  status,
  children,
}: {
  nodeId: string;
  node: Node | undefined;
  status?: string | null;
  children?: ReactNode;
}) {
  const { t } = useLingui();
  const f = useFormatters();
  const openPanel = useUiStore((s) => s.openPanel);
  // Named so the extracted message reads `version {version}` rather than
  // `version {0}`: the placeholder name is all a translator gets.
  const version = node?.version;

  return (
    <header className="border-border flex h-12 shrink-0 items-center gap-2 border-b px-3">
      <Button
        variant="ghost"
        size="icon-sm"
        aria-label={t`Back to Drive`}
        render={
          <Link
            to={node ? ("/drive/$spaceId" as const) : ("/drive" as const)}
            params={node ? { spaceId: node.space_id } : undefined}
            search={node ? { dir: node.parent_id } : undefined}
          />
        }
      >
        <ArrowLeft />
      </Button>

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
        {node && capsOf(node).download && (
          <Button
            variant="ghost"
            size="icon-sm"
            aria-label={t`Download`}
            render={<a href={`/api/v1/nodes/${nodeId}/download`} />}
          >
            <Download />
          </Button>
        )}
      </div>
    </header>
  );
}
