import { useEffect } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { CheckCircle2, ChevronDown, ChevronUp, RotateCcw, X } from "lucide-react";
import { Button } from "@workspace/ui/components/button";
import { cn } from "@workspace/ui/lib/utils";
import { fmtSize } from "@/lib/api";
import { keys } from "@/lib/queries";
import { onUploadSettled, useUploadStore, type Transfer } from "@/lib/upload-store";

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
  const transfers = useUploadStore((s) => s.transfers);
  const paused = useUploadStore((s) => s.paused);
  const collapsed = useUploadStore((s) => s.collapsed);
  const dismissed = useUploadStore((s) => s.dismissed);
  const { cancel, retry, clearCompleted, setPaused, setCollapsed, dismiss } =
    useUploadStore.getState();

  // Each finished file changes the listing it landed in, and the space's used
  // bytes. The store cannot reach the query client itself, so the refetch is
  // driven from here.
  useEffect(
    () =>
      onUploadSettled((spaceId, parentId) => {
        void qc.invalidateQueries({ queryKey: keys.children(spaceId, parentId) });
        void qc.invalidateQueries({ queryKey: ["tree", spaceId] });
        void qc.invalidateQueries({ queryKey: keys.spaces });
      }),
    [qc],
  );

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

  return (
    <div className="border-border bg-popover ring-foreground/10 fixed right-4 bottom-4 z-50 w-90 overflow-hidden rounded-lg shadow-md ring-1">
      <div className="border-border flex h-9 items-center gap-2 border-b px-3">
        <div className="flex min-w-0 flex-1 flex-col">
          <span className="truncate text-xs font-medium">{title(transfers, done, failed)}</span>
          <span className="text-muted-foreground truncate text-[10px] tabular-nums">
            {finished
              ? `${fmtSize(loadedBytes)} transferred`
              : `${fmtSize(loadedBytes)} of ${fmtSize(totalBytes)} · ${pct}%`}
          </span>
        </div>
        <Button
          variant="ghost"
          size="icon-xs"
          aria-label={collapsed ? "Expand transfers" : "Collapse transfers"}
          aria-expanded={!collapsed}
          onClick={() => setCollapsed(!collapsed)}
        >
          {collapsed ? <ChevronUp /> : <ChevronDown />}
        </Button>
        <Button variant="ghost" size="icon-xs" aria-label="Close transfers" onClick={dismiss}>
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
              <TransferRow key={t.id} transfer={t} onCancel={cancel} onRetry={retry} />
            ))}
          </ul>

          <div className="border-border flex h-8 items-center justify-between border-t px-2">
            {finished ? (
              <span className="text-muted-foreground px-1 text-[10px]">
                {failed > 0 ? `${failed} failed` : "All transfers complete"}
              </span>
            ) : (
              <Button variant="ghost" size="xs" onClick={() => setPaused(!paused)}>
                {paused ? "Resume queue" : "Pause queue"}
              </Button>
            )}
            <Button variant="ghost" size="xs" disabled={done === 0} onClick={clearCompleted}>
              Clear completed
            </Button>
          </div>
        </>
      )}
    </div>
  );
}

function title(transfers: Transfer[], done: number, failed: number): string {
  const pending = transfers.filter((t) => t.status === "uploading" || t.status === "queued").length;
  if (pending > 0) return `Uploading ${done + 1} of ${transfers.length}`;
  if (failed > 0) return `${done} uploaded, ${failed} failed`;
  return `Uploaded ${done} ${done === 1 ? "file" : "files"}`;
}

interface RowProps {
  transfer: Transfer;
  onCancel: (id: string) => void;
  onRetry: (id: string) => void;
}

function TransferRow({ transfer: t, onCancel, onRetry }: RowProps) {
  const pct = t.size > 0 ? Math.round((t.loaded / t.size) * 100) : 0;

  return (
    <li
      className={cn(
        "border-border relative flex h-10 items-center gap-2 border-b px-3 last:border-b-0",
        t.status === "error" && "bg-destructive/10",
      )}
    >
      <div className="flex min-w-0 flex-1 flex-col">
        <span className="truncate text-xs" title={t.path ? `${t.path}/${t.name}` : t.name}>
          {t.name}
        </span>
        <span
          className={cn(
            "truncate text-[10px] tabular-nums",
            t.status === "error" ? "text-destructive" : "text-muted-foreground",
          )}
        >
          {statusLine(t, pct)}
        </span>
      </div>

      {t.status === "done" && <CheckCircle2 className="text-primary size-3.5 shrink-0" />}
      {t.status === "uploading" && (
        <span className="text-muted-foreground shrink-0 text-[10px] tabular-nums">{pct}%</span>
      )}
      {t.status === "error" && (
        <Button variant="ghost" size="xs" onClick={() => onRetry(t.id)}>
          <RotateCcw data-icon="inline-start" /> Retry
        </Button>
      )}
      {(t.status === "queued" || t.status === "uploading") && (
        <Button
          variant="ghost"
          size="icon-xs"
          aria-label={`Cancel ${t.name}`}
          onClick={() => onCancel(t.id)}
        >
          <X />
        </Button>
      )}

      {t.status === "uploading" && (
        <div
          className="bg-primary absolute bottom-0 left-0 h-0.5 transition-[width]"
          style={{ width: `${pct}%` }}
        />
      )}
    </li>
  );
}

function statusLine(t: Transfer, pct: number): string {
  switch (t.status) {
    case "done":
      return `${fmtSize(t.size)}${t.path ? ` · ${t.path}` : ""}`;
    case "uploading":
      return `${fmtSize(t.loaded)} of ${fmtSize(t.size)}${rate(t, pct)}`;
    case "queued":
      return "Waiting";
    case "cancelled":
      return "Cancelled";
    case "error":
      return t.error ?? "Upload failed";
  }
}

/** Average rate since the transfer started. Omitted until there is a sample. */
function rate(t: Transfer, pct: number): string {
  if (!t.startedAt || pct === 0) return "";
  const seconds = (Date.now() - t.startedAt) / 1000;
  if (seconds < 1) return "";
  return ` · ${fmtSize(Math.round(t.loaded / seconds))}/s`;
}
