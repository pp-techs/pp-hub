import { createFileRoute } from "@tanstack/react-router";
import { useState } from "react";
import { toast } from "sonner";
import { Button } from "@workspace/ui/components/button";
import { Input } from "@workspace/ui/components/input";
import { Label } from "@workspace/ui/components/label";
import { Switch } from "@workspace/ui/components/switch";
import { Textarea } from "@workspace/ui/components/textarea";
import { fmtSize, type Space, type SpaceSettings } from "@/lib/api";
import { useSpace, useUpdateSpace } from "@/lib/queries";
import { RailLabel } from "@/components/rail-item";

export const Route = createFileRoute("/drive/$spaceId/settings/general")({
  head: () => ({ meta: [{ title: "General · Space settings · pp-hub" }] }),
  component: GeneralPage,
});

function GeneralPage() {
  const { spaceId } = Route.useParams();
  const space = useSpace(spaceId);

  if (!space) return <p className="text-muted-foreground p-4 text-xs">Loading…</p>;
  // Keyed so switching space remounts the form rather than showing the
  // previous space's values in state that was initialised from it.
  return <GeneralForm key={space.id} space={space} />;
}

function GeneralForm({ space }: { space: Space }) {
  const update = useUpdateSpace(space.id);
  const [name, setName] = useState(space.name);
  const [slug, setSlug] = useState(space.slug ?? "");
  const [description, setDescription] = useState(space.description ?? "");
  const [defaultAsAdmin, setDefaultAsAdmin] = useState(space.default_as_admin ?? false);
  // Held as a String so the field can be emptied without becoming 0, which
  // means "unlimited" and is a very different thing from "not yet typed".
  const [quotaGb, setQuotaGb] = useState(
    space.quota_bytes > 0 ? String(Math.round(space.quota_bytes / 1024 ** 3)) : "",
  );
  // Same String-not-number reasoning as the quota: empty means "keep forever",
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
   * writes that empty String over real data. Diffing removes that failure mode
   * rather than relying on every read being complete.
   */
  const changed = (): SpaceSettings => {
    const quota = quotaGb.trim() ? Number(quotaGb) * 1024 ** 3 : 0;
    const retention = retentionDays.trim() ? Number(retentionDays) : 0;
    const next: SpaceSettings = {};
    if (name.trim() !== space.name) next.name = name.trim();
    if (slug.trim() !== (space.slug ?? "")) next.slug = slug.trim();
    if (description.trim() !== (space.description ?? "")) next.description = description.trim();
    if (defaultAsAdmin !== (space.default_as_admin ?? false)) {
      next.default_as_admin = defaultAsAdmin;
    }
    if (quota !== space.quota_bytes) next.quota_bytes = quota;
    if (retention !== (space.trash_retention_days ?? 0)) next.trash_retention_days = retention;
    return next;
  };

  const dirty = Object.keys(changed()).length > 0;

  const discard = () => {
    setName(space.name);
    setSlug(space.slug ?? "");
    setDescription(space.description ?? "");
    setDefaultAsAdmin(space.default_as_admin ?? false);
    setQuotaGb(space.quota_bytes > 0 ? String(Math.round(space.quota_bytes / 1024 ** 3)) : "");
    setRetentionDays(space.trash_retention_days ? String(space.trash_retention_days) : "");
  };

  return (
    <form
      className="flex max-w-xl flex-col gap-6 overflow-y-auto p-4"
      onSubmit={(e) => {
        e.preventDefault();
        update.mutate(changed(), {
          onSuccess: () => toast.success("Settings saved"),
          onError: (e) => toast.error("Could not save", { description: e.message }),
        });
      }}
    >
      <section className="flex flex-col gap-3">
        <RailLabel>Identity</RailLabel>

        <div className="flex flex-col gap-1">
          <Label htmlFor="space-name">Space name</Label>
          <p className="text-muted-foreground text-[10px]">
            Shown in the switcher and every breadcrumb.
          </p>
          <Input id="space-name" value={name} onChange={(e) => setName(e.target.value)} required />
        </div>

        <div className="flex flex-col gap-1">
          <Label htmlFor="space-slug">URL slug</Label>
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
      </section>

      <section className="flex flex-col gap-3">
        <RailLabel>Membership</RailLabel>
        <div className="flex items-start justify-between gap-4">
          <Label htmlFor="space-default-admin" className="cursor-pointer font-normal">
            New members are admins by default
            <span className="text-muted-foreground block text-[10px]">
              When on, everyone you invite can change these settings. Change it per-person on the
              People tab.
            </span>
          </Label>
          <Switch
            id="space-default-admin"
            checked={defaultAsAdmin}
            onCheckedChange={setDefaultAsAdmin}
          />
        </div>
      </section>

      <section className="flex flex-col gap-3">
        <RailLabel>Limits</RailLabel>
        <div className="grid gap-3 sm:grid-cols-2">
          <div className="flex flex-col gap-1">
            <Label htmlFor="space-quota">Storage quota (GB)</Label>
            <Input
              id="space-quota"
              type="number"
              min={0}
              value={quotaGb}
              onChange={(e) => setQuotaGb(e.target.value)}
              placeholder="Unlimited"
            />
            <p className="text-muted-foreground text-[10px]">Empty means unlimited.</p>
          </div>

          <div className="flex flex-col gap-1">
            <Label htmlFor="space-retention">Trash retention (days)</Label>
            <Input
              id="space-retention"
              type="number"
              min={0}
              value={retentionDays}
              onChange={(e) => setRetentionDays(e.target.value)}
              placeholder="Forever"
            />
            <p className="text-muted-foreground text-[10px]">
              Empty keeps trashed items until someone deletes them. A number purges them
              automatically — and permanently — once it passes.
            </p>
          </div>
        </div>
        <p className="text-muted-foreground text-[10px] tabular-nums">
          {fmtSize(space.used_bytes)} in use
          {space.quota_bytes > 0 && ` of ${fmtSize(space.quota_bytes)}`}.
        </p>
      </section>

      <div className="border-border flex items-center gap-2 border-t pt-4">
        <Button type="submit" disabled={!dirty || update.isPending || !name.trim()}>
          Save changes
        </Button>
        <Button type="button" variant="ghost" disabled={!dirty} onClick={discard}>
          Discard
        </Button>
        <span className="text-muted-foreground text-[10px]">
          {dirty ? "Unsaved changes" : "No unsaved changes"}
        </span>
      </div>
    </form>
  );
}
