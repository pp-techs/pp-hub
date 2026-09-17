import { useMemo, useState } from "react";
import { toast } from "sonner";
import { RotateCcw, Trash2 } from "lucide-react";
import { Button } from "@workspace/ui/components/button";
import { Checkbox } from "@workspace/ui/components/checkbox";
import { Input } from "@workspace/ui/components/input";
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
import { fmtRelative, fmtSize, type TrashNode } from "@/lib/api";
import { useEmptyTrash, usePurgeNode, useRestoreNode, useSpaces, useTrash } from "@/lib/queries";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { NodeIcon } from "@/components/node-icon";

interface TrashPanelProps {
  spaceId: string;
}

/**
 * Items soft-deleted in this space, with the two ways out: restore, which
 * lifts a node back into the tree, and delete forever, which releases its
 * blobs and cannot be undone.
 *
 * Only a space owner can act here — the server gates restore and purge at the
 * same rank it gates delete — so the buttons are hidden for everyone else
 * rather than shown and rejected.
 */
export function TrashPanel({ spaceId }: TrashPanelProps) {
  const trash = useTrash(spaceId);
  const spaces = useSpaces();
  const isAdmin = spaces.data?.find((s) => s.id === spaceId)?.is_admin;

  const restore = useRestoreNode(spaceId);
  const purge = usePurgeNode(spaceId);
  const empty = useEmptyTrash(spaceId);

  const [query, setQuery] = useState("");
  const [selectedIds, setSelectedIds] = useState<Set<string>>(new Set());
  const [confirmPurge, setConfirmPurge] = useState<TrashNode[] | null>(null);
  const [confirmEmpty, setConfirmEmpty] = useState(false);

  const rows = useMemo(() => {
    const q = query.trim().toLowerCase();
    const all = trash.data ?? [];
    return q ? all.filter((n) => n.name.toLowerCase().includes(q)) : all;
  }, [trash.data, query]);

  const totalBytes = (trash.data ?? []).reduce((sum, n) => sum + (n.size ?? 0), 0);
  const selected = rows.filter((n) => selectedIds.has(n.id));
  const allChecked = rows.length > 0 && selected.length === rows.length;

  const toggle = (id: string, checked: boolean) =>
    setSelectedIds((prev) => {
      const next = new Set(prev);
      if (checked) next.add(id);
      else next.delete(id);
      return next;
    });

  /**
   * Restores one node and reports what the server had to adjust. A restore
   * that silently lands somewhere other than where it came from is the kind
   * of thing people only notice a week later.
   */
  const runRestore = (node: TrashNode) =>
    restore.mutate(node.id, {
      onSuccess: (result) => {
        const notes = [
          result.relocated &&
            "its original folder is also in the trash, so it went to the top level",
          result.renamed_from && `renamed to “${result.node.name}” — the old name was taken`,
        ].filter(Boolean);
        toast.success(
          `Restored “${result.node.name}”`,
          notes.length > 0 ? { description: capitalize(notes.join("; ")) } : undefined,
        );
        setSelectedIds((prev) => {
          const next = new Set(prev);
          next.delete(node.id);
          return next;
        });
      },
      onError: (e) => toast.error(`Could not restore “${node.name}”`, { description: e.message }),
    });

  const runPurge = (nodes: TrashNode[]) => {
    for (const node of nodes) {
      purge.mutate(node.id, {
        onError: (e) => toast.error(`Could not delete “${node.name}”`, { description: e.message }),
      });
    }
    setSelectedIds(new Set());
  };

  return (
    <div className="flex flex-col gap-3">
      <div className="flex items-start justify-between gap-2">
        <div className="flex flex-col gap-0.5">
          <h2 className="font-heading text-sm font-medium">Trash</h2>
          <p className="text-muted-foreground text-xs tabular-nums">
            {trash.isPending
              ? "Loading…"
              : `${plural(trash.data?.length ?? 0, "item")} · ${fmtSize(totalBytes)} · deleted items still count toward storage`}
          </p>
        </div>

        {isAdmin && (trash.data?.length ?? 0) > 0 && (
          <div className="flex shrink-0 items-center gap-2">
            {selected.length > 0 && (
              <Button
                variant="outline"
                disabled={restore.isPending}
                onClick={() => selected.forEach(runRestore)}
              >
                <RotateCcw data-icon="inline-start" /> Restore {selected.length}
              </Button>
            )}
            <Button
              variant="destructive"
              disabled={empty.isPending}
              onClick={() => setConfirmEmpty(true)}
            >
              <Trash2 data-icon="inline-start" /> Empty trash
            </Button>
          </div>
        )}
      </div>

      {(trash.data?.length ?? 0) > 0 && (
        <Input
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder="Filter deleted items"
          aria-label="Filter deleted items"
          className="max-w-xs"
        />
      )}

      {trash.isPending ? (
        <p className="text-muted-foreground py-8 text-xs">Loading…</p>
      ) : rows.length === 0 ? (
        <Empty className="py-12">
          <EmptyHeader>
            <EmptyMedia>
              <Trash2 className="text-muted-foreground size-10" />
            </EmptyMedia>
            <EmptyTitle>{query.trim() ? "No deleted items match" : "Trash is empty"}</EmptyTitle>
            <EmptyDescription>
              {query.trim() ? "Try a different search." : retentionNote(trash.data?.[0]?.purges_at)}
            </EmptyDescription>
          </EmptyHeader>
        </Empty>
      ) : (
        <Table className="text-xs">
          <TableHeader>
            <TableRow>
              {isAdmin && (
                <TableHead className="w-8">
                  <Checkbox
                    checked={allChecked}
                    indeterminate={!allChecked && selected.length > 0}
                    aria-label="Select all deleted items"
                    className="size-3.5"
                    onCheckedChange={(checked) =>
                      setSelectedIds(checked === true ? new Set(rows.map((n) => n.id)) : new Set())
                    }
                  />
                </TableHead>
              )}
              <TableHead>Name</TableHead>
              <TableHead className="w-40">Original location</TableHead>
              <TableHead className="w-32">Deleted by</TableHead>
              <TableHead className="w-28">Deleted</TableHead>
              <TableHead className="w-28">Purges in</TableHead>
              <TableHead className="w-24">Size</TableHead>
              {isAdmin && <TableHead className="w-40" />}
            </TableRow>
          </TableHeader>
          <TableBody>
            {rows.map((n) => (
              <TableRow key={n.id}>
                {isAdmin && (
                  <TableCell>
                    <Checkbox
                      checked={selectedIds.has(n.id)}
                      aria-label={`Select ${n.name}`}
                      className="size-3.5"
                      onCheckedChange={(checked) => toggle(n.id, checked === true)}
                    />
                  </TableCell>
                )}
                <TableCell>
                  <div className="flex items-center gap-2">
                    <NodeIcon node={n} />
                    <span className="truncate font-medium">{n.name}</span>
                    {n.kind === "dir" && n.child_count > 0 && (
                      <span className="text-muted-foreground shrink-0 text-[10px] tabular-nums">
                        {plural(n.child_count, "item")}
                      </span>
                    )}
                  </div>
                </TableCell>
                <TableCell className="text-muted-foreground truncate" title={n.location}>
                  {n.location || "Top level"}
                </TableCell>
                <TableCell className="text-muted-foreground truncate">
                  {n.deleted_by || n.owner || "—"}
                </TableCell>
                <TableCell className="text-muted-foreground tabular-nums">
                  {fmtRelative(n.deleted_at)}
                </TableCell>
                <TableCell className="tabular-nums">
                  <PurgesIn at={n.purges_at} />
                </TableCell>
                <TableCell className="text-muted-foreground tabular-nums">
                  {n.kind === "file" && n.size != null ? fmtSize(n.size) : "—"}
                </TableCell>
                {isAdmin && (
                  <TableCell>
                    <div className="flex items-center justify-end gap-1">
                      <Button
                        variant="ghost"
                        size="xs"
                        disabled={restore.isPending}
                        onClick={() => runRestore(n)}
                      >
                        Restore
                      </Button>
                      <Button
                        variant="destructive"
                        size="xs"
                        disabled={purge.isPending}
                        onClick={() => setConfirmPurge([n])}
                      >
                        Delete forever
                      </Button>
                    </div>
                  </TableCell>
                )}
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}

      <ConfirmDialog
        open={!!confirmPurge}
        onOpenChange={(v) => !v && setConfirmPurge(null)}
        title="Delete forever"
        description={
          confirmPurge?.length === 1
            ? describePurge(confirmPurge[0])
            : `Permanently delete ${confirmPurge?.length ?? 0} items? This cannot be undone.`
        }
        confirmLabel="Delete forever"
        confirmVariant="destructive"
        onConfirm={() => runPurge(confirmPurge ?? [])}
      />

      <ConfirmDialog
        open={confirmEmpty}
        onOpenChange={setConfirmEmpty}
        title="Empty trash"
        description={`Permanently delete all ${plural(trash.data?.length ?? 0, "item")} in the trash, including everything inside deleted folders. This cannot be undone.`}
        confirmLabel="Empty trash"
        confirmVariant="destructive"
        onConfirm={() =>
          empty.mutate(undefined, {
            onSuccess: (purged) => {
              toast.success(`Emptied the trash`, {
                description: `${plural(purged, "item")} permanently deleted.`,
              });
              setSelectedIds(new Set());
            },
            onError: (e) => toast.error("Could not empty the trash", { description: e.message }),
          })
        }
      />
    </div>
  );
}

/**
 * Countdown to automatic purging, reddening inside three days.
 *
 * Most spaces keep their trash indefinitely — retention defaults to 0 — so the
 * common case is an em dash rather than a number, and the label says why
 * instead of implying a deadline that does not exist.
 */
function PurgesIn({ at }: { at?: string }) {
  if (!at) return <span className="text-muted-foreground">Kept</span>;

  const days = Math.ceil((new Date(at).getTime() - Date.now()) / 86_400_000);
  if (days <= 0) return <span className="text-destructive">Due</span>;

  return (
    <span className={days <= 3 ? "text-destructive" : "text-muted-foreground"}>
      {days} {days === 1 ? "day" : "days"}
    </span>
  );
}

function describePurge(node: TrashNode): string {
  const subtree =
    node.kind === "dir" && node.child_count > 0
      ? ` and the ${plural(node.child_count, "item")} inside it`
      : "";
  return `Permanently delete “${node.name}”${subtree}? This cannot be undone.`;
}

/** Explains the bin's lifetime, which depends on the space's retention. */
function retentionNote(anyPurgesAt?: string): string {
  return anyPurgesAt
    ? "Items here are removed automatically once their retention window passes."
    : "Items you move to trash stay here until you restore or delete them.";
}

function plural(n: number, noun: string): string {
  return `${n} ${noun}${n === 1 ? "" : "s"}`;
}

function capitalize(s: string): string {
  return s.charAt(0).toUpperCase() + s.slice(1);
}
