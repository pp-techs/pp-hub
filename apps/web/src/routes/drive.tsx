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
import { TeamSwitcher } from "@/components/team-switcher";
import { NavUser } from "@/components/nav-user";
import { FolderTree } from "@/components/folder-tree";

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
    <div className="bg-background text-foreground flex min-h-svh">
      <aside className="bg-sidebar text-sidebar-foreground flex w-64 shrink-0 flex-col border-r">
        {/* Team Switcher */}
        <div className="border-b p-2">
          <TeamSwitcher />
        </div>

        {/* Navigation sections */}
        <nav className="flex flex-1 flex-col overflow-y-auto p-2">
          {/* My Drive — folder tree */}
          <div className="flex flex-col gap-0.5">
            <span className="px-3 py-1 text-[11px] font-medium uppercase text-muted-foreground">
              My Drive
            </span>
            <FolderTree />
          </div>

          {/* Trash & Shared */}
          <div className="mt-4 flex flex-col gap-0.5">
            <Button variant="ghost" className="justify-start text-muted-foreground" disabled>
              Trash
            </Button>
            <Button variant="ghost" className="justify-start text-muted-foreground" disabled>
              Shared with me
            </Button>
          </div>
        </nav>

        {/* User profile */}
        <div className="border-t p-2">
          <NavUser />
        </div>
      </aside>

      <main className="flex min-w-0 flex-1 flex-col">
        {/* Always render both to keep hooks order stable */}
        <div className={showLanding ? "flex flex-1 flex-col" : "hidden"}>
          <LandingPage />
        </div>
        <div className={showLanding ? "hidden" : "flex flex-1 flex-col"}>
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
