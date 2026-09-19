import { useMemo, useState } from "react";
import { toast } from "sonner";
import { Lock, RotateCcw, Trash2 } from "lucide-react";
import { Badge } from "@workspace/ui/components/badge";
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
import { cn } from "@workspace/ui/lib/utils";
import { fmtRelative, fmtSize, type TrashNode } from "@/lib/api";
import { useEmptyTrash, usePurgeNode, useRestoreNode, useSpaces, useTrash } from "@/lib/queries";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { NodeIcon } from "@/components/node-icon";

interface TrashPanelProps {
  spaceId: string;
}

/**
 * What a purge does *not* do, stated in both confirmations: the server refuses
 * to destroy subtrees the purger did not create and hands them back instead.
 */
const RESCUE_RULE =
  "Items other people created are not destroyed — they move to their owner’s top level.";

/**
 * Items soft-deleted in this space, with the two ways out: restore, which
 * lifts a node back into the tree, and delete forever, which releases its
 * blobs and cannot be undone.
 *
 * Only a space owner can act here — the server gates restore and purge at the
 * same rank it gates delete — so the buttons are hidden for everyone else
 * rather than shown and rejected.
 *
 * A row can also be read-only: somebody else's delete that the caller sees
 * only because their own files are buried inside it. It carries the count of
 * what is buried and no controls at all, because restore and purge there are
 * not theirs to run — the server refuses both.
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
  /**
   * The rows the caller may act on. A read-only tombstone is somebody else's
   * delete, so it is kept out of the selection — including select-all —
   * rather than offered and then refused by the server.
   */
  const actionable = rows.filter((n) => !n.read_only);
  const selected = actionable.filter((n) => selectedIds.has(n.id));
  const allChecked = actionable.length > 0 && selected.length === actionable.length;

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

  /**
   * Purging is not always destruction: the server keeps subtrees the purger
   * did not create and moves them to their owner's top level, so the outcome
   * is reported rather than assumed. Nodes are purged independently — one
   * rejection must not swallow the rest — and counted once all have settled.
   */
  const runPurge = async (nodes: TrashNode[]) => {
    setSelectedIds(new Set());

    let purged = 0;
    let rescued = 0;
    await Promise.all(
      nodes.map((node) =>
        purge
          .mutateAsync(node.id)
          .then((result) => {
            purged += 1;
            rescued += result.rescued;
          })
          .catch((e: Error) =>
            toast.error(`Could not delete “${node.name}”`, { description: e.message }),
          ),
      ),
    );

    if (purged === 0) return;
    const note = rescueNote(rescued);
    toast.success(
      `Permanently deleted ${plural(purged, "item")}`,
      note ? { description: note } : undefined,
    );
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
                      setSelectedIds(
                        checked === true ? new Set(actionable.map((n) => n.id)) : new Set(),
                      )
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
            {rows.map((n) => {
              // A read-only row is somebody else's delete. It is here to say
              // where the caller's own files went and that a purge is coming,
              // so it shows the count and nothing to press.
              const readOnly = n.read_only === true;
              const buried = n.buried_own ?? 0;
              return (
                <TableRow key={n.id} className={cn(readOnly && "text-muted-foreground")}>
                  {isAdmin && (
                    <TableCell>
                      <Checkbox
                        checked={!readOnly && selectedIds.has(n.id)}
                        disabled={readOnly}
                        aria-label={
                          readOnly ? `${n.name} was deleted by someone else` : `Select ${n.name}`
                        }
                        className="size-3.5"
                        onCheckedChange={(checked) => toggle(n.id, checked === true)}
                      />
                    </TableCell>
                  )}
                  <TableCell>
                    <div className="flex items-center gap-2">
                      <NodeIcon node={n} />
                      <span className={cn("truncate", readOnly ? "font-normal" : "font-medium")}>
                        {n.name}
                      </span>
                      {readOnly && (
                        <span
                          className="shrink-0"
                          title="Deleted by someone else — you see it because your items are inside"
                        >
                          <Lock className="text-muted-foreground size-3" />
                        </span>
                      )}
                      {buried > 0 && (
                        <Badge variant="secondary" className="shrink-0 font-normal">
                          {plural(buried, "item")} of yours inside
                        </Badge>
                      )}
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
                      {readOnly ? (
                        <div className="text-muted-foreground flex justify-end text-[10px]">
                          Not yours to restore
                        </div>
                      ) : (
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
                      )}
                    </TableCell>
                  )}
                </TableRow>
              );
            })}
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
            : `Permanently delete ${confirmPurge?.length ?? 0} items? This cannot be undone. ${RESCUE_RULE}`
        }
        confirmLabel="Delete forever"
        confirmVariant="destructive"
        onConfirm={() => void runPurge(confirmPurge ?? [])}
      />

      <ConfirmDialog
        open={confirmEmpty}
        onOpenChange={setConfirmEmpty}
        title="Empty trash"
        description={`Permanently delete all ${plural(trash.data?.length ?? 0, "item")} in the trash, including everything inside deleted folders. This cannot be undone. ${RESCUE_RULE}`}
        confirmLabel="Empty trash"
        confirmVariant="destructive"
        onConfirm={() =>
          empty.mutate(undefined, {
            onSuccess: ({ purged, rescued }) => {
              toast.success("Emptied the trash", {
                description: [`${plural(purged, "item")} permanently deleted.`, rescueNote(rescued)]
                  .filter(Boolean)
                  .join(" "),
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
  return `Permanently delete “${node.name}”${subtree}? This cannot be undone. ${RESCUE_RULE}`;
}

/**
 * What a finished purge has to add when it rescued something: those files
 * still exist, somewhere the caller cannot see, and a report of "deleted"
 * alone would be a lie.
 */
function rescueNote(rescued: number): string | undefined {
  return rescued > 0
    ? `${plural(rescued, "item")} created by other people moved to their owner’s top level instead.`
    : undefined;
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
