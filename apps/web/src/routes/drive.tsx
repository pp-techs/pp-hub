import { Outlet, createFileRoute, redirect, useNavigate } from "@tanstack/react-router";
import { FolderPlus, Plus } from "lucide-react";
import { useEffect, useState } from "react";
import type { FormEvent } from "react";
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
import { requireAuth, useCreateSpace, useSpaces } from "@/lib/queries";
import { useUiStore } from "@/lib/ui-store";
import { SpaceSwitcher } from "@/components/space-switcher";
import { NavUser } from "@/components/nav-user";
import { FolderTree } from "@/components/folder-tree";
import { QuickAccess } from "@/components/quick-access";
import { SpaceNav } from "@/components/space-nav";
import { RailSearch } from "@/components/rail-search";
import { StorageMeter } from "@/components/storage-meter";
import { RailLabel } from "@/components/rail-item";
import { TransferTray } from "@/components/transfer-tray";
import { CommandPalette } from "@/components/command-palette";

export const Route = createFileRoute("/drive")({
  head: () => ({ meta: [{ title: "Drive · pp-hub" }] }),
  beforeLoad: async ({ context }) => {
    await requireAuth(context.queryClient);
    if (/^\/drive\/?$/.test(location.pathname)) {
      const spaces = await context.queryClient.ensureQueryData({
        queryKey: ["spaces"],
        queryFn: () => import("@/lib/api").then((m) => m.getSpaces()),
      });
      if (spaces.length > 0) {
        const last = useUiStore.getState().lastSpaceId;
        const target = last ?? spaces[0].id;
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
  const setPaletteOpen = useUiStore((s) => s.setPaletteOpen);

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

  return (
    <div className="bg-background text-foreground flex h-svh overflow-hidden">
      <aside className="bg-sidebar text-sidebar-foreground border-border flex w-64 shrink-0 flex-col justify-between border-r">
        {/* Band 1 — active space */}
        <SpaceSwitcher />

        {/* Band 2 — search and navigation */}
        <div className="flex flex-1 flex-col gap-4 overflow-y-auto px-2 py-3">
          <RailSearch />

          <div>
            <RailLabel>Quick access</RailLabel>
            <QuickAccess />
          </div>

          <div>
            <RailLabel>Files</RailLabel>
            <FolderTree />
          </div>

          <div>
            <RailLabel>Spaces</RailLabel>
            <SpaceNav />
          </div>
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

      <NewSpaceDialog />
      {/* Mounted by the shell so uploads survive navigating between folders. */}
      <TransferTray />
      <CommandPalette />
    </div>
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
              <span className="text-sm">Make new members admins</span>
            </label>
          </div>

          <div className="flex flex-col gap-1">
            <Label htmlFor="new-space-quota">Storage limit (GB)</Label>
            <Input
              id="new-space-quota"
              type="number"
              min={0}
              value={quotaGb}
              onChange={(e) => setQuotaGb(e.target.value)}
              placeholder="Unlimited"
              className="max-w-32"
            />
            <p className="text-muted-foreground text-[10px]">
              Leave empty to share the server pool.
            </p>
          </div>

          <DialogFooter>
            <span className="text-muted-foreground mr-auto text-[10px]">
              You will be the owner.
            </span>
            <Button type="submit" disabled={!name.trim() || create.isPending}>
              Create space
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
