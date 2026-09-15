import { useState } from "react";
import { useNavigate } from "@tanstack/react-router";
import { toast } from "sonner";
import { Trash2 } from "lucide-react";
import { Button } from "@workspace/ui/components/button";
import { Input } from "@workspace/ui/components/input";
import { Label } from "@workspace/ui/components/label";
import { Textarea } from "@workspace/ui/components/textarea";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@workspace/ui/components/tabs";
import { ToggleGroup, ToggleGroupItem } from "@workspace/ui/components/toggle-group";
import { fmtSize, type MemberRole, type Space, type SpaceSettings } from "@/lib/api";
import { useDeleteSpace, useUpdateSpace } from "@/lib/queries";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { SpaceMembersPanel } from "@/components/space-members-panel";

/**
 * Space settings, reached from the rail as `?panel=settings`.
 *
 * The Stitch design has six tabs — General, Members, Sharing, Storage,
 * Integrations, Danger zone. Three of those have no server behind them yet
 * (per-space sharing policy, storage tiering, integrations), so they are left
 * off rather than shipped as empty panes.
 */
export function SpaceSettingsPanel({ space }: { space: Space }) {
  const isOwner = space.role === "owner";

  if (!isOwner) {
    return (
      <div className="flex flex-col gap-1">
        <h2 className="font-heading text-sm font-medium">Settings</h2>
        <p className="text-muted-foreground text-xs">
          Only a space owner can change these settings.
        </p>
      </div>
    );
  }

  return (
    <Tabs defaultValue="general" className="flex flex-col gap-4">
      <TabsList variant="line">
        <TabsTrigger value="general">General</TabsTrigger>
        <TabsTrigger value="members">Members</TabsTrigger>
        <TabsTrigger value="danger">Danger zone</TabsTrigger>
      </TabsList>

      <TabsContent value="general">
        <GeneralTab space={space} />
      </TabsContent>
      <TabsContent value="members">
        <MembersTab space={space} />
      </TabsContent>
      <TabsContent value="danger">
        <DangerTab space={space} />
      </TabsContent>
    </Tabs>
  );
}

function GeneralTab({ space }: { space: Space }) {
  const update = useUpdateSpace(space.id);
  const [name, setName] = useState(space.name);
  const [slug, setSlug] = useState(space.slug ?? "");
  const [description, setDescription] = useState(space.description ?? "");
  const [defaultRole, setDefaultRole] = useState<MemberRole>(space.default_role ?? "viewer");
  // Held as a string so the field can be emptied without becoming 0, which
  // means "unlimited" and is a very different thing from "not yet typed".
  const [quotaGb, setQuotaGb] = useState(
    space.quota_bytes > 0 ? String(Math.round(space.quota_bytes / 1024 ** 3)) : "",
  );
  // Same string-not-number reasoning as the quota: empty means "keep forever",
  // which is a different thing from a typed 0.
  const [retentionDays, setRetentionDays] = useState(
    space.trash_retention_days ? String(space.trash_retention_days) : "",
  );

  /**
   * Sends only the fields that actually changed.
   *
   * The server treats an absent field as "leave it alone", so a partial patch
   * is safe. Sending the whole set is not: if the form ever initialises from a
   * response that is missing a field, it holds "" for it and the next save
   * writes that empty string over real data. Diffing removes that failure mode
   * rather than relying on every read being complete.
   */
  const save = () => {
    const quota = quotaGb.trim() ? Number(quotaGb) * 1024 ** 3 : 0;
    const changed: SpaceSettings = {};
    if (name.trim() !== space.name) changed.name = name.trim();
    if (slug.trim() !== (space.slug ?? "")) changed.slug = slug.trim();
    if (description.trim() !== (space.description ?? "")) {
      changed.description = description.trim();
    }
    if (defaultRole !== (space.default_role ?? "viewer")) changed.default_role = defaultRole;
    if (quota !== space.quota_bytes) changed.quota_bytes = quota;
    const retention = retentionDays.trim() ? Number(retentionDays) : 0;
    if (retention !== (space.trash_retention_days ?? 0)) {
      changed.trash_retention_days = retention;
    }

    if (Object.keys(changed).length === 0) {
      toast.info("Nothing to save");
      return;
    }

    update.mutate(changed, {
      onSuccess: () => toast.success("Settings saved"),
      onError: (e) => toast.error("Could not save", { description: e.message }),
    });
  };

  return (
    <form
      className="flex max-w-lg flex-col gap-3"
      onSubmit={(e) => {
        e.preventDefault();
        save();
      }}
    >
      <div className="flex flex-col gap-1">
        <Label htmlFor="space-name">Name</Label>
        <Input id="space-name" value={name} onChange={(e) => setName(e.target.value)} required />
      </div>

      <div className="flex flex-col gap-1">
        <Label htmlFor="space-slug">Slug</Label>
        <Input
          id="space-slug"
          value={slug}
          onChange={(e) => setSlug(e.target.value)}
          placeholder="marketing"
        />
        <p className="text-muted-foreground text-[10px]">
          Lowercase letters, numbers and dashes. Leave empty for none.
        </p>
      </div>

      <div className="flex flex-col gap-1">
        <Label htmlFor="space-description">Description</Label>
        <Textarea
          id="space-description"
          value={description}
          onChange={(e) => setDescription(e.target.value)}
          placeholder="What lives in this space?"
        />
      </div>

      <div className="flex flex-col gap-1">
        <Label>Default member role</Label>
        <ToggleGroup
          value={[defaultRole]}
          onValueChange={(v) => v[0] && setDefaultRole(v[0] as MemberRole)}
        >
          <ToggleGroupItem value="viewer">Viewer</ToggleGroupItem>
          <ToggleGroupItem value="editor">Editor</ToggleGroupItem>
          <ToggleGroupItem value="owner">Owner</ToggleGroupItem>
        </ToggleGroup>
      </div>

      <div className="flex flex-col gap-1">
        <Label htmlFor="space-quota">Storage limit (GB)</Label>
        <Input
          id="space-quota"
          type="number"
          min={0}
          value={quotaGb}
          onChange={(e) => setQuotaGb(e.target.value)}
          placeholder="Unlimited"
          className="max-w-32"
        />
        <p className="text-muted-foreground text-[10px] tabular-nums">
          {fmtSize(space.used_bytes)} in use. Leave empty to share the server pool.
        </p>
      </div>

      <div className="flex flex-col gap-1">
        <Label htmlFor="space-retention">Keep deleted items for (days)</Label>
        <Input
          id="space-retention"
          type="number"
          min={0}
          value={retentionDays}
          onChange={(e) => setRetentionDays(e.target.value)}
          placeholder="Forever"
          className="max-w-32"
        />
        <p className="text-muted-foreground text-[10px]">
          Leave empty to keep trashed items until someone deletes them. Setting a number means they
          are purged automatically — and permanently — once it passes.
        </p>
      </div>

      <div>
        <Button type="submit" disabled={update.isPending || !name.trim()}>
          Save changes
        </Button>
      </div>
    </form>
  );
}

function MembersTab({ space }: { space: Space }) {
  return <SpaceMembersPanel spaceId={space.id} currentUserRole={space.role} />;
}

function DangerTab({ space }: { space: Space }) {
  const navigate = useNavigate();
  const remove = useDeleteSpace();
  const [confirming, setConfirming] = useState(false);
  const [typed, setTyped] = useState("");

  return (
    <div className="border-destructive/30 flex max-w-lg flex-col gap-2 rounded-lg border p-3">
      <h3 className="text-destructive text-xs font-medium">Delete this space</h3>
      <p className="text-muted-foreground text-xs">
        Removes “{space.name}” and everything in it — every file, version, share and member. The
        blobs are released and cannot be recovered.
      </p>
      <div className="flex flex-col gap-1">
        <Label htmlFor="confirm-name">Type the space name to confirm</Label>
        <Input
          id="confirm-name"
          value={typed}
          onChange={(e) => setTyped(e.target.value)}
          placeholder={space.name}
          className="max-w-64"
        />
      </div>
      <div>
        <Button
          variant="destructive"
          disabled={typed !== space.name || remove.isPending}
          onClick={() => setConfirming(true)}
        >
          <Trash2 data-icon="inline-start" /> Delete space
        </Button>
      </div>

      <ConfirmDialog
        open={confirming}
        onOpenChange={setConfirming}
        title="Delete space"
        description={`Permanently delete “${space.name}” and all of its contents? This cannot be undone.`}
        confirmLabel="Delete space"
        confirmVariant="destructive"
        onConfirm={() =>
          remove.mutate(space.id, {
            onSuccess: () => {
              toast.success(`Deleted “${space.name}”`);
              void navigate({ to: "/drive" });
            },
            onError: (e) => toast.error("Could not delete", { description: e.message }),
          })
        }
      />
    </div>
  );
}
