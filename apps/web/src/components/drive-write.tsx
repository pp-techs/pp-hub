import { createContext, useCallback, useContext, useMemo, useRef, useState } from "react";
import type { FormEvent, ReactNode } from "react";
import { useSearch } from "@tanstack/react-router";
import { toast } from "sonner";
import { Button } from "@workspace/ui/components/button";
import { Input } from "@workspace/ui/components/input";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@workspace/ui/components/dialog";
import type { Node } from "@/lib/api";
import { collectInputFiles } from "@/lib/drop-items";
import { useCopyNode, useCreateFolder, useMoveNode, useNode, useSpace } from "@/lib/queries";
import { useUiStore } from "@/lib/ui-store";
import { useUploadStore } from "@/lib/upload-store";
import { useActiveSpaceId } from "@/lib/use-active-space";
import { pasteTargets, writeTarget, type WriteTarget } from "@/lib/write-target";

export interface DriveWrite extends WriteTarget {
  /** What a Paste here would land. Empty means the item must be disabled. */
  pasteable: Node[];
  newFolder: () => void;
  uploadFiles: () => void;
  uploadFolder: () => void;
  paste: () => void;
}

const DriveWriteContext = createContext<DriveWrite | null>(null);

/**
 * The one owner of where a write lands, and of the controls that perform it:
 * both hidden file inputs, the New-folder dialog and the paste.
 *
 * It is mounted by the drive shell because that is the only ancestor both
 * consumers share — the rail's New menu is a sibling of the inset, the
 * listing's background menu is inside it. Ownership anywhere lower reaches one
 * of them and not the other, which is exactly how the rail came to upload into
 * the space root from inside a folder. The target itself is derived from the
 * router, so the two can only ever agree.
 *
 * Wraps `SidebarProvider` rather than sitting inside it: the inset's margin
 * and rounding are `peer-data-[variant=inset]`, which only matches while the
 * inset is a direct sibling of the sidebar.
 */
export function DriveWriteProvider({ children }: { children: ReactNode }) {
  const spaceId = useActiveSpaceId();
  // `?dir=` belongs to the folder listing; every other surface resolves to the
  // space root, which is what it writes to.
  const { dir } = useSearch({ strict: false }) as { dir?: string };
  const space = useSpace(spaceId ?? "");
  const folder = useNode(dir);
  const dirs = useUiStore((s) => s.dirs);
  const clipboard = useUiStore((s) => s.clipboard);
  const clearClipboard = useUiStore((s) => s.clearClipboard);
  const enqueue = useUploadStore((s) => s.enqueue);
  const copy = useCopyNode(spaceId ?? "");
  const move = useMoveNode(spaceId ?? "");
  const mkdir = useCreateFolder(spaceId ?? "", dir);
  const fileRef = useRef<HTMLInputElement>(null);
  const folderRef = useRef<HTMLInputElement>(null);
  const [newFolderOpen, setNewFolderOpen] = useState(false);
  const [newFolderName, setNewFolderName] = useState("");

  const target = useMemo(
    () =>
      writeTarget({
        spaceId,
        spaceName: space?.name,
        dir,
        dirName: dir ? dirs[dir]?.name : undefined,
        dirCanEdit: folder.data?.caps?.edit,
      }),
    [spaceId, space?.name, dir, dirs, folder.data?.caps?.edit],
  );
  const pasteable = useMemo(() => pasteTargets(clipboard, target, dirs), [clipboard, target, dirs]);

  const upload = useCallback(
    (list: FileList | null) => {
      const items = collectInputFiles(list);
      if (items.length === 0 || !target.spaceId) return;
      enqueue(items, target.spaceId, target.dir);
      // The transfer tray reports progress but not the destination, and an
      // upload started from Home or Recent lands somewhere the user cannot see.
      toast.success(
        items.length === 1
          ? `Uploading 1 file to ${target.label}`
          : `Uploading ${items.length} files to ${target.label}`,
      );
    },
    [enqueue, target.spaceId, target.dir, target.label],
  );

  const paste = useCallback(() => {
    if (pasteable.length === 0 || !clipboard) return;
    const cut = clipboard.mode === "cut";
    const nodes = pasteable;
    let done = 0;
    // Each node settles on its own, so one rejection — a name already taken in
    // the destination — does not hold up the rest.
    void Promise.all(
      nodes.map((node) =>
        (cut ? move : copy)
          .mutateAsync({ nodeId: node.id, parentId: target.dir })
          .then(() => {
            done += 1;
          })
          .catch((e: Error) =>
            toast.error(`Could not paste “${node.name}”`, { description: e.message }),
          ),
      ),
    ).then(() => {
      if (done === 0) return;
      const what = done === 1 ? "1 item" : `${done} items`;
      toast.success(`${cut ? "Moved" : "Copied"} ${what} to ${target.label}`);
      // A cut is consumed by its paste; a copy stays on the clipboard so the
      // same nodes can be pasted into a second folder.
      if (cut) clearClipboard();
    });
  }, [pasteable, clipboard, move, copy, target.dir, target.label, clearClipboard]);

  const value = useMemo<DriveWrite>(
    () => ({
      ...target,
      pasteable,
      newFolder: () => setNewFolderOpen(true),
      uploadFiles: () => fileRef.current?.click(),
      uploadFolder: () => folderRef.current?.click(),
      paste,
    }),
    [target, pasteable, paste],
  );

  return (
    <DriveWriteContext.Provider value={value}>
      {children}

      {/* Two inputs rather than one: `webkitdirectory` is a property of the
          element, not of the click, so picking files and picking a folder
          cannot share a control. */}
      <FileInput ref={fileRef} onFiles={upload} />
      <FileInput ref={folderRef} webkitdirectory onFiles={upload} />

      <Dialog
        open={newFolderOpen}
        onOpenChange={(v) => {
          setNewFolderOpen(v);
          if (!v) setNewFolderName("");
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>New folder</DialogTitle>
            <DialogDescription>Create a folder in {target.label}.</DialogDescription>
          </DialogHeader>
          <form
            onSubmit={(e: FormEvent) => {
              e.preventDefault();
              mkdir.mutate(newFolderName.trim(), {
                onSuccess: (made) => {
                  setNewFolderOpen(false);
                  setNewFolderName("");
                  toast.success(`Created “${made.name}” in ${target.label}`);
                },
                onError: (e) => toast.error("Could not create folder", { description: e.message }),
              });
            }}
          >
            <Input
              autoFocus
              value={newFolderName}
              onChange={(e) => setNewFolderName(e.target.value)}
              placeholder="Folder name"
              required
            />
            <DialogFooter className="mt-4">
              <Button type="submit" disabled={!newFolderName.trim() || mkdir.isPending}>
                Create
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
    </DriveWriteContext.Provider>
  );
}

export function useDriveWrite(): DriveWrite {
  const value = useContext(DriveWriteContext);
  if (!value) throw new Error("useDriveWrite must be used inside DriveWriteProvider");
  return value;
}

/** Hidden file input that resets after selection, so the same pick repeats. */
function FileInput({
  ref,
  webkitdirectory,
  onFiles,
}: {
  ref: React.RefObject<HTMLInputElement | null>;
  webkitdirectory?: boolean;
  onFiles: (files: FileList | null) => void;
}) {
  return (
    <input
      ref={ref}
      type="file"
      multiple
      {...(webkitdirectory ? { webkitdirectory: "" } : {})}
      className="hidden"
      onChange={(e) => {
        onFiles(e.target.files);
        e.target.value = "";
      }}
    />
  );
}
