import { Outlet, createFileRoute, redirect, useNavigate } from "@tanstack/react-router";
import { LogOut, Plus } from "lucide-react";
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
  DialogTrigger,
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
  return (
    <div className="bg-background text-foreground flex min-h-svh">
      <aside className="text-sidebar-foreground bg-sidebar flex w-64 shrink-0 flex-col border-r">
        <div className="flex items-center justify-between px-3 py-3">
          <span className="text-sm font-semibold">pp-hub</span>
          <NewSpaceDialog
            onCreated={(id) => void navigate({ to: "/drive/$spaceId", params: { spaceId: id } })}
          />
        </div>
        <SpaceNav />
        <div className="border-t p-2">
          <UserChip />
          <Button
            variant="ghost"
            size="sm"
            className="mt-1 w-full justify-start"
            onClick={async () => {
              await logout();
              void navigate({ to: "/login" });
            }}
          >
            <LogOut /> Sign out
          </Button>
        </div>
      </aside>
      <main className="flex min-w-0 flex-1 flex-col">
        <Outlet />
      </main>
    </div>
  );
}

function UserChip() {
  const me = meOptions();
  if (!me.data) return null;
  return (
    <p className="text-muted-foreground truncate px-2 text-xs" title={me.data.email}>
      {me.data.name || me.data.email}
    </p>
  );
}

function NewSpaceDialog({ onCreated }: { onCreated: (id: string) => void }) {
  const [open, setOpen] = useState(false);
  const [name, setName] = useState("");
  const create = useCreateSpace();

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger
        render={
          <Button variant="ghost" size="icon-sm" aria-label="New space">
            <Plus />
          </Button>
        }
      />
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
