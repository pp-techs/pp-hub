import { Outlet, createFileRoute, redirect, useNavigate } from "@tanstack/react-router";
import { FolderPlus, Plus } from "lucide-react";
import { useEffect, useState } from "react";
import type { CSSProperties, FormEvent } from "react";
import { toast } from "sonner";
import { Trans, useLingui } from "@lingui/react/macro";
import { msg } from "@lingui/core/macro";
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
import { fetchSpaces, requireAuth, useCreateSpace, useMe, useSpaces } from "@/lib/queries";
import { useUiStore } from "@/lib/ui-store";
import { useActiveSpaceId } from "@/lib/use-active-space";
import { DriveWriteProvider } from "@/components/drive-write";
import { AppSidebar } from "@/components/app-sidebar";
import { HeaderSlotProvider } from "@/components/header-slot";
import { SiteHeader } from "@/components/site-header";
import { TransferTray } from "@/components/transfer-tray";
import { CommandPalette } from "@/components/command-palette";
import { SidebarInset, SidebarProvider } from "@workspace/ui/components/sidebar";
import { pageTitle } from "@/lib/i18n";
export const Route = createFileRoute("/drive")({
  head: () => ({ meta: [{ title: pageTitle(msg`Drive`) }] }),
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
    // `HeaderSlotProvider` and `DriveWriteProvider` wrap the provider, never
    // sit between it and `SidebarInset`: the inset's margin/rounding is
    // `peer-data-[variant=inset]`, which only matches while the inset is a
    // direct sibling of the sidebar.
    <HeaderSlotProvider>
      <DriveWriteProvider>
        {/* `SidebarProvider` is only `min-h-svh`; the drive clamps to the
            viewport and scrolls inside the listing, so the shell pins the
            height here. Miss this and the listing grows the page instead of
            its own scroller — which is also what the marquee band measures
            against. */}
        <SidebarProvider
          className="h-svh overflow-hidden"
          style={
            {
              "--sidebar-width": "calc(var(--spacing) * 72)",
              "--header-height": "calc(var(--spacing) * 12)",
            } as CSSProperties
          }
        >
          <AppSidebar variant="inset" spaceId={spaceId} />

          <SidebarInset className="min-h-0 overflow-hidden">
            <SiteHeader />
            {/* Always render both to keep hooks order stable */}
            <div className={showLanding ? "flex flex-1 flex-col" : "hidden"}>
              <LandingPage />
            </div>
            <div className={showLanding ? "hidden" : "flex min-h-0 flex-1 flex-col"}>
              <Outlet />
            </div>
          </SidebarInset>

          <NewSpaceDialog />
          {/* Mounted by the shell so uploads survive navigating between folders. */}
          <TransferTray />
          <CommandPalette />
        </SidebarProvider>
      </DriveWriteProvider>
    </HeaderSlotProvider>
  );
}

function LandingPage() {
  const setNewSpaceOpen = useUiStore((s) => s.setNewSpaceOpen);
  const canCreateSpaces = useMe().data?.can_create_spaces ?? false;

  // Belonging to no space means two very different things depending on who
  // creates spaces here. Offering "create your first space" to somebody who
  // cannot is worse than unhelpful — it tells them the fix is theirs.
  if (!canCreateSpaces) {
    return (
      <div className="flex flex-1 flex-col items-center justify-center gap-4 p-8">
        <FolderPlus className="text-muted-foreground size-16" />
        <h1 className="text-xl font-semibold">
          <Trans>You are not in any space yet</Trans>
        </h1>
        <p className="text-muted-foreground max-w-sm text-center">
          <Trans>
            Spaces follow the organizations you belong to at your identity provider. Ask an
            administrator to add you to one — it appears here the next time you sign in.
          </Trans>
        </p>
      </div>
    );
  }

  return (
    <div className="flex flex-1 flex-col items-center justify-center gap-4 p-8">
      <FolderPlus className="text-muted-foreground size-16" />
      <h1 className="text-xl font-semibold">
        <Trans>Welcome to pp-hub</Trans>
      </h1>
      <p className="text-muted-foreground max-w-sm text-center">
        <Trans>
          Create your first space to start organizing your files and collaborating with your team.
        </Trans>
      </p>
      <Button onClick={() => setNewSpaceOpen(true)}>
        <Plus data-icon="inline-start" /> <Trans>Create space</Trans>
      </Button>
    </div>
  );
}

function NewSpaceDialog() {
  const navigate = useNavigate();
  const open = useUiStore((s) => s.newSpaceOpen);
  const setOpen = useUiStore((s) => s.setNewSpaceOpen);
  const create = useCreateSpace();
  const { t } = useLingui();

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
          <DialogTitle>
            <Trans>Create a space</Trans>
          </DialogTitle>
          <DialogDescription>
            <Trans>
              Spaces are isolated file areas with their own members, permissions and storage.
            </Trans>
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
                  toast.error(t`Could not create the space`, {
                    description: e.message,
                  }),
              },
            );
          }}
        >
          <div className="flex flex-col gap-1">
            <Label htmlFor="new-space-name">
              <Trans>Name</Trans>
            </Label>
            <Input
              id="new-space-name"
              autoFocus
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder={t`Marketing`}
              required
            />
          </div>

          <div className="flex flex-col gap-1">
            <Label htmlFor="new-space-slug">
              <Trans>Slug</Trans>
            </Label>
            <Input
              id="new-space-slug"
              value={slug}
              onChange={(e) => setSlug(e.target.value)}
              placeholder={t`marketing`}
            />
            <p className="text-muted-foreground text-[10px]">
              <Trans>Lowercase letters, numbers and dashes. Optional.</Trans>
            </p>
          </div>

          <div className="flex flex-col gap-1">
            <Label htmlFor="new-space-description">
              <Trans>Description</Trans>
            </Label>
            <Textarea
              id="new-space-description"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder={t`What lives in this space?`}
            />
          </div>

          <div className="flex flex-col gap-1">
            <Label>
              <Trans>New members are admins by default</Trans>
            </Label>
            <p className="text-muted-foreground text-[10px]">
              <Trans>When turned on, everyone you invite joins as an admin.</Trans>
            </p>
            <label className="flex items-center gap-2">
              <input
                type="checkbox"
                checked={defaultAsAdmin}
                onChange={(e) => setDefaultAsAdmin(e.target.checked)}
              />
              <span className="text-sm">
                <Trans>Enable</Trans>
              </span>
            </label>
          </div>

          <div className="flex flex-col gap-1">
            <Label htmlFor="new-space-quota">
              <Trans>Storage quota (GB)</Trans>
            </Label>
            <Input
              id="new-space-quota"
              type="number"
              min="0"
              step="1"
              value={quotaGb}
              onChange={(e) => setQuotaGb(e.target.value)}
              placeholder={t`Unlimited`}
            />
            <p className="text-muted-foreground text-[10px]">
              <Trans>Leave empty for unlimited. Cannot be changed after creation.</Trans>
            </p>
          </div>

          <DialogFooter>
            <Button type="submit" disabled={create.isPending}>
              {create.isPending ? <Trans>Creating…</Trans> : <Trans>Create</Trans>}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
