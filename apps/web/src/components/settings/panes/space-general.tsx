import { useState } from "react";
import { Trans, useLingui } from "@lingui/react/macro";
import { toast } from "sonner";
import { Button } from "@workspace/ui/components/button";
import { Input } from "@workspace/ui/components/input";
import { Label } from "@workspace/ui/components/label";
import { Switch } from "@workspace/ui/components/switch";
import { Textarea } from "@workspace/ui/components/textarea";
import type { Space, SpaceSettings } from "@/lib/api";
import { useSpace, useUpdateSpace } from "@/lib/queries";
import { useFormatters } from "@/lib/format";
import { useActiveSpaceId } from "@/lib/use-active-space";
import { SectionLabel } from "@/components/section-label";
import { useSettingsDirty } from "@/components/settings/settings-dirty";

export function SpaceGeneralPane() {
  const space = useSpace(useActiveSpaceId());

  if (!space)
    return (
      <p className="text-muted-foreground text-xs">
        <Trans>Loading…</Trans>
      </p>
    );
  // Keyed so switching space remounts the form rather than showing the
  // previous space's values in state that was initialised from it.
  return <GeneralForm key={space.id} space={space} />;
}

function GeneralForm({ space }: { space: Space }) {
  const { t } = useLingui();
  const f = useFormatters();
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
  useSettingsDirty(dirty);

  const discard = () => {
    setName(space.name);
    setSlug(space.slug ?? "");
    setDescription(space.description ?? "");
    setDefaultAsAdmin(space.default_as_admin ?? false);
    setQuotaGb(space.quota_bytes > 0 ? String(Math.round(space.quota_bytes / 1024 ** 3)) : "");
    setRetentionDays(space.trash_retention_days ? String(space.trash_retention_days) : "");
  };

  const usedSize = f.size(space.used_bytes);
  const quotaSize = f.size(space.quota_bytes);

  return (
    <form
      className="flex max-w-xl flex-col gap-6"
      onSubmit={(e) => {
        e.preventDefault();
        update.mutate(changed(), {
          onSuccess: () => toast.success(t`Settings saved`),
          onError: (e) => toast.error(t`Could not save`, { description: e.message }),
        });
      }}
    >
      <section className="flex flex-col gap-3">
        <SectionLabel>
          <Trans>Identity</Trans>
        </SectionLabel>

        <div className="flex flex-col gap-1">
          <Label htmlFor="space-name">
            <Trans>Space name</Trans>
          </Label>
          <p className="text-muted-foreground text-[10px]">
            <Trans>Shown in the switcher and every breadcrumb.</Trans>
          </p>
          <Input id="space-name" value={name} onChange={(e) => setName(e.target.value)} required />
        </div>

        <div className="flex flex-col gap-1">
          <Label htmlFor="space-slug">
            <Trans>URL slug</Trans>
          </Label>
          <Input
            id="space-slug"
            value={slug}
            onChange={(e) => setSlug(e.target.value)}
            placeholder={t`marketing`}
          />
          <p className="text-muted-foreground text-[10px]">
            <Trans>Lowercase letters, numbers and dashes. Leave empty for none.</Trans>
          </p>
        </div>

        <div className="flex flex-col gap-1">
          <Label htmlFor="space-description">
            <Trans>Description</Trans>
          </Label>
          <Textarea
            id="space-description"
            value={description}
            onChange={(e) => setDescription(e.target.value)}
            placeholder={t`What lives in this space?`}
          />
        </div>
      </section>

      <section className="flex flex-col gap-3">
        <SectionLabel>
          <Trans>Membership</Trans>
        </SectionLabel>
        <div className="flex items-start justify-between gap-4">
          <Label
            htmlFor="space-default-admin"
            className="cursor-pointer flex-col items-start gap-0.5 font-normal"
          >
            <Trans>New members are admins by default</Trans>
            <span className="text-muted-foreground block text-[10px]">
              <Trans>
                When on, everyone you invite can change these settings. Change it per-person on the
                Members pane.
              </Trans>
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
        <SectionLabel>
          <Trans>Limits</Trans>
        </SectionLabel>
        <div className="grid gap-3 sm:grid-cols-2">
          <div className="flex flex-col gap-1">
            <Label htmlFor="space-quota">
              <Trans>Storage quota (GB)</Trans>
            </Label>
            <Input
              id="space-quota"
              type="number"
              min={0}
              value={quotaGb}
              onChange={(e) => setQuotaGb(e.target.value)}
              placeholder={t`Unlimited`}
            />
            <p className="text-muted-foreground text-[10px]">
              <Trans>Empty means unlimited.</Trans>
            </p>
          </div>

          <div className="flex flex-col gap-1">
            <Label htmlFor="space-retention">
              <Trans>Trash retention (days)</Trans>
            </Label>
            <Input
              id="space-retention"
              type="number"
              min={0}
              value={retentionDays}
              onChange={(e) => setRetentionDays(e.target.value)}
              placeholder={t`Forever`}
            />
            <p className="text-muted-foreground text-[10px]">
              <Trans>
                Empty keeps trashed items until someone deletes them. A number purges them
                automatically — and permanently — once it passes.
              </Trans>
            </p>
          </div>
        </div>
        {/* One sentence per case rather than a clause bolted onto another:
            a quota only exists sometimes, and word order around it is not
            the translator's to guess. */}
        <p className="text-muted-foreground text-[10px] tabular-nums">
          {space.quota_bytes > 0 ? (
            <Trans>
              {usedSize} in use of {quotaSize}.
            </Trans>
          ) : (
            <Trans>{usedSize} in use.</Trans>
          )}
        </p>
      </section>

      <div className="border-border flex items-center gap-2 border-t pt-4">
        <Button type="submit" disabled={!dirty || update.isPending || !name.trim()}>
          <Trans>Save changes</Trans>
        </Button>
        <Button type="button" variant="ghost" disabled={!dirty} onClick={discard}>
          <Trans>Discard</Trans>
        </Button>
        <span className="text-muted-foreground text-[10px]">
          {dirty ? <Trans>Unsaved changes</Trans> : <Trans>No unsaved changes</Trans>}
        </span>
      </div>
    </form>
  );
}
