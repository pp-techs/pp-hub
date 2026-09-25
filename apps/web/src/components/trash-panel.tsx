import { useMemo, useState } from "react";
import { toast } from "sonner";
import { Plural, Trans, useLingui } from "@lingui/react/macro";
import { msg, plural } from "@lingui/core/macro";
import type { MessageDescriptor } from "@lingui/core";
import { Lock, RotateCcw, Trash2 } from "lucide-react";
import { Badge } from "@workspace/ui/components/badge";
import { Button } from "@workspace/ui/components/button";
import { Checkbox } from "@workspace/ui/components/checkbox";
import { Input } from "@workspace/ui/components/input";
import { Skeleton } from "@workspace/ui/components/skeleton";
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
import type { TrashNode } from "@/lib/api";
import { useEmptyTrash, usePurgeNode, useRestoreNode, useSpaces, useTrash } from "@/lib/queries";
import { useFormatters } from "@/lib/format";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { NodeIcon } from "@/components/node-icon";
import { SkeletonTableRows } from "@/components/loading";

interface TrashPanelProps {
  spaceId: string;
}

/**
 * What a purge does *not* do, stated in both confirmations: the server refuses
 * to destroy subtrees the purger did not create and hands them back instead.
 *
 * A descriptor rather than a string: module scope is evaluated at import time,
 * before a catalog is active, so rendered text here would freeze whichever
 * language loaded first. Each confirmation resolves it with `i18n._`.
 */
const RESCUE_RULE = msg`Items other people created are not destroyed — they move to their owner’s top level.`;

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
  const { i18n, t } = useLingui();
  const f = useFormatters();
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
  const totalSize = f.size(totalBytes);
  /**
   * The rows the caller may act on. A read-only tombstone is somebody else's
   * delete, so it is kept out of the selection — including select-all —
   * rather than offered and then refused by the server.
   */
  const actionable = rows.filter((n) => !n.read_only);
  const selected = actionable.filter((n) => selectedIds.has(n.id));
  const allChecked = actionable.length > 0 && selected.length === actionable.length;
  const selectedCount = selected.length;

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
  const runRestore = (node: TrashNode) => {
    const name = node.name;
    restore.mutate(node.id, {
      onSuccess: (result) => {
        const restored = result.node.name;
        // Each note is a whole sentence rather than a clause: a clause spliced
        // into a list reads as a fragment to a translator and cannot be
        // reordered around the rest of the sentence.
        const notes = [
          result.relocated
            ? t`Its original folder is also in the trash, so it went to the top level.`
            : null,
          result.renamed_from ? t`Renamed to “${restored}” — the old name was taken.` : null,
        ].filter(Boolean);
        toast.success(
          t`Restored “${restored}”`,
          notes.length > 0 ? { description: notes.join(" ") } : undefined,
        );
        setSelectedIds((prev) => {
          const next = new Set(prev);
          next.delete(node.id);
          return next;
        });
      },
      onError: (e) => toast.error(t`Could not restore “${name}”`, { description: e.message }),
    });
  };

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
      nodes.map((node) => {
        const name = node.name;
        return purge
          .mutateAsync(node.id)
          .then((result) => {
            purged += 1;
            rescued += result.rescued;
          })
          .catch((e: Error) =>
            toast.error(t`Could not delete “${name}”`, { description: e.message }),
          );
      }),
    );

    if (purged === 0) return;
    const note = rescueNote(rescued);
    toast.success(
      plural(purged, { one: "Permanently deleted # item", other: "Permanently deleted # items" }),
      note ? { description: i18n._(note) } : undefined,
    );
  };

  /**
   * Both purge confirmations end on the same caveat, which stays its own
   * message and is joined to the prompt here: two whole sentences, each
   * translatable alone, rather than one sentence assembled from pieces.
   */
  const purgeTargets = confirmPurge ?? [];
  const purgePrompt = [
    purgeTargets.length === 1
      ? i18n._(describePurge(purgeTargets[0]!))
      : plural(purgeTargets.length, {
          one: "Permanently delete # item? This cannot be undone.",
          other: "Permanently delete # items? This cannot be undone.",
        }),
    i18n._(RESCUE_RULE),
  ].join(" ");

  const emptyPrompt = [
    plural(trash.data?.length ?? 0, {
      one: "Permanently delete the # item in the trash, including everything inside deleted folders. This cannot be undone.",
      other:
        "Permanently delete all # items in the trash, including everything inside deleted folders. This cannot be undone.",
    }),
    i18n._(RESCUE_RULE),
  ].join(" ");

  return (
    <div className="flex flex-col gap-3">
      <div className="flex items-start justify-between gap-2">
        <div className="flex flex-col gap-0.5">
          <h2 className="font-heading text-sm font-medium">
            <Trans>Trash</Trans>
          </h2>
          <div className="text-muted-foreground text-xs tabular-nums">
            {trash.isPending ? (
              <Skeleton className="my-0.5 h-3 w-24" />
            ) : (
              <Trans>
                <Plural value={trash.data?.length ?? 0} one="# item" other="# items" /> ·{" "}
                {totalSize} · deleted items still count toward storage
              </Trans>
            )}
          </div>
        </div>

        {isAdmin && (trash.data?.length ?? 0) > 0 && (
          <div className="flex shrink-0 items-center gap-2">
            {selected.length > 0 && (
              <Button
                variant="outline"
                disabled={restore.isPending}
                onClick={() => selected.forEach(runRestore)}
              >
                <RotateCcw data-icon="inline-start" /> <Trans>Restore {selectedCount}</Trans>
              </Button>
            )}
            <Button
              variant="destructive"
              disabled={empty.isPending}
              onClick={() => setConfirmEmpty(true)}
            >
              <Trash2 data-icon="inline-start" /> <Trans>Empty trash</Trans>
            </Button>
          </div>
        )}
      </div>

      {(trash.data?.length ?? 0) > 0 && (
        <Input
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder={t`Filter deleted items`}
          aria-label={t`Filter deleted items`}
          className="max-w-xs"
        />
      )}

      {!trash.isPending && rows.length === 0 ? (
        <Empty className="py-12">
          <EmptyHeader>
            <EmptyMedia>
              <Trash2 className="text-muted-foreground size-10" />
            </EmptyMedia>
            <EmptyTitle>{query.trim() ? t`No deleted items match` : t`Trash is empty`}</EmptyTitle>
            <EmptyDescription>
              {query.trim()
                ? t`Try a different search.`
                : i18n._(retentionNote(trash.data?.[0]?.purges_at))}
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
                    aria-label={t`Select all deleted items`}
                    className="size-3.5"
                    onCheckedChange={(checked) =>
                      setSelectedIds(
                        checked === true ? new Set(actionable.map((n) => n.id)) : new Set(),
                      )
                    }
                  />
                </TableHead>
              )}
              <TableHead>
                <Trans>Name</Trans>
              </TableHead>
              <TableHead className="w-40">
                <Trans>Original location</Trans>
              </TableHead>
              <TableHead className="w-32">
                <Trans>Deleted by</Trans>
              </TableHead>
              <TableHead className="w-28">
                <Trans>Deleted</Trans>
              </TableHead>
              <TableHead className="w-28">
                <Trans>Purges in</Trans>
              </TableHead>
              <TableHead className="w-24">
                <Trans>Size</Trans>
              </TableHead>
              {isAdmin && <TableHead className="w-40" />}
            </TableRow>
          </TableHeader>
          <TableBody>
            {trash.isPending ? (
              <SkeletonTableRows columns={isAdmin ? 8 : 6} />
            ) : (
              rows.map((n) => {
                // A read-only row is somebody else's delete. It is here to say
                // where the caller's own files went and that a purge is coming,
                // so it shows the count and nothing to press.
                const readOnly = n.read_only === true;
                const buried = n.buried_own ?? 0;
                const nodeName = n.name;
                return (
                  <TableRow key={n.id} className={cn(readOnly && "text-muted-foreground")}>
                    {isAdmin && (
                      <TableCell>
                        <Checkbox
                          checked={!readOnly && selectedIds.has(n.id)}
                          disabled={readOnly}
                          aria-label={
                            readOnly
                              ? t`${nodeName} was deleted by someone else`
                              : t`Select ${nodeName}`
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
                            title={t`Deleted by someone else — you see it because your items are inside`}
                          >
                            <Lock className="text-muted-foreground size-3" />
                          </span>
                        )}
                        {buried > 0 && (
                          <Badge variant="secondary" className="shrink-0 font-normal">
                            <Plural
                              value={buried}
                              one="# item of yours inside"
                              other="# items of yours inside"
                            />
                          </Badge>
                        )}
                        {n.kind === "dir" && n.child_count > 0 && (
                          <span className="text-muted-foreground shrink-0 text-[10px] tabular-nums">
                            <Plural value={n.child_count} one="# item" other="# items" />
                          </span>
                        )}
                      </div>
                    </TableCell>
                    <TableCell className="text-muted-foreground truncate" title={n.location}>
                      {n.location || t`Top level`}
                    </TableCell>
                    <TableCell className="text-muted-foreground truncate">
                      {n.deleted_by || n.owner || "—"}
                    </TableCell>
                    <TableCell className="text-muted-foreground tabular-nums">
                      {f.relative(n.deleted_at)}
                    </TableCell>
                    <TableCell className="tabular-nums">
                      <PurgesIn at={n.purges_at} />
                    </TableCell>
                    <TableCell className="text-muted-foreground tabular-nums">
                      {n.kind === "file" && n.size != null ? f.size(n.size) : "—"}
                    </TableCell>
                    {isAdmin && (
                      <TableCell>
                        {readOnly ? (
                          <div className="text-muted-foreground flex justify-end text-[10px]">
                            <Trans>Not yours to restore</Trans>
                          </div>
                        ) : (
                          <div className="flex items-center justify-end gap-1">
                            <Button
                              variant="ghost"
                              size="xs"
                              disabled={restore.isPending}
                              onClick={() => runRestore(n)}
                            >
                              <Trans>Restore</Trans>
                            </Button>
                            <Button
                              variant="destructive"
                              size="xs"
                              disabled={purge.isPending}
                              onClick={() => setConfirmPurge([n])}
                            >
                              <Trans>Delete forever</Trans>
                            </Button>
                          </div>
                        )}
                      </TableCell>
                    )}
                  </TableRow>
                );
              })
            )}
          </TableBody>
        </Table>
      )}

      <ConfirmDialog
        open={!!confirmPurge}
        onOpenChange={(v) => !v && setConfirmPurge(null)}
        title={t`Delete forever`}
        description={purgePrompt}
        confirmLabel={t`Delete forever`}
        confirmVariant="destructive"
        onConfirm={() => void runPurge(confirmPurge ?? [])}
      />

      <ConfirmDialog
        open={confirmEmpty}
        onOpenChange={setConfirmEmpty}
        title={t`Empty trash`}
        description={emptyPrompt}
        confirmLabel={t`Empty trash`}
        confirmVariant="destructive"
        onConfirm={() =>
          empty.mutate(undefined, {
            onSuccess: ({ purged, rescued }) => {
              const note = rescueNote(rescued);
              toast.success(t`Emptied the trash`, {
                description: [
                  plural(purged, {
                    one: "# item permanently deleted.",
                    other: "# items permanently deleted.",
                  }),
                  note ? i18n._(note) : null,
                ]
                  .filter(Boolean)
                  .join(" "),
              });
              setSelectedIds(new Set());
            },
            onError: (e) => toast.error(t`Could not empty the trash`, { description: e.message }),
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
  if (!at) {
    return (
      <span className="text-muted-foreground">
        <Trans>Kept</Trans>
      </span>
    );
  }

  const days = Math.ceil((new Date(at).getTime() - Date.now()) / 86_400_000);
  if (days <= 0) {
    return (
      <span className="text-destructive">
        <Trans>Due</Trans>
      </span>
    );
  }

  return (
    <span className={days <= 3 ? "text-destructive" : "text-muted-foreground"}>
      <Plural value={days} one="# day" other="# days" />
    </span>
  );
}

function describePurge(node: TrashNode): MessageDescriptor {
  const name = node.name;
  return node.kind === "dir" && node.child_count > 0
    ? msg`Permanently delete “${name}” and the ${plural(node.child_count, {
        one: "# item",
        other: "# items",
      })} inside it? This cannot be undone.`
    : msg`Permanently delete “${name}”? This cannot be undone.`;
}

/**
 * What a finished purge has to add when it rescued something: those files
 * still exist, somewhere the caller cannot see, and a report of "deleted"
 * alone would be a lie.
 */
function rescueNote(rescued: number): MessageDescriptor | undefined {
  return rescued > 0
    ? msg`${plural(rescued, {
        one: "# item created by other people moved to their owner’s top level instead.",
        other: "# items created by other people moved to their owner’s top level instead.",
      })}`
    : undefined;
}

/** Explains the bin's lifetime, which depends on the space's retention. */
function retentionNote(anyPurgesAt?: string): MessageDescriptor {
  return anyPurgesAt
    ? msg`Items here are removed automatically once their retention window passes.`
    : msg`Items you move to trash stay here until you restore or delete them.`;
}
