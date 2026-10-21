import { useState } from "react";
import { createModal } from "@buiducnhat/better-modal";
import { ChevronRight, Folder, HardDrive, Loader2, TriangleAlert, Users } from "lucide-react";
import { Plural, Trans, useLingui } from "@lingui/react/macro";
import { msg, plural } from "@lingui/core/macro";
import type { I18n } from "@lingui/core";
import { Button } from "@workspace/ui/components/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@workspace/ui/components/dialog";
import { cn } from "@workspace/ui/lib/utils";
import { useMoveImpact, useTree } from "@/lib/queries";
import { useKeyedState } from "@/lib/use-keyed-state";
import { ApiError, type MoveImpact, type Node, type Principal } from "@/lib/api";

/** Undefined destination means the top level of the space. */
type Destination = string | undefined;

type Props = {
  spaceId: string;
  spaceName: string;
  /** The nodes being moved — they and their descendants are not valid targets. */
  nodes: Node[];
};

/**
 * Folder picker for "Move to…".
 *
 * The tree endpoint only ever returns one level at a time, so this walks down
 * lazily the same way the rail does. A folder being moved is excluded as a
 * destination, and so is the folder the selection already sits in — both would
 * be no-ops, and moving a folder into itself is rejected by the server anyway.
 *
 * Picking a destination also asks the server who gains and who loses access,
 * because access is inherited: moving a file out of a colleague's folder takes
 * it away from them, and moving it into a shared folder hands it over. That
 * alone never blocks the move — it is the supported way to narrow sharing, the
 * same advice Drive gives — but it must not be silent.
 *
 * Some moves are not the caller's to make at all: someone else's item into a
 * team folder, or a re-parenting that would widen the mover's own reach or
 * change who can open it without their holding share on it. The server
 * refuses those, the impact question included, with a sentence saying which —
 * so that sentence replaces the delta and Move stays unavailable, rather than
 * failing after the click.
 */
export const MoveDialog = createModal<Props, Destination | null>(
  "move-node",
  ({ spaceId, spaceName, nodes, modal }) => {
    const movingIds = new Set(nodes.map((n) => n.id));
    // Keyed on the selection, so reopening for different nodes clears the
    // destination instead of keeping the one chosen last time — which could
    // otherwise move files somewhere the user never selected.
    const [target, setTarget] = useKeyedState<string, Destination>(
      nodes.map((n) => n.id).join(","),
      undefined,
    );

    // Every node in a selection shares a parent, so one is enough to compare.
    const currentParent = nodes[0]?.parent_id;
    const unchanged = target === currentParent;
    // Pulled out of the JSX so the placeholders read as `{name}` and
    // `{count}` for a translator rather than a bare index.
    const name = nodes[0]?.name;
    const count = nodes.length;
    const impact = useMoveImpact(
      nodes.map((n) => n.id),
      target,
      modal.visible && !unchanged,
    );
    // One refused node refuses the selection: "Move 5 items" must not quietly
    // mean "move the 3 you may".
    const refused =
      impact.error instanceof ApiError && impact.error.status === 403 ? impact.error.message : null;
    return (
      <Dialog open={modal.visible} onOpenChange={(open) => !open && modal.resolve(null)}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>
              {count === 1 ? (
                <Trans>Move “{name}”</Trans>
              ) : (
                <Plural value={count} other="Move # items" />
              )}
            </DialogTitle>
            <DialogDescription>
              <Trans>Choose a destination folder in {spaceName}.</Trans>
            </DialogDescription>
          </DialogHeader>

          <div className="border-border max-h-72 min-h-40 overflow-y-auto rounded-md border p-1">
            <button
              type="button"
              aria-pressed={target === undefined}
              className={destinationRow(target === undefined)}
              onClick={() => setTarget(undefined)}
            >
              <HardDrive
                className={cn("size-3.5 shrink-0", target !== undefined && "text-muted-foreground")}
              />
              <span className="truncate font-medium">{spaceName}</span>
            </button>
            <DestinationLevel
              spaceId={spaceId}
              parentId={undefined}
              depth={1}
              target={target}
              movingIds={movingIds}
              onSelect={setTarget}
            />
          </div>

          {refused ? (
            <p className="text-destructive flex items-start gap-1.5 text-xs">
              <TriangleAlert className="mt-0.5 size-3.5 shrink-0" />
              <span>{refused}</span>
            </p>
          ) : (
            <AccessChange impact={impact.data} />
          )}

          <DialogFooter>
            <Button variant="outline" onClick={() => modal.resolve(null)}>
              <Trans>Cancel</Trans>
            </Button>
            <Button disabled={unchanged || refused !== null} onClick={() => modal.resolve(target)}>
              {unchanged ? <Trans>Already here</Trans> : <Trans>Move</Trans>}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    );
  },
);

/**
 * The access delta of the pending move, in the two directions that matter.
 * Nothing renders while the query is idle or the answer is empty, so a move
 * inside your own tree stays a two-click operation.
 */
function AccessChange({ impact }: { impact: MoveImpact | undefined }) {
  const { i18n } = useLingui();
  if (!impact || (impact.losing.length === 0 && impact.gaining.length === 0)) return null;
  // Named locals so each placeholder reaches a translator as `{gaining}` /
  // `{losing}` rather than a bare index.
  const gaining = names(impact.gaining, i18n);
  const losing = names(impact.losing, i18n);
  return (
    <div className="space-y-1 text-xs">
      {impact.gaining.length > 0 && (
        <p className="text-muted-foreground flex items-start gap-1.5">
          <Users className="mt-0.5 size-3.5 shrink-0" />
          <span>
            <Trans>Gains access: {gaining}.</Trans>
          </span>
        </p>
      )}
      {impact.losing.length > 0 && (
        <p className="text-destructive flex items-start gap-1.5">
          <TriangleAlert className="mt-0.5 size-3.5 shrink-0" />
          <span>
            <Trans>Loses access: {losing}.</Trans>
          </span>
        </p>
      )}
    </div>
  );
}

/**
 * Three names, then a count: a folder shared with a big group is one line.
 *
 * Takes the i18n instance rather than reaching for a hook: it is called from
 * the render of `AccessChange`, which already holds one, and a plain helper
 * cannot hold a hook of its own.
 */
function names(ps: Principal[], i18n: I18n): string {
  const shown = ps
    .slice(0, 3)
    .map((p) => (p.kind === "link" ? i18n._(msg`the public link`) : p.name));
  const rest = ps.length - shown.length;
  const listed = shown.join(", ");
  return rest > 0
    ? i18n._(msg`${listed} and ${plural(rest, { one: "# more", other: "# more" })}`)
    : listed;
}

function destinationRow(active: boolean) {
  return cn(
    "flex h-7 w-full items-center gap-2 rounded-md px-2 text-left text-xs transition-colors",
    active ? "bg-primary text-primary-foreground" : "hover:bg-muted text-foreground",
  );
}

interface LevelProps {
  spaceId: string;
  parentId: string | undefined;
  depth: number;
  target: Destination;
  movingIds: Set<string>;
  onSelect: (id: Destination) => void;
}

function DestinationLevel({ spaceId, parentId, depth, target, movingIds, onSelect }: LevelProps) {
  const { data, isLoading } = useTree(spaceId, parentId);
  const dirs = (data ?? []).filter((n) => n.kind === "dir" && !movingIds.has(n.id));

  if (isLoading) {
    return (
      <div className="flex h-7 items-center gap-2 px-2" style={{ paddingLeft: depth * 16 }}>
        <div className="size-3 shrink-0 animate-spin">
          <Loader2 className="text-muted-foreground size-full" />
        </div>
      </div>
    );
  }

  return (
    <>
      {dirs.map((dir) => (
        <DestinationItem
          key={dir.id}
          spaceId={spaceId}
          id={dir.id}
          name={dir.name}
          hasChildren={dir.has_children}
          depth={depth}
          target={target}
          movingIds={movingIds}
          onSelect={onSelect}
        />
      ))}
    </>
  );
}

interface ItemProps extends Omit<LevelProps, "parentId"> {
  id: string;
  name: string;
  hasChildren: boolean;
}

function DestinationItem({
  spaceId,
  id,
  name,
  hasChildren,
  depth,
  target,
  movingIds,
  onSelect,
}: ItemProps) {
  const [expanded, setExpanded] = useState(false);
  const { t } = useLingui();
  const active = target === id;

  return (
    <>
      {/* Expand and select are separate controls, so they are separate buttons
          in a shared row rather than one nested inside the other. */}
      <div className={destinationRow(active)} style={{ paddingLeft: depth * 16 }}>
        <button
          type="button"
          aria-label={expanded ? t`Collapse` : t`Expand`}
          aria-expanded={expanded}
          className="shrink-0"
          onClick={() => setExpanded((v) => !v)}
        >
          <ChevronRight
            className={cn(
              "size-3.5 transition-transform",
              expanded && "rotate-90",
              !hasChildren && "invisible",
            )}
          />
        </button>
        <button
          type="button"
          aria-pressed={active}
          className="flex min-w-0 flex-1 items-center gap-2 text-left"
          onClick={() => onSelect(id)}
        >
          <Folder className="size-3.5 shrink-0" />
          <span className="truncate">{name}</span>
        </button>
      </div>
      {expanded && (
        <DestinationLevel
          spaceId={spaceId}
          parentId={id}
          depth={depth + 1}
          target={target}
          movingIds={movingIds}
          onSelect={onSelect}
        />
      )}
    </>
  );
}
