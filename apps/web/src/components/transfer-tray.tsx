import { Plural, Trans, useLingui } from "@lingui/react/macro";
import { msg } from "@lingui/core/macro";
import type { I18n } from "@lingui/core";
import { useEffect } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { CheckCircle2, ChevronDown, ChevronUp, RotateCcw, X } from "lucide-react";
import { Button } from "@workspace/ui/components/button";
import { cn } from "@workspace/ui/lib/utils";
import { useFormatters } from "@/lib/format";
import { keys } from "@/lib/queries";
import { onUploadSettled, useUploadStore, type Transfer } from "@/lib/upload-store";
import { createUploadInvalidator } from "@/lib/upload-invalidation";

/**
 * Upload queue, anchored bottom-right above everything.
 *
 * Mounted by the drive shell rather than the folder view, so navigating away
 * from the folder you dropped into does not cancel the uploads or hide their
 * progress. Dismissing the tray clears finished rows; it never aborts a
 * transfer that is still running.
 */
export function TransferTray() {
  const qc = useQueryClient();
  const { t } = useLingui();
  const f = useFormatters();
  const transfers = useUploadStore((s) => s.transfers);
  const paused = useUploadStore((s) => s.paused);
  const collapsed = useUploadStore((s) => s.collapsed);
  const dismissed = useUploadStore((s) => s.dismissed);
  const { cancel, retry, clearCompleted, setPaused, setCollapsed, dismiss } =
    useUploadStore.getState();

  // Each finished file changes the listing it landed in, and the space's used
  // bytes. The store cannot reach the query client itself, so the refetch is
  // driven from here: listings refresh at most once per UPLOAD_REFRESH_MS, and
  // the space's used bytes once the queue stops uploading.
  useEffect(() => {
    const invalidator = createUploadInvalidator({
      invalidateFolder: (spaceId, parentId) =>
        void qc.invalidateQueries({ queryKey: keys.children(spaceId, parentId) }),
      invalidateTree: (spaceId) => void qc.invalidateQueries({ queryKey: ["tree", spaceId] }),
      invalidateSpaces: () => void qc.invalidateQueries({ queryKey: keys.spaces }),
      isBusy: () =>
        useUploadStore
          .getState()
          // eslint-disable-next-line lingui/no-unlocalized-strings -- TransferStatus id
          .transfers.some((t) => t.status === "uploading"),
    });
    const off = onUploadSettled(invalidator.notify);
    return () => {
      off();
      invalidator.dispose();
    };
  }, [qc]);

  if (dismissed || transfers.length === 0) return null;

  let done = 0;
  let failed = 0;
  let activeCount = 0;
  let totalBytes = 0;
  let loadedBytes = 0;

  for (const t of transfers) {
    totalBytes += t.size;
    if (t.status === "done") {
      done++;
      loadedBytes += t.size;
    } else {
      loadedBytes += t.loaded;
      if (t.status === "error") {
        failed++;
      } else if (t.status === "uploading" || t.status === "queued") {
        activeCount++;
      }
    }
  }
  const finished = activeCount === 0;
  const pct = totalBytes > 0 ? Math.round((loadedBytes / totalBytes) * 100) : 0;
  const loadedSize = f.size(loadedBytes);
  const totalSize = f.size(totalBytes);

  return (
    <div className="border-border bg-popover ring-foreground/10 fixed right-4 bottom-4 z-50 w-90 overflow-hidden rounded-lg shadow-md ring-1">
      <div className="border-border flex h-9 items-center gap-2 border-b px-3">
        <div className="flex min-w-0 flex-1 flex-col">
          <span className="truncate text-xs font-medium">
            <TrayTitle transfers={transfers} done={done} failed={failed} />
          </span>
          <span className="text-muted-foreground truncate text-[10px] tabular-nums">
            {finished
              ? t`${loadedSize} transferred`
              : t`${loadedSize} of ${totalSize}` + ` · ${pct}%`}
          </span>
        </div>
        <Button
          variant="ghost"
          size="icon-xs"
          aria-label={collapsed ? t`Expand transfers` : t`Collapse transfers`}
          aria-expanded={!collapsed}
          onClick={() => setCollapsed(!collapsed)}
        >
          {collapsed ? <ChevronUp /> : <ChevronDown />}
        </Button>
        <Button variant="ghost" size="icon-xs" aria-label={t`Close transfers`} onClick={dismiss}>
          <X />
        </Button>
      </div>

      <div className="bg-muted h-0.5 w-full" role="progressbar" aria-valuenow={pct}>
        <div className="bg-primary h-full transition-[width]" style={{ width: `${pct}%` }} />
      </div>

      {!collapsed && (
        <>
          <ul className="max-h-72 overflow-y-auto">
            {transfers.map((t) => (
              <TransferRow
                key={t.id}
                transfer={t}
                paused={paused}
                onCancel={cancel}
                onRetry={retry}
              />
            ))}
          </ul>

          <div className="border-border flex h-8 items-center justify-between border-t px-2">
            {finished ? (
              <span className="text-muted-foreground px-1 text-[10px]">
                {failed > 0 ? (
                  <Plural value={failed} one="# failed" other="# failed" />
                ) : (
                  <Trans>All transfers complete</Trans>
                )}
              </span>
            ) : (
              <Button variant="ghost" size="xs" onClick={() => setPaused(!paused)}>
                {paused ? <Trans>Resume uploads</Trans> : <Trans>Pause uploads</Trans>}
              </Button>
            )}
            <Button variant="ghost" size="xs" disabled={done === 0} onClick={clearCompleted}>
              <Trans>Clear completed</Trans>
            </Button>
          </div>
        </>
      )}
    </div>
  );
}

function TrayTitle({
  transfers,
  done,
  failed,
}: {
  transfers: Transfer[];
  done: number;
  failed: number;
}) {
  const pending = transfers.filter((t) => t.status === "uploading" || t.status === "queued").length;
  const current = done + 1;
  const total = transfers.length;
  if (pending > 0) {
    return (
      <Trans>
        Uploading {current} of {total}
      </Trans>
    );
  }
  if (failed > 0) {
    return (
      <Trans>
        {done} uploaded, {failed} failed
      </Trans>
    );
  }
  return <Plural value={done} one="Uploaded # file" other="Uploaded # files" />;
}

interface RowProps {
  transfer: Transfer;
  paused: boolean;
  onCancel: (id: string) => void;
  onRetry: (id: string) => void;
}

function TransferRow({ transfer, paused, onCancel, onRetry }: RowProps) {
  const { t } = useLingui();
  const pct = transfer.size > 0 ? Math.round((transfer.loaded / transfer.size) * 100) : 0;
  const fileName = transfer.name;

  return (
    <li
      className={cn(
        "border-border relative flex h-10 items-center gap-2 border-b px-3 last:border-b-0",
        transfer.status === "error" && "bg-destructive/10",
      )}
    >
      <div className="flex min-w-0 flex-1 flex-col">
        <span
          className="truncate text-xs"
          title={transfer.path ? `${transfer.path}/${transfer.name}` : transfer.name}
        >
          {transfer.name}
        </span>
        <span
          className={cn(
            "truncate text-[10px] tabular-nums",
            transfer.status === "error" ? "text-destructive" : "text-muted-foreground",
          )}
        >
          <StatusLine transfer={transfer} paused={paused} />
        </span>
      </div>

      {transfer.status === "done" && <CheckCircle2 className="text-primary size-3.5 shrink-0" />}
      {transfer.status === "uploading" && (
        <span className="text-muted-foreground shrink-0 text-[10px] tabular-nums">{pct}%</span>
      )}
      {transfer.status === "error" && (
        <Button variant="ghost" size="xs" onClick={() => onRetry(transfer.id)}>
          <RotateCcw data-icon="inline-start" /> <Trans>Retry</Trans>
        </Button>
      )}
      {(transfer.status === "queued" || transfer.status === "uploading") && (
        <Button
          variant="ghost"
          size="icon-xs"
          aria-label={t`Cancel ${fileName}`}
          onClick={() => onCancel(transfer.id)}
        >
          <X />
        </Button>
      )}

      {transfer.status === "uploading" && (
        <div
          className="bg-primary absolute bottom-0 left-0 h-0.5 transition-[width]"
          style={{ width: `${pct}%` }}
        />
      )}
    </li>
  );
}

function StatusLine({ transfer, paused }: { transfer: Transfer; paused: boolean }) {
  const { t, i18n } = useLingui();
  const f = useFormatters();

  switch (transfer.status) {
    case "done":
      return <>{`${f.size(transfer.size)}${transfer.path ? ` · ${transfer.path}` : ""}`}</>;
    case "uploading": {
      const loadedSize = f.size(transfer.loaded);
      const totalSize = f.size(transfer.size);
      const progress = t`${loadedSize} of ${totalSize}`;
      return <>{progress + rate(transfer, f.size, i18n)}</>;
    }
    case "queued":
      return paused ? <Trans>Paused</Trans> : <Trans>Waiting</Trans>;
    case "cancelled":
      return <Trans>Cancelled</Trans>;
    case "error":
      return <>{transfer.error ?? t`Upload failed`}</>;
  }
}

/**
 * Average rate of the current attempt. Omitted until it has sent something.
 * Bytes the server already held when a resumed attempt started are not
 * counted: they were sent before, not in the time measured here.
 *
 * Takes the i18n instance for the same reason `move-dialog`'s `names` does:
 * the caller's render already holds one, and a plain helper cannot hold a
 * hook. The per-second suffix is a message, not punctuation — it is a word in
 * some languages.
 */
function rate(t: Transfer, size: (n: number) => string, i18n: I18n): string {
  const sent = t.loaded - (t.startedFrom ?? 0);
  if (!t.startedAt || sent <= 0) return "";
  const seconds = (Date.now() - t.startedAt) / 1000;
  if (seconds < 1) return "";
  const perSecond = size(Math.round(sent / seconds));
  return ` · ${i18n._(msg`${perSecond}/s`)}`;
}
