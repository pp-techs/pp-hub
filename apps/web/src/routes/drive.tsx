import { Outlet, createFileRoute, redirect, useNavigate } from "@tanstack/react-router";
import { FolderPlus, Plus } from "lucide-react";
import { useState } from "react";
import type { FormEvent } from "react";
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
            <RailLabel>My Drive</RailLabel>
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
  const [name, setName] = useState("");
  const create = useCreateSpace();

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>New space</DialogTitle>
          <DialogDescription>Create a shared workspace.</DialogDescription>
        </DialogHeader>
        <form
          onSubmit={(e: FormEvent) => {
            e.preventDefault();
            create.mutate(name.trim(), {
              onSuccess: (sp) => {
                setOpen(false);
                setName("");
                void navigate({ to: "/drive/$spaceId", params: { spaceId: sp.id } });
              },
            });
          }}
        >
          <Input
            autoFocus
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="Space name"
            required
          />
          <DialogFooter className="mt-4">
            <Button type="submit" disabled={!name.trim() || create.isPending}>
              Create
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
