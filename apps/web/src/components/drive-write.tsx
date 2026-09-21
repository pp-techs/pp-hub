import { createContext, useCallback, useContext, useMemo, useRef, useState } from "react";
import type { FormEvent, ReactNode } from "react";
import { useSearch } from "@tanstack/react-router";
import { Trans, useLingui } from "@lingui/react/macro";
import { plural } from "@lingui/core/macro";
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
import { TeamFolderDialog } from "@/components/team-folder-dialog";
import { useUploadWithConflicts } from "@/components/upload-conflict-dialog";
import type { Node } from "@/lib/api";
import { collectInputFiles } from "@/lib/drop-items";
import {
  useCopyNode,
  useCreateFolder,
  useCreateTeamFolder,
  useMoveNode,
  useNode,
  useSpace,
} from "@/lib/queries";
import { useUiStore } from "@/lib/ui-store";
import { useActiveSpaceId } from "@/lib/use-active-space";
import { pasteTargets, writeTarget, type WriteTarget } from "@/lib/write-target";

export interface DriveWrite extends WriteTarget {
  /** What a Paste here would land. Empty means the item must be disabled. */
  pasteable: Node[];
  newFolder: () => void;
  /** True only for a space admin: the server rejects anyone else with a 403. */
  canCreateTeamFolder: boolean;
  /** Always creates at the space's top level — a team folder cannot be nested. */
  newTeamFolder: () => void;
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
  const { t } = useLingui();
  // `?dir=` belongs to the folder listing; every other surface resolves to the
  // space root, which is what it writes to.
  const { dir } = useSearch({ strict: false }) as { dir?: string };
  const space = useSpace(spaceId ?? "");
  const folder = useNode(dir);
  const dirs = useUiStore((s) => s.dirs);
  const clipboard = useUiStore((s) => s.clipboard);
  const clearClipboard = useUiStore((s) => s.clearClipboard);
  const upload = useUploadWithConflicts();
  const copy = useCopyNode(spaceId ?? "");
  const move = useMoveNode(spaceId ?? "");
  const mkdir = useCreateFolder(spaceId ?? "", dir);
  const mkteam = useCreateTeamFolder(spaceId ?? "");
  const fileRef = useRef<HTMLInputElement>(null);
  const folderRef = useRef<HTMLInputElement>(null);
  const [newFolderOpen, setNewFolderOpen] = useState(false);
  const [newFolderName, setNewFolderName] = useState("");

  // Fallback names for a destination whose row has not loaded yet. They are
  // interpolated into toasts, so they are translated here rather than in
  // `write-target.ts`, which stays a pure resolver with no i18n dependency.
  const fallbacks = useMemo(() => ({ space: t`this space`, dir: t`this folder` }), [t]);

  const target = useMemo(
    () =>
      writeTarget({
        spaceId,
        spaceName: space?.name,
        dir,
        dirName: dir ? dirs[dir]?.name : undefined,
        dirCanEdit: folder.data?.caps?.edit,
        fallbacks,
      }),
    [spaceId, space?.name, dir, dirs, folder.data?.caps?.edit, fallbacks],
  );
  const pasteable = useMemo(() => pasteTargets(clipboard, target, dirs), [clipboard, target, dirs]);
  const destination = target.label;

  const pickFiles = useCallback(
    (list: FileList | null) => {
      const items = collectInputFiles(list);
      const { spaceId: into, dir: parent, label } = target;
      if (items.length === 0 || !into) return;
      // Starting the upload is this surface's job; whether it has to ask
      // about same-named files first is the destination's, not the picker's.
      void upload(items, into, parent).then((outcome) => {
        if (outcome === "cancelled") {
          toast(t`Upload cancelled`);
          return;
        }
        const count = items.length;
        // The transfer tray reports progress but not the destination, and an
        // upload started from Home or Recent lands somewhere the user cannot see.
        toast.success(
          t`Uploading ${plural(count, { one: "# file", other: "# files" })} to ${label}`,
        );
      });
    },
    [upload, target, t],
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
          .catch((e: Error) => {
            const nodeName = node.name;
            toast.error(t`Could not paste “${nodeName}”`, { description: e.message });
          }),
      ),
    ).then(() => {
      if (done === 0) return;
      // The plural is written inside each `t` rather than hoisted into a
      // variable: only an inline macro becomes one ICU message. A hoisted
      // `plural()` is a second message spliced into this one, which is
      // exactly the fragment a translator cannot reorder.
      toast.success(
        cut
          ? t`Moved ${plural(done, { one: "# item", other: "# items" })} to ${destination}`
          : t`Copied ${plural(done, { one: "# item", other: "# items" })} to ${destination}`,
      );
      // A cut is consumed by its paste; a copy stays on the clipboard so the
      // same nodes can be pasted into a second folder.
      if (cut) clearClipboard();
    });
  }, [pasteable, clipboard, move, copy, target.dir, destination, clearClipboard, t]);

  // Creation lives here rather than in the menu because the menu is rendered
  // in two places: the rail's New and the listing's background. Note what is
  // *not* read — `target`: a team folder is always top level, so the folder
  // the user happens to be browsing has no say in where it lands.
  const newTeamFolder = useCallback(() => {
    if (!spaceId) return;
    void TeamFolderDialog.show({ spaceId, spaceName: space?.name ?? t`this space` }).then(
      (name) => {
        if (!name) return;
        mkteam.mutate(name, {
          onSuccess: (made) => {
            const folderName = made.name;
            toast.success(t`Created team folder “${folderName}”`, {
              description: t`Nobody can see it yet — access to a team folder is given through Share.`,
            });
          },
          onError: (e) => toast.error(t`Could not create team folder`, { description: e.message }),
        });
      },
    );
  }, [spaceId, space?.name, mkteam, t]);

  const value = useMemo<DriveWrite>(
    () => ({
      ...target,
      pasteable,
      newFolder: () => setNewFolderOpen(true),
      // A team folder belongs to the space, so only a space admin may start
      // one; the server answers everyone else with a 403.
      canCreateTeamFolder: space?.is_admin === true,
      newTeamFolder,
      uploadFiles: () => fileRef.current?.click(),
      uploadFolder: () => folderRef.current?.click(),
      paste,
    }),
    [target, pasteable, paste, space?.is_admin, newTeamFolder],
  );

  return (
    <DriveWriteContext.Provider value={value}>
      {children}

      {/* Two inputs rather than one: `webkitdirectory` is a property of the
          element, not of the click, so picking files and picking a folder
          cannot share a control. */}
      <FileInput ref={fileRef} onFiles={pickFiles} />
      <FileInput ref={folderRef} webkitdirectory onFiles={pickFiles} />

      <Dialog
        open={newFolderOpen}
        onOpenChange={(v) => {
          setNewFolderOpen(v);
          if (!v) setNewFolderName("");
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>
              <Trans>New folder</Trans>
            </DialogTitle>
            <DialogDescription>
              <Trans>Create a folder in {destination}.</Trans>
            </DialogDescription>
          </DialogHeader>
          <form
            onSubmit={(e: FormEvent) => {
              e.preventDefault();
              mkdir.mutate(newFolderName.trim(), {
                onSuccess: (made) => {
                  const createdName = made.name;
                  setNewFolderOpen(false);
                  setNewFolderName("");
                  toast.success(t`Created “${createdName}” in ${destination}`);
                },
                onError: (e) => toast.error(t`Could not create folder`, { description: e.message }),
              });
            }}
          >
            <Input
              autoFocus
              value={newFolderName}
              onChange={(e) => setNewFolderName(e.target.value)}
              placeholder={t`Folder name`}
              required
            />
            <DialogFooter className="mt-4">
              <Button type="submit" disabled={!newFolderName.trim() || mkdir.isPending}>
                <Trans>Create</Trans>
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
  // eslint-disable-next-line lingui/no-unlocalized-strings -- developer-facing Error text
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
