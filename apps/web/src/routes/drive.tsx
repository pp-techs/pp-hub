import { Outlet, createFileRoute, redirect, useNavigate } from "@tanstack/react-router";
import { FolderPlus, Plus } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import type { FormEvent, RefObject } from "react";
import { toast } from "sonner";
import { Button } from "@workspace/ui/components/button";
import { Label } from "@workspace/ui/components/label";
import { Textarea } from "@workspace/ui/components/textarea";
import { Input } from "@workspace/ui/components/input";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@workspace/ui/components/dialog";
import { fetchSpaces, requireAuth, useCreateSpace, useSpaces } from "@/lib/queries";
import { useUiStore } from "@/lib/ui-store";
import { useUploadStore } from "@/lib/upload-store";
import { useActiveSpaceId } from "@/lib/use-active-space";
import { FolderTree } from "@/components/folder-tree";
import { SpaceSwitcher } from "@/components/space-switcher";
import { NavUser } from "@/components/nav-user";
import { QuickAccess } from "@/components/quick-access";
import { StorageMeter } from "@/components/storage-meter";
import { TransferTray } from "@/components/transfer-tray";
import { CommandPalette } from "@/components/command-palette";
export const Route = createFileRoute("/drive")({
  head: () => ({ meta: [{ title: "Drive · pp-hub" }] }),
  beforeLoad: async ({ context, location }) => {
    const { queryClient } = context;
    // The spaces list doesn't depend on who signs in, so it goes out
    // alongside the auth check rather than behind it; nested routes pick up
    // the same in-flight query through the cache. The noop catch keeps a 401
    // from surfacing as unhandled when auth redirects to /login first.
    const spacesPromise = fetchSpaces(queryClient);
    spacesPromise.catch(() => {});
    await requireAuth(queryClient, location.href);
    if (/^\/drive\/?$/.test(location.pathname)) {
      const spaces = await spacesPromise;
      if (spaces.length > 0) {
        const last = useUiStore.getState().lastSpaceId;
        // The remembered space may have been deleted since it was recorded.
        const target = spaces.find((s) => s.id === last)?.id ?? spaces[0].id;
        throw redirect({
          to: "/drive/$spaceId",
          params: { spaceId: target },
          search: { dir: undefined },
        });
      }
    }
  },
  component: DriveLayout,
});

function DriveLayout() {
  const spaces = useSpaces();
  const showLanding = spaces.data?.length === 0;
  const spaceId = useActiveSpaceId();
  const setPaletteOpen = useUiStore((s) => s.setPaletteOpen);
  const setNewFolderOpen = useUiStore((s) => s.setNewFolderOpen);
  const enqueue = useUploadStore((s) => s.enqueue);
  const fileRef = useRef<HTMLInputElement>(null);
  const folderRef = useRef<HTMLInputElement>(null);

  // Bound once for the whole drive, so ⌘K works from the listing, a panel or
  // the rail. The palette itself only handles keys while it is open.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key.toLowerCase() === "k" && (e.metaKey || e.ctrlKey)) {
        e.preventDefault();
        setPaletteOpen(true);
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [setPaletteOpen]);

  const handleUploadFiles = () => fileRef.current?.click();
  const handleUploadFolder = () => folderRef.current?.click();

  return (
    <div className="bg-background text-foreground flex h-svh overflow-hidden">
      <aside className="bg-sidebar text-sidebar-foreground border-border flex w-64 shrink-0 flex-col justify-between border-r">
        {/* Space switcher */}
        <SpaceSwitcher spaceId={spaceId} />

        {/* Navigation */}
        <div className="flex flex-1 flex-col gap-4 overflow-y-auto px-2 py-3">
          {/* Both bands are about one space, so they wait for one rather than
              rendering an empty frame. Only true before the first space is
              created. */}
          {spaceId && (
            <QuickAccess
              spaceId={spaceId}
              onNewFolder={() => setNewFolderOpen(true)}
              onUploadFiles={handleUploadFiles}
              onUploadFolder={handleUploadFolder}
            >
              <FolderTree spaceId={spaceId} />
            </QuickAccess>
          )}
        </div>

        {/* Band 3 — storage and account */}
        <div className="border-border flex shrink-0 flex-col gap-3 border-t p-3">
          <StorageMeter />
          <div className="border-border border-t pt-2">
            <NavUser />
          </div>
        </div>
      </aside>

      <main className="flex min-w-0 flex-1 flex-col overflow-hidden">
        {/* Always render both to keep hooks order stable */}
        <div className={showLanding ? "flex flex-1 flex-col" : "hidden"}>
          <LandingPage />
        </div>
        <div className={showLanding ? "hidden" : "flex min-h-0 flex-1 flex-col"}>
          <Outlet />
        </div>
      </main>

      {/* Hidden file inputs triggered by QuickAccess */}
      <FileInput
        ref={fileRef}
        multiple
        onFiles={(files) => {
          if (files.length)
            enqueue(
              files.map((f) => ({ file: f, path: "" })),
              spaceId ?? "",
              undefined,
            );
        }}
      />
      <FileInput
        ref={folderRef}
        webkitdirectory
        onFiles={(files) => {
          if (files.length)
            enqueue(
              files.map((f) => ({ file: f, path: f.webkitRelativePath })),
              spaceId ?? "",
              undefined,
            );
        }}
      />

      <NewSpaceDialog />
      {/* Mounted by the shell so uploads survive navigating between folders. */}
      <TransferTray />
      <CommandPalette />
    </div>
  );
}

/** Hidden file input that resets after selection */
function FileInput({
  ref,
  multiple,
  webkitdirectory,
  onFiles,
}: {
  ref: RefObject<HTMLInputElement | null>;
  multiple?: boolean;
  webkitdirectory?: boolean;
  onFiles: (files: File[]) => void;
}) {
  return (
    <input
      ref={ref}
      type="file"
      multiple={multiple}
      {...(webkitdirectory ? { webkitdirectory: "" } : {})}
      className="hidden"
      onChange={(e) => {
        const files = e.target.files;
        if (files?.length) {
          onFiles([...files]);
        }
        e.target.value = "";
      }}
    />
  );
}

function LandingPage() {
  const setNewSpaceOpen = useUiStore((s) => s.setNewSpaceOpen);
  return (
    <div className="flex flex-1 flex-col items-center justify-center gap-4 p-8">
      <FolderPlus className="text-muted-foreground size-16" />
      <h1 className="text-xl font-semibold">Welcome to pp-hub</h1>
      <p className="text-muted-foreground max-w-sm text-center">
        Create your first space to start organizing your files and collaborating with your team.
      </p>
      <Button onClick={() => setNewSpaceOpen(true)}>
        <Plus data-icon="inline-start" /> Create space
      </Button>
    </div>
  );
}

function NewSpaceDialog() {
  const navigate = useNavigate();
  const open = useUiStore((s) => s.newSpaceOpen);
  const setOpen = useUiStore((s) => s.setNewSpaceOpen);
  const create = useCreateSpace();

  const [name, setName] = useState("");
  const [slug, setSlug] = useState("");
  const [description, setDescription] = useState("");
  const [defaultAsAdmin, setDefaultAsAdmin] = useState(false);
  const [quotaGb, setQuotaGb] = useState("");

  const reset = () => {
    setName("");
    setSlug("");
    setDescription("");
    setDefaultAsAdmin(false);
    setQuotaGb("");
  };

  return (
    <Dialog
      open={open}
      onOpenChange={(v) => {
        setOpen(v);
        if (!v) reset();
      }}
    >
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Create a space</DialogTitle>
          <DialogDescription>
            Spaces are isolated file areas with their own members, permissions and storage.
          </DialogDescription>
        </DialogHeader>
        <form
          className="flex flex-col gap-3"
          onSubmit={(e: FormEvent) => {
            e.preventDefault();
            create.mutate(
              {
                name: name.trim(),
                // Only send what was filled in; the server applies its own
                // defaults for the rest.
                slug: slug.trim() || undefined,
                description: description.trim() || undefined,
                default_as_admin: defaultAsAdmin,
                quota_bytes: quotaGb.trim() ? Number(quotaGb) * 1024 ** 3 : undefined,
              },
              {
                onSuccess: (sp) => {
                  setOpen(false);
                  reset();
                  void navigate({
                    to: "/drive/$spaceId",
                    params: { spaceId: sp.id },
                    search: { dir: undefined },
                  });
                },
                onError: (e) =>
                  toast.error("Could not create the space", {
                    description: e.message,
                  }),
              },
            );
          }}
        >
          <div className="flex flex-col gap-1">
            <Label htmlFor="new-space-name">Name</Label>
            <Input
              id="new-space-name"
              autoFocus
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="Marketing"
              required
            />
          </div>

          <div className="flex flex-col gap-1">
            <Label htmlFor="new-space-slug">Slug</Label>
            <Input
              id="new-space-slug"
              value={slug}
              onChange={(e) => setSlug(e.target.value)}
              placeholder="marketing"
            />
            <p className="text-muted-foreground text-[10px]">
              Lowercase letters, numbers and dashes. Optional.
            </p>
          </div>

          <div className="flex flex-col gap-1">
            <Label htmlFor="new-space-description">Description</Label>
            <Textarea
              id="new-space-description"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder="What lives in this space?"
            />
          </div>

          <div className="flex flex-col gap-1">
            <Label>New members are admins by default</Label>
            <p className="text-muted-foreground text-[10px]">
              When turned on, everyone you invite joins as an admin.
            </p>
            <label className="flex items-center gap-2">
              <input
                type="checkbox"
                checked={defaultAsAdmin}
                onChange={(e) => setDefaultAsAdmin(e.target.checked)}
              />
              <span className="text-sm">Enable</span>
            </label>
          </div>

          <div className="flex flex-col gap-1">
            <Label htmlFor="new-space-quota">Storage quota (GB)</Label>
            <Input
              id="new-space-quota"
              type="number"
              min="0"
              step="1"
              value={quotaGb}
              onChange={(e) => setQuotaGb(e.target.value)}
              placeholder="Unlimited"
            />
            <p className="text-muted-foreground text-[10px]">
              Leave empty for unlimited. Cannot be changed after creation.
            </p>
          </div>

          <DialogFooter>
            <Button type="submit" disabled={create.isPending}>
              {create.isPending ? "Creating…" : "Create"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
