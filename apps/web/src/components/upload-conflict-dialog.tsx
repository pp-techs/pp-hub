import { useCallback } from "react";
import { createModal } from "@buiducnhat/better-modal";
import { useQueryClient } from "@tanstack/react-query";
import { FileStack } from "lucide-react";
import { Button } from "@workspace/ui/components/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@workspace/ui/components/dialog";
import type { Node } from "@/lib/api";
import { keys } from "@/lib/queries";
import { useUploadStore, type UploadItem } from "@/lib/upload-store";
import { useKeyedState } from "@/lib/use-keyed-state";

/** A file about to be uploaded whose name is already taken in the destination. */
export interface UploadConflict {
  name: string;
  existingId: string;
}

/** What the user chose for the whole batch; null is "upload nothing". */
export type ConflictChoice = "keep-both" | "replace" | null;

/** Names listed in full before the list collapses into "and N more". */
const VISIBLE = 5;

/**
 * The name the server will land a colliding upload under: the counter goes
 * before the extension (`report (2).txt`), matching `suffixName` in
 * `internal/fs`. Quoting a name the listing will not show is worse than
 * quoting none.
 */
function suffixed(name: string): string {
  const dot = name.lastIndexOf(".");
  return dot > 0 ? `${name.slice(0, dot)} (2)${name.slice(dot)}` : `${name} (2)`;
}

/**
 * Asks what to do when an upload would collide with files already in the
 * destination.
 *
 * The server used to overwrite a same-named file silently; it now lands the
 * upload as "name (2)" instead, which is safe but just as surprising the
 * other way round — re-uploading a corrected spreadsheet left the stale one
 * in place and the shares pointing at it. So the choice is the user's, made
 * once for the whole batch rather than once per file: a folder drop can
 * collide forty times, and forty modals is not a decision, it is an obstacle.
 */
export const UploadConflictDialog = createModal<{ conflicts: UploadConflict[] }, ConflictChoice>(
  "upload-conflict",
  ({ conflicts, modal }) => {
    const one = conflicts.length === 1;
    // A closed modal stays mounted, so a list expanded for one batch would
    // still be expanded the next time a different batch collides.
    const [expanded, setExpanded] = useKeyedState(
      conflicts.map((c) => c.existingId).join(","),
      false,
    );

    const hidden = conflicts.length - VISIBLE;
    const shown = expanded ? conflicts : conflicts.slice(0, VISIBLE);

    return (
      <Dialog open={modal.visible} onOpenChange={(open) => !open && modal.resolve(null)}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>{one ? "That name is taken" : "Those names are taken"}</DialogTitle>
            <DialogDescription>
              {one
                ? "A file with this name is already in this folder."
                : `${conflicts.length} of these files are already in this folder.`}{" "}
              <strong className="font-medium">Keep both</strong> uploads alongside, named “
              {one ? suffixed(conflicts[0].name) : "name (2)"}”.{" "}
              <strong className="font-medium">Replace</strong> adds a new version to the file that
              is already there, keeping its owner, its link and its history.
            </DialogDescription>
          </DialogHeader>

          <ul className="border-border max-h-48 overflow-y-auto rounded-md border p-1">
            {shown.map((conflict) => (
              <li
                key={conflict.existingId}
                className="flex h-7 items-center gap-2 rounded-md px-2 text-xs"
              >
                <FileStack className="text-muted-foreground size-3.5 shrink-0" />
                <span className="truncate">{conflict.name}</span>
              </li>
            ))}
            {hidden > 0 && !expanded && (
              <li>
                <button
                  type="button"
                  className="text-muted-foreground hover:text-foreground h-7 px-2 text-xs underline-offset-4 hover:underline"
                  onClick={() => setExpanded(true)}
                >
                  and {hidden} more
                </button>
              </li>
            )}
          </ul>

          <DialogFooter>
            <Button variant="outline" onClick={() => modal.resolve(null)}>
              Cancel
            </Button>
            <Button variant="secondary" onClick={() => modal.resolve("replace")}>
              Replace
            </Button>
            <Button onClick={() => modal.resolve("keep-both")}>Keep both</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    );
  },
);

/**
 * The names in `items` that a file already in `existing` would collide with.
 *
 * Only top-level items can collide: everything with a folder path lands in a
 * directory the upload itself creates or reuses, and the server settles those
 * names. Folders are not candidates either — a file never replaces a folder.
 */
export function findUploadConflicts(
  items: UploadItem[],
  existing: Node[] | undefined,
): UploadConflict[] {
  if (!existing?.length) return [];
  const byName = new Map(existing.filter((n) => n.kind === "file").map((n) => [n.name, n.id]));
  if (byName.size === 0) return [];

  const conflicts: UploadConflict[] = [];
  const seen = new Set<string>();
  for (const { file, path } of items) {
    if (path) continue;
    const existingId = byName.get(file.name);
    if (!existingId || seen.has(file.name)) continue;
    seen.add(file.name);
    conflicts.push({ name: file.name, existingId });
  }
  return conflicts;
}

export type UploadOutcome = "uploaded" | "cancelled";

/**
 * Enqueues an upload, asking first when it would collide with what is already
 * in the destination.
 *
 * Every surface that uploads goes through this — the pickers behind the New
 * menu and the drop zone over the listing — because the collision is a
 * property of the destination, not of how the files got there.
 *
 * The comparison reads the destination listing out of the query cache rather
 * than fetching it: that is the very listing the user is looking at when they
 * drop onto a folder, so it is both free and exactly what they think is
 * there. With no listing cached (uploading to a space root from Home, say)
 * there is nothing to compare against and the upload proceeds as before —
 * the server still refuses to overwrite, so the worst case is the old
 * "name (2)", never a lost file.
 */
export function useUploadWithConflicts(): (
  items: UploadItem[],
  spaceId: string,
  parentId?: string,
) => Promise<UploadOutcome> {
  const queryClient = useQueryClient();
  const enqueue = useUploadStore((s) => s.enqueue);

  return useCallback(
    async (items: UploadItem[], spaceId: string, parentId?: string): Promise<UploadOutcome> => {
      if (items.length === 0) return "uploaded";

      const existing = queryClient.getQueryData<Node[]>(keys.children(spaceId, parentId));
      const conflicts = findUploadConflicts(items, existing);
      if (conflicts.length === 0) {
        enqueue(items, spaceId, parentId);
        return "uploaded";
      }

      const choice = await UploadConflictDialog.show({ conflicts });
      if (choice === null) return "cancelled";
      if (choice === "keep-both") {
        enqueue(items, spaceId, parentId);
        return "uploaded";
      }

      const replacing = new Map(conflicts.map((c) => [c.name, c.existingId]));
      enqueue(
        items.map((item) =>
          item.path ? item : { ...item, replaceNodeId: replacing.get(item.file.name) },
        ),
        spaceId,
        parentId,
      );
      return "uploaded";
    },
    [queryClient, enqueue],
  );
}
