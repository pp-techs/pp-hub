import { useState } from "react";
import { createModal } from "@buiducnhat/better-modal";
import { ChevronRight, Folder, HardDrive, Loader2 } from "lucide-react";
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
import { useTree } from "@/lib/queries";
import { useKeyedState } from "@/lib/use-keyed-state";
import type { Node } from "@/lib/api";

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

    return (
      <Dialog open={modal.visible} onOpenChange={(open) => !open && modal.resolve(null)}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>
              {nodes.length === 1 ? `Move “${nodes[0].name}”` : `Move ${nodes.length} items`}
            </DialogTitle>
            <DialogDescription>Choose a destination folder in {spaceName}.</DialogDescription>
          </DialogHeader>

          <div className="border-border max-h-72 min-h-40 overflow-y-auto rounded-md border p-1">
            <button
              type="button"
              aria-pressed={target === undefined}
              className={destinationRow(target === undefined)}
              onClick={() => setTarget(undefined)}
            >
              <HardDrive className="text-muted-foreground size-3.5 shrink-0" />
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

          <DialogFooter>
            <Button variant="outline" onClick={() => modal.resolve(null)}>
              Cancel
            </Button>
            <Button disabled={unchanged} onClick={() => modal.resolve(target)}>
              {unchanged ? "Already here" : "Move"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    );
  },
);

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
        <Loader2 className="text-muted-foreground size-3 animate-spin" />
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
  const active = target === id;

  return (
    <>
      {/* Expand and select are separate controls, so they are separate buttons
          in a shared row rather than one nested inside the other. */}
      <div className={destinationRow(active)} style={{ paddingLeft: depth * 16 }}>
        <button
          type="button"
          aria-label={expanded ? "Collapse" : "Expand"}
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
