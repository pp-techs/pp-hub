import { toast } from "sonner";
import { Trans, useLingui } from "@lingui/react/macro";
import { Download, RotateCcw } from "lucide-react";
import { Button } from "@workspace/ui/components/button";
import { Badge } from "@workspace/ui/components/badge";
import { capsOf, type Node } from "@/lib/api";
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
 */
export function VersionList({ node, onRestored }: { node: Node; onRestored?: () => void }) {
  const { t } = useLingui();
  const f = useFormatters();
  const caps = capsOf(node);
  const versions = useVersions(node.id);
  const restore = useRestoreVersion(node.space_id);

  if (versions.isPending) {
    return (
      <p className="text-muted-foreground text-xs">
        <Trans>Loading…</Trans>
      </p>
    );
  }
  if (versions.isError) {
    return <p className="text-destructive text-xs">{(versions.error as Error).message}</p>;
  }

  const current = versions.data[0]?.id;

  return (
    <div className="flex flex-col gap-3">
      <ul className="border-border flex flex-col gap-3 border-l pl-3">
        {versions.data.map((v) => {
          const versionNo = v.version_no;
          return (
            <li key={v.id} className="relative flex flex-col gap-1">
              <span
                aria-hidden
                className="bg-border absolute top-1.5 -left-[17px] size-1.5 rounded-full"
              />
              <div className="flex items-center gap-2">
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
                        onClick={() => {
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
                        }}
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
