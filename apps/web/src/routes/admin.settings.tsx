import { createFileRoute, Link } from "@tanstack/react-router";
import { useState } from "react";
import { toast } from "sonner";
import { Trans, useLingui } from "@lingui/react/macro";
import { msg } from "@lingui/core/macro";
import { RotateCcw, Save } from "lucide-react";
import { Button } from "@workspace/ui/components/button";
import { Input } from "@workspace/ui/components/input";
import { Label } from "@workspace/ui/components/label";
import { Badge } from "@workspace/ui/components/badge";
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@workspace/ui/components/card";
import { AdminHeader } from "@/components/admin-header";
import { SectionLabel } from "@/components/section-label";
import {
  adminSettingsOptions,
  requireAuth,
  useAdminSettings,
  useDeleteAdminSetting,
  useMe,
  useUpdateAdminSettings,
} from "@/lib/queries";
import { pageTitle } from "@/lib/i18n";
import type { AdminSetting, AdminSettingsMap } from "@/lib/api";

export const Route = createFileRoute("/admin/settings")({
  head: () => ({ meta: [{ title: pageTitle(msg`Instance settings`) }] }),
  beforeLoad: async ({ context, location }) => {
    await requireAuth(context.queryClient, location.href);
  },
  loader: ({ context }) => {
    void context.queryClient.ensureQueryData(adminSettingsOptions());
  },
  component: AdminSettingsPage,
});

function AdminSettingsPage() {
  const { data: me } = useMe();
  const { data: settings, isLoading } = useAdminSettings();

  if (me && !me.is_admin) {
    return (
      <main className="flex min-h-svh flex-col items-center justify-center gap-3 p-6">
        <p className="text-muted-foreground text-sm">
          <Trans>Admin access required.</Trans>
        </p>
        <Button variant="outline" render={<Link to="/drive" />}>
          <Trans>Back to Drive</Trans>
        </Button>
      </main>
    );
  }

  return (
    <main className="bg-background text-foreground flex min-h-svh flex-col">
      <AdminHeader
        title={<Trans>Instance settings</Trans>}
        subtitle={<Trans>Operational policies and configuration across this install</Trans>}
        // eslint-disable-next-line lingui/no-unlocalized-strings -- admin tab identifier
        activeTab="settings"
      />

      <div className="mx-auto flex w-full max-w-4xl flex-col gap-6 p-4 md:p-6">
        {isLoading || !settings ? (
          <p className="text-muted-foreground text-sm">
            <Trans>Loading settings…</Trans>
          </p>
        ) : (
          <SettingsForm settings={settings} />
        )}
      </div>
    </main>
  );
}

function SourceBadge({ source }: { source: AdminSetting["source"] }) {
  if (source === "database") {
    return (
      <Badge variant="default">
        <Trans>Database override</Trans>
      </Badge>
    );
  }
  if (source === "env") {
    return (
      <Badge variant="secondary">
        <Trans>Environment variable</Trans>
      </Badge>
    );
  }
  return (
    <Badge variant="outline">
      <Trans>Default</Trans>
    </Badge>
  );
}

function SettingsForm({ settings }: { settings: AdminSettingsMap }) {
  const { t } = useLingui();
  const update = useUpdateAdminSettings();
  const remove = useDeleteAdminSetting();
  // eslint-disable-next-line lingui/no-unlocalized-strings -- default instance name
  const defaultInstanceName = "pp-hub";
  const currentInstanceName = (settings.instance_name?.value as string) ?? defaultInstanceName;
  const currentMaxUpload = (settings.max_upload_bytes?.value as number) ?? 0;
  const currentRetention = (settings.audit_retention_days?.value as number) ?? 365;
  const currentQuota = (settings.default_space_quota_bytes?.value as number) ?? 0;
  const [instanceName, setInstanceName] = useState(currentInstanceName);
  const [maxUploadGb, setMaxUploadGb] = useState(
    currentMaxUpload > 0 ? String(Math.round(currentMaxUpload / 1024 ** 3)) : "0",
  );
  const [auditRetentionDays, setAuditRetentionDays] = useState(String(currentRetention));
  const [defaultSpaceQuotaGb, setDefaultSpaceQuotaGb] = useState(
    currentQuota > 0 ? String(Math.round(currentQuota / 1024 ** 3)) : "0",
  );

  const initialMaxUploadGb =
    currentMaxUpload > 0 ? String(Math.round(currentMaxUpload / 1024 ** 3)) : "0";
  const initialQuotaGb = currentQuota > 0 ? String(Math.round(currentQuota / 1024 ** 3)) : "0";

  const isDirty =
    instanceName !== currentInstanceName ||
    maxUploadGb !== initialMaxUploadGb ||
    auditRetentionDays !== String(currentRetention) ||
    defaultSpaceQuotaGb !== initialQuotaGb;

  const handleSave = async (e: React.FormEvent) => {
    e.preventDefault();

    const patch: Record<string, unknown> = {};

    if (instanceName !== currentInstanceName) {
      const trimmed = instanceName.trim();
      if (!trimmed) {
        toast.error(t`Instance name cannot be empty`);
        return;
      }
      patch.instance_name = trimmed;
    }

    if (maxUploadGb !== initialMaxUploadGb) {
      const parsedGb = Number.parseFloat(maxUploadGb);
      if (Number.isNaN(parsedGb) || parsedGb < 0) {
        toast.error(t`Maximum upload size must be a valid number (0 for unlimited)`);
        return;
      }
      patch.max_upload_bytes = Math.round(parsedGb * 1024 ** 3);
    }

    if (auditRetentionDays !== String(currentRetention)) {
      const parsedDays = Number.parseInt(auditRetentionDays, 10);
      if (Number.isNaN(parsedDays) || parsedDays < 1 || parsedDays > 3650) {
        toast.error(t`Audit retention must be between 1 and 3650 days`);
        return;
      }
      patch.audit_retention_days = parsedDays;
    }

    if (defaultSpaceQuotaGb !== initialQuotaGb) {
      const parsedQuota = Number.parseFloat(defaultSpaceQuotaGb);
      if (Number.isNaN(parsedQuota) || parsedQuota < 0) {
        toast.error(t`Default quota must be a valid number (0 for unlimited)`);
        return;
      }
      patch.default_space_quota_bytes = Math.round(parsedQuota * 1024 ** 3);
    }

    if (Object.keys(patch).length === 0) return;

    try {
      await update.mutateAsync(patch);
      toast.success(t`Settings saved`);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t`Failed to save settings`);
    }
  };

  const handleReset = async (key: string) => {
    try {
      await remove.mutateAsync(key);
      toast.success(t`Setting reverted to deployment default`);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t`Failed to reset setting`);
    }
  };

  return (
    <form onSubmit={handleSave} className="flex flex-col gap-6">
      <section className="flex flex-col gap-3">
        <SectionLabel>
          <Trans>General & Branding</Trans>
        </SectionLabel>
        <Card>
          <CardHeader>
            <div className="flex items-center gap-2">
              <CardTitle>
                <Trans>Instance display name</Trans>
              </CardTitle>
              {settings.instance_name && <SourceBadge source={settings.instance_name.source} />}
            </div>
            <CardDescription>{settings.instance_name?.description}</CardDescription>
            {settings.instance_name?.source === "database" && (
              <CardAction>
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  // eslint-disable-next-line lingui/no-unlocalized-strings -- setting key
                  onClick={() => handleReset("instance_name")}
                  disabled={remove.isPending}
                >
                  <RotateCcw className="size-3.5" />
                  <Trans>Reset</Trans>
                </Button>
              </CardAction>
            )}
          </CardHeader>
          <CardContent>
            <div className="flex max-w-sm flex-col gap-1.5">
              <Label htmlFor="instance_name">
                <Trans>Display name</Trans>
              </Label>
              <Input
                id="instance_name"
                value={instanceName}
                onChange={(e) => setInstanceName(e.target.value)}
                maxLength={64}
                placeholder={defaultInstanceName}
              />
            </div>
          </CardContent>
        </Card>
      </section>

      <section className="flex flex-col gap-3">
        <SectionLabel>
          <Trans>Storage & Uploads</Trans>
        </SectionLabel>
        <div className="flex flex-col gap-4">
          <Card>
            <CardHeader>
              <div className="flex items-center gap-2">
                <CardTitle>
                  <Trans>Maximum upload size</Trans>
                </CardTitle>
                {settings.max_upload_bytes && (
                  <SourceBadge source={settings.max_upload_bytes.source} />
                )}
              </div>
              <CardDescription>{settings.max_upload_bytes?.description}</CardDescription>
              {settings.max_upload_bytes?.source === "database" && (
                <CardAction>
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    // eslint-disable-next-line lingui/no-unlocalized-strings -- setting key
                    onClick={() => handleReset("max_upload_bytes")}
                    disabled={remove.isPending}
                  >
                    <RotateCcw className="size-3.5" />
                    <Trans>Reset</Trans>
                  </Button>
                </CardAction>
              )}
            </CardHeader>
            <CardContent>
              <div className="flex max-w-sm flex-col gap-1.5">
                <Label htmlFor="max_upload_bytes">
                  <Trans>Maximum size (GB, 0 = unlimited)</Trans>
                </Label>
                <div className="flex items-center gap-2">
                  <Input
                    id="max_upload_bytes"
                    type="number"
                    min="0"
                    step="0.5"
                    value={maxUploadGb}
                    onChange={(e) => setMaxUploadGb(e.target.value)}
                  />
                  <span className="text-muted-foreground text-sm">
                    <Trans>GB</Trans>
                  </span>
                </div>
              </div>
            </CardContent>
          </Card>

          <Card>
            <CardHeader>
              <div className="flex items-center gap-2">
                <CardTitle>
                  <Trans>Default space quota</Trans>
                </CardTitle>
                {settings.default_space_quota_bytes && (
                  <SourceBadge source={settings.default_space_quota_bytes.source} />
                )}
              </div>
              <CardDescription>{settings.default_space_quota_bytes?.description}</CardDescription>
              {settings.default_space_quota_bytes?.source === "database" && (
                <CardAction>
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    // eslint-disable-next-line lingui/no-unlocalized-strings -- setting key
                    onClick={() => handleReset("default_space_quota_bytes")}
                    disabled={remove.isPending}
                  >
                    <RotateCcw className="size-3.5" />
                    <Trans>Reset</Trans>
                  </Button>
                </CardAction>
              )}
            </CardHeader>
            <CardContent>
              <div className="flex max-w-sm flex-col gap-1.5">
                <Label htmlFor="default_space_quota_bytes">
                  <Trans>Default quota for new spaces (GB, 0 = unlimited)</Trans>
                </Label>
                <div className="flex items-center gap-2">
                  <Input
                    id="default_space_quota_bytes"
                    type="number"
                    min="0"
                    step="1"
                    value={defaultSpaceQuotaGb}
                    onChange={(e) => setDefaultSpaceQuotaGb(e.target.value)}
                  />
                  <span className="text-muted-foreground text-sm">
                    <Trans>GB</Trans>
                  </span>
                </div>
              </div>
            </CardContent>
          </Card>
        </div>
      </section>

      <section className="flex flex-col gap-3">
        <SectionLabel>
          <Trans>Security & Auditing</Trans>
        </SectionLabel>
        <Card>
          <CardHeader>
            <div className="flex items-center gap-2">
              <CardTitle>
                <Trans>Security log retention</Trans>
              </CardTitle>
              {settings.audit_retention_days && (
                <SourceBadge source={settings.audit_retention_days.source} />
              )}
            </div>
            <CardDescription>{settings.audit_retention_days?.description}</CardDescription>
            {settings.audit_retention_days?.source === "database" && (
              <CardAction>
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  // eslint-disable-next-line lingui/no-unlocalized-strings -- setting key
                  onClick={() => handleReset("audit_retention_days")}
                  disabled={remove.isPending}
                >
                  <RotateCcw className="size-3.5" />
                  <Trans>Reset</Trans>
                </Button>
              </CardAction>
            )}
          </CardHeader>
          <CardContent>
            <div className="flex max-w-sm flex-col gap-1.5">
              <Label htmlFor="audit_retention_days">
                <Trans>Retention period (days)</Trans>
              </Label>
              <div className="flex items-center gap-2">
                <Input
                  id="audit_retention_days"
                  type="number"
                  min="1"
                  max="3650"
                  value={auditRetentionDays}
                  onChange={(e) => setAuditRetentionDays(e.target.value)}
                />
                <span className="text-muted-foreground text-sm">
                  <Trans>days</Trans>
                </span>
              </div>
            </div>
          </CardContent>
        </Card>
      </section>

      <div className="flex items-center justify-end gap-3 border-t pt-4">
        <Button
          type="button"
          variant="outline"
          disabled={!isDirty || update.isPending}
          onClick={() => {
            setInstanceName(currentInstanceName);
            setMaxUploadGb(initialMaxUploadGb);
            setAuditRetentionDays(String(currentRetention));
            setDefaultSpaceQuotaGb(initialQuotaGb);
          }}
        >
          <Trans>Discard</Trans>
        </Button>
        <Button type="submit" disabled={!isDirty || update.isPending}>
          <Save className="size-4" />
          <Trans>Save changes</Trans>
        </Button>
      </div>
    </form>
  );
}
