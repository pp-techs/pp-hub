import { createFileRoute } from "@tanstack/react-router";
import { toast } from "sonner";
import { Trans, useLingui } from "@lingui/react/macro";
import { msg } from "@lingui/core/macro";
import type { I18n, MessageDescriptor } from "@lingui/core";
import { RefreshCw, Save } from "lucide-react";

import { Badge } from "@workspace/ui/components/badge";
import { Button } from "@workspace/ui/components/button";
import { Card } from "@workspace/ui/components/card";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@workspace/ui/components/table";
import { AdminPage } from "@/components/admin-page";
import { SkeletonTableRows } from "@/components/loading";
import type { OpenFile, SaveAllResult, SaveState } from "@/lib/api";
import { useFormatters } from "@/lib/format";
import { pageTitle } from "@/lib/i18n";
import { useAdminOpenFiles, useSaveAdminOpenFile, useSaveAdminOpenFiles } from "@/lib/queries";

export const Route = createFileRoute("/admin/open-files")({
  head: () => ({ meta: [{ title: pageTitle(msg`Open files`) }] }),
  // The `/admin` layout gates the whole section on an instance admin.
  component: OpenFilesPage,
});

/**
 * What holds a file open. Descriptors, not strings: module scope runs before
 * a catalog is active.
 */
const EDITOR_LABELS: Record<OpenFile["editor"], MessageDescriptor> = {
  office: msg`OnlyOffice`,
  blocks: msg`Block editor`,
  other: msg`Other client`,
};

function savedToast(i18n: I18n, result: SaveAllResult) {
  const { unchanged, failed } = result;
  const written = result.queued + result.saved;
  const summary = i18n._(msg`${written} saving, ${unchanged} already saved, ${failed} failed`);
  if (failed > 0) toast.error(summary);
  else toast.success(summary);
}

function LastSaved({ version, at }: { version: number; at: string }) {
  const fmt = useFormatters();
  const ago = fmt.relative(at);
  return (
    <Trans>
      v{version} · {ago}
    </Trans>
  );
}

function stateToast(i18n: I18n, state: SaveState) {
  if (state === "unchanged") toast(i18n._(msg`Already saved`));
  else toast.success(state === "queued" ? i18n._(msg`Saving…`) : i18n._(msg`Saved`));
}

/**
 * The files someone has open in an editor right now: what a redeploy would
 * interrupt. OnlyOffice and the block editor keep a session's edits in memory
 * until it goes quiet, so "Save all" asks every open editor to write its
 * edits as a version before the restart instead of leaving it to the
 * periodic save.
 */
function OpenFilesPage() {
  const { t, i18n } = useLingui();
  const fmt = useFormatters();
  const { data, isLoading, isFetching, refetch } = useAdminOpenFiles();
  const saveAll = useSaveAdminOpenFiles();
  const saveOne = useSaveAdminOpenFile();
  const files = data ?? [];
  const saveable = files.some((f) => f.editor !== "other");

  return (
    <AdminPage
      title={<Trans>Open files</Trans>}
      description={
        <Trans>
          Files open in an editor right now. Save them before restarting the server so nothing typed
          since the last save is lost.
        </Trans>
      }
      actions={
        <>
          <Button variant="outline" onClick={() => void refetch()} disabled={isFetching}>
            <RefreshCw data-icon="inline-start" className={isFetching ? "animate-spin" : ""} />
            <Trans>Refresh</Trans>
          </Button>
          <Button
            disabled={!saveable || saveAll.isPending}
            onClick={() =>
              saveAll.mutate(undefined, {
                onSuccess: (result) => savedToast(i18n, result),
                onError: (error) => toast.error(t`Could not save`, { description: error.message }),
              })
            }
          >
            <Save data-icon="inline-start" /> <Trans>Save all</Trans>
          </Button>
        </>
      }
    >
      <Card className="gap-0 py-0">
        <Table>
          <TableHeader>
            <TableRow className="hover:bg-transparent">
              <TableHead className="h-9 pl-4 text-[11px] tracking-wider uppercase">
                <Trans>File</Trans>
              </TableHead>
              <TableHead className="h-9 w-36 text-[11px] tracking-wider uppercase">
                <Trans>Editor</Trans>
              </TableHead>
              <TableHead className="h-9 w-32 text-[11px] tracking-wider uppercase">
                <Trans>Opened</Trans>
              </TableHead>
              <TableHead className="h-9 w-32 text-[11px] tracking-wider uppercase">
                <Trans>Last active</Trans>
              </TableHead>
              <TableHead className="h-9 w-44 text-[11px] tracking-wider uppercase">
                <Trans>Last saved</Trans>
              </TableHead>
              <TableHead className="h-9 w-28 pr-4">
                <span className="sr-only">
                  <Trans>Actions</Trans>
                </span>
              </TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {isLoading ? (
              <SkeletonTableRows columns={6} />
            ) : files.length === 0 ? (
              <TableRow className="hover:bg-transparent">
                <TableCell colSpan={6} className="text-muted-foreground py-10 text-center text-sm">
                  <Trans>No file is open in an editor.</Trans>
                </TableCell>
              </TableRow>
            ) : (
              files.map((f) => (
                <TableRow key={f.node_id}>
                  <TableCell className="min-w-0 pl-4">
                    <div className="flex min-w-0 flex-col">
                      <span className="truncate text-sm font-medium">{f.name}</span>
                      <span className="text-muted-foreground truncate text-xs">{f.space_name}</span>
                    </div>
                  </TableCell>
                  <TableCell>
                    <Badge variant="outline">{i18n._(EDITOR_LABELS[f.editor])}</Badge>
                  </TableCell>
                  <TableCell className="text-muted-foreground text-xs">
                    {fmt.relative(f.locked_at)}
                  </TableCell>
                  <TableCell className="text-muted-foreground text-xs">
                    {fmt.relative(f.refreshed_at)}
                  </TableCell>
                  <TableCell className="text-muted-foreground text-xs">
                    {f.saved_at ? <LastSaved version={f.version_no ?? 0} at={f.saved_at} /> : "—"}
                  </TableCell>
                  <TableCell className="pr-4 text-right">
                    <Button
                      variant="ghost"
                      size="sm"
                      disabled={f.editor === "other" || saveOne.isPending}
                      onClick={() =>
                        saveOne.mutate(f.node_id, {
                          onSuccess: (state) => stateToast(i18n, state),
                          onError: (error) =>
                            toast.error(t`Could not save`, { description: error.message }),
                        })
                      }
                    >
                      <Save data-icon="inline-start" /> <Trans>Save</Trans>
                    </Button>
                  </TableCell>
                </TableRow>
              ))
            )}
          </TableBody>
        </Table>
      </Card>
    </AdminPage>
  );
}
