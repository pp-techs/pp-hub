import { Link, Outlet, createFileRoute, redirect, useNavigate } from "@tanstack/react-router";
import { HardDrive, LogOut, Settings, UserRound } from "lucide-react";
import { useState } from "react";
import type { FormEvent } from "react";
import { Button } from "@workspace/ui/components/button";
import { Input } from "@workspace/ui/components/input";
import { Avatar, AvatarFallback } from "@workspace/ui/components/avatar";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@workspace/ui/components/dialog";
import { requireAuth } from "@/lib/auth";
import { logout, meOptions, useCreateSpace } from "@/lib/queries";
import { useUiStore } from "@/lib/ui-store";
import { SpaceNav } from "@/components/space-nav";

export const Route = createFileRoute("/drive")({
  head: () => ({ meta: [{ title: "Drive · pp-hub" }] }),
  beforeLoad: async ({ context }) => {
    await requireAuth(context.queryClient);
    // Landing on bare /drive: jump to the last visited space. The child
    // validates it still exists (notFound -> SpaceGone).
    if (/^\/drive\/?$/.test(location.pathname)) {
      const last = useUiStore.getState().lastSpaceId;
      if (last) {
        throw redirect({
          to: "/drive/$spaceId",
          params: { spaceId: last },
          search: { dir: undefined },
        });
      }
    }
  },
  component: DriveLayout,
});

function DriveLayout() {
  const navigate = useNavigate();
  const me = meOptions();

  return (
    <div className="bg-background text-foreground flex min-h-svh">
      <aside className="bg-sidebar text-sidebar-foreground flex w-64 shrink-0 flex-col border-r">
        <div className="flex items-center gap-2 px-3 py-3">
          <HardDrive className="size-5" />
          <span className="text-sm font-semibold">pp-hub</span>
        </div>
        <div className="flex flex-col gap-0.5 p-2">
          <Button
            variant="ghost"
            className="justify-start"
            nativeButton={false}
            render={<Link to="/drive" />}
          >
            <HardDrive data-icon="inline-start" /> Drive
          </Button>
          <Button variant="ghost" className="justify-start" disabled>
            <Settings data-icon="inline-start" /> Settings
          </Button>
          <Button variant="ghost" className="justify-start" disabled>
            <UserRound data-icon="inline-start" /> User Profile
          </Button>
        </div>
        <div className="flex min-h-0 flex-1 flex-col">
          <div className="px-3 py-1 text-[11px] font-medium uppercase text-muted-foreground">
            Spaces
          </div>
          <SpaceNav />
        </div>
        <div className="mt-auto border-t p-2">
          <div className="flex items-center gap-2 px-2 py-1.5">
            <Avatar size="sm">
              <AvatarFallback>
                {(me.data?.name || me.data?.email || "?").charAt(0).toUpperCase()}
              </AvatarFallback>
            </Avatar>
            <UserChip />
          </div>
          <Button
            variant="ghost"
            size="sm"
            className="mt-1 w-full justify-start"
            onClick={async () => {
              await logout();
              void navigate({ to: "/login" });
            }}
          >
            <LogOut data-icon="inline-start" /> Sign out
          </Button>
        </div>
      </aside>
      <main className="flex min-w-0 flex-1 flex-col">
        <Outlet />
      </main>
      <NewSpaceDialog
        onCreated={(id) => void navigate({ to: "/drive/$spaceId", params: { spaceId: id } })}
      />
    </div>
  );
}

function UserChip() {
  const me = meOptions();
  if (!me.data) return null;
  return (
    <p className="text-muted-foreground truncate text-xs" title={me.data.email}>
      {me.data.name || me.data.email}
    </p>
  );
}

function NewSpaceDialog({ onCreated }: { onCreated: (id: string) => void }) {
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
                onCreated(sp.id);
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
