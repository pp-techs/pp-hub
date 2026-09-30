import { toast } from "sonner";
import { Trans, useLingui } from "@lingui/react/macro";
import { Download, Eye, RotateCcw } from "lucide-react";
import { Button } from "@workspace/ui/components/button";
import { cn } from "@workspace/ui/lib/utils";
import { Badge } from "@workspace/ui/components/badge";
import { Skeleton } from "@workspace/ui/components/skeleton";
import { LoadingStatus } from "@/components/loading";
import { VersionPreviewModal } from "@/components/version-preview-modal";
import { capsOf, isMarkdownFile, type Node, type Version } from "@/lib/api";
import { useRestoreVersion, useVersions } from "@/lib/queries";
import { useFormatters } from "@/lib/format";

/**
 * Version history for a file, shared by the ⋯ menu's dialog and the preview
 * modal's Versions tab so the two cannot drift apart.
 *
 * Only the current version can be downloaded: the API serves a node's bytes,
 * not a version's, so a per-version download button would have nothing behind
 * it. Restoring makes an old version current, which is the way to get at it —
 * and it is a write, so a reader sees the history without the button.
 *
 * A markdown document's versions can also be viewed rendered: the server
 * parses any of them on request (behind the view capability, like the
 * document itself), and the preview offers the same Restore as the row.
 */
export function VersionList({ node, onRestored }: { node: Node; onRestored?: () => void }) {
  const { t } = useLingui();
  const f = useFormatters();
  const caps = capsOf(node);
  const versions = useVersions(node.id);
  const restore = useRestoreVersion(node.space_id);
  const viewable = caps.view && isMarkdownFile(node);

  if (versions.isPending) {
    return (
      <LoadingStatus>
        <ul className="border-border flex flex-col gap-3 border-l pl-3">
          {[0, 1, 2].map((i) => (
            <li key={i} className="relative flex flex-col gap-1">
              <span
                aria-hidden
                className="bg-border absolute top-1.5 -left-[17px] size-1.5 rounded-full"
              />
              <Skeleton className="my-0.5 h-3 w-20" />
              <Skeleton className="h-2.5 w-28" />
              <Skeleton className="h-6 w-20" />
            </li>
          ))}
        </ul>
      </LoadingStatus>
    );
  }
  if (versions.isError) {
    return <p className="text-destructive text-xs">{(versions.error as Error).message}</p>;
  }

  const current = versions.data[0]?.id;

  const restoreTo = (v: Version) => {
    const versionNo = v.version_no;
    restore.mutate(
      { nodeId: node.id, versionId: v.id },
      {
        onSuccess: () => {
          toast.success(t`Restored version ${versionNo}`, {
            description: t`It is now the current version.`,
          });
          onRestored?.();
        },
        onError: (e) => toast.error(e.message),
      },
    );
  };

  const view = (v: Version) => {
    VersionPreviewModal.show({ node, version: v, canRestore: caps.edit && v.id !== current }).then(
      (restoreIt) => {
        if (restoreIt) restoreTo(v);
      },
      // Reopened on another version before an answer: nothing was decided.
      () => undefined,
    );
  };

  return (
    <div className="flex flex-col gap-3">
      <ul className="border-border flex flex-col gap-3 border-l pl-3">
        {versions.data.map((v) => {
          const versionNo = v.version_no;
          return (
            <li key={v.id} className="relative flex flex-col gap-1">
              {/* Centred on the list's 1px rule: the rule's centre is 12.5px
                  (pl-3 + half the border) left of the item, so the 6px dot's
                  left edge is at -15.5px. Vertically it centres on the 20px
                  title row (top 7px). */}
              <span
                aria-hidden
                className={cn(
                  "absolute top-[7px] -left-[15.5px] size-1.5 rounded-full",
                  v.id === current ? "bg-primary" : "bg-muted-foreground/40",
                )}
              />
              <div className="flex min-h-5 items-center gap-2">
                <span className="text-xs font-medium">
                  <Trans>Version {versionNo}</Trans>
                </span>
                {v.id === current && (
                  <Badge>
                    <Trans>Current</Trans>
                  </Badge>
                )}
              </div>
              <span className="text-muted-foreground text-[10px] tabular-nums">
                {f.relative(v.created_at)} · {f.size(v.size)}
              </span>
              <div className="flex items-center gap-1">
                {viewable && (
                  <Button variant="ghost" size="xs" onClick={() => view(v)}>
                    <Eye data-icon="inline-start" /> <Trans>View</Trans>
                  </Button>
                )}
                {v.id === current
                  ? caps.download && (
                      <Button
                        variant="ghost"
                        size="xs"
                        render={<a href={`/api/v1/nodes/${node.id}/download`} />}
                      >
                        <Download data-icon="inline-start" /> <Trans>Download</Trans>
                      </Button>
                    )
                  : caps.edit && (
                      <Button
                        variant="ghost"
                        size="xs"
                        disabled={restore.isPending}
                        onClick={() => restoreTo(v)}
                      >
                        <RotateCcw data-icon="inline-start" /> <Trans>Restore</Trans>
                      </Button>
                    )}
              </div>
            </li>
          );
        })}
      </ul>
      <p className="text-muted-foreground text-[10px]">
        <Trans>
          Versions are immutable. Restoring makes the selected version current; it does not delete
          anything.
        </Trans>
      </p>
    </div>
  );
}
