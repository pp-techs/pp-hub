import { createFileRoute, Link } from "@tanstack/react-router";
import { useState } from "react";
import { toast } from "sonner";
import { Trans, useLingui } from "@lingui/react/macro";
import { msg } from "@lingui/core/macro";
import type { MessageDescriptor } from "@lingui/core";
import { RotateCcw, Save, Send } from "lucide-react";
import { Button } from "@workspace/ui/components/button";
import { Input } from "@workspace/ui/components/input";
import { Label } from "@workspace/ui/components/label";
import { Badge } from "@workspace/ui/components/badge";
import { Skeleton } from "@workspace/ui/components/skeleton";
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@workspace/ui/components/card";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@workspace/ui/components/select";
import { AdminHeader } from "@/components/admin-header";
import { SectionLabel } from "@/components/section-label";
import { LoadingStatus } from "@/components/loading";
import {
  adminSettingsOptions,
  requireAuth,
  useAdminSettings,
  useDeleteAdminSetting,
  useMe,
  useSendTestEmail,
  useUpdateAdminSettings,
} from "@/lib/queries";
import { pageTitle } from "@/lib/i18n";
import type { AdminSetting, AdminSettingsMap, SmtpSettingValue } from "@/lib/api";

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
          <SettingsSkeleton />
        ) : (
          <>
            <SettingsForm settings={settings} />
            <MailSettingsSection
              key={JSON.stringify(settings.smtp?.value)}
              setting={settings.smtp}
            />
          </>
        )}
      </div>
    </main>
  );
}

/**
 * The page's opening sections as they will land: a section label over setting
 * cards, each a title, a description and one input. Real `Card`s, so the
 * padding and borders are the ones the form renders with.
 */
function SettingsSkeleton() {
  return (
    <LoadingStatus className="flex flex-col gap-6">
      {[1, 2].map((cards, s) => (
        <section key={s} className="flex flex-col gap-3">
          <Skeleton className="mb-1 h-3.5 w-32" />
          <div className="flex flex-col gap-4">
            {Array.from({ length: cards }, (_, c) => (
              <Card key={c}>
                <CardHeader>
                  <Skeleton className="h-4 w-40" />
                  <Skeleton className="h-3.5 w-3/4" />
                </CardHeader>
                <CardContent>
                  <div className="flex max-w-sm flex-col gap-1.5">
                    <Skeleton className="h-3 w-28" />
                    <Skeleton className="h-7 w-full" />
                  </div>
                </CardContent>
              </Card>
            ))}
          </div>
        </section>
      ))}
    </LoadingStatus>
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

/**
 * The integer settings this form edits in days or counts, with the default
 * shown when the server omits one and the bounds the server enforces. The
 * bounds are mirrored here only to refuse an obviously bad value before the
 * round trip; the server's check is the one that counts.
 */
const INT_SETTINGS = [
  { key: "audit_retention_days", fallback: 365, min: 1, max: 3650 },
  { key: "read_audit_retention_days", fallback: 90, min: 1, max: 3650 },
  { key: "alert_failed_login_threshold", fallback: 10, min: 0, max: 10000 },
  { key: "alert_failed_login_window_minutes", fallback: 15, min: 1, max: 1440 },
  { key: "version_retention_count", fallback: 100, min: 0, max: 10000 },
] as const;

type IntSettingKey = (typeof INT_SETTINGS)[number]["key"];
type IntSettingValues = Record<IntSettingKey, string>;

/**
 * One integer setting: where its value comes from, a Reset back to the
 * deployment default when it is a database override, and the input itself.
 */
function IntSettingCard({
  settingKey,
  settings,
  values,
  title,
  label,
  suffix,
  hint,
  onChange,
  onReset,
  resetDisabled,
}: {
  settingKey: IntSettingKey;
  settings: AdminSettingsMap;
  values: IntSettingValues;
  title: React.ReactNode;
  label: React.ReactNode;
  suffix?: React.ReactNode;
  hint?: React.ReactNode;
  onChange: (key: IntSettingKey, value: string) => void;
  onReset: (key: IntSettingKey) => void;
  resetDisabled: boolean;
}) {
  const setting = settings[settingKey];
  const spec = INT_SETTINGS.find((s) => s.key === settingKey);
  return (
    <Card>
      <CardHeader>
        <div className="flex items-center gap-2">
          <CardTitle>{title}</CardTitle>
          {setting && <SourceBadge source={setting.source} />}
        </div>
        <CardDescription>{setting?.description}</CardDescription>
        {setting?.source === "database" && (
          <CardAction>
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={() => onReset(settingKey)}
              disabled={resetDisabled}
            >
              <RotateCcw className="size-3.5" />
              <Trans>Reset</Trans>
            </Button>
          </CardAction>
        )}
      </CardHeader>
      <CardContent>
        <div className="flex max-w-sm flex-col gap-1.5">
          <Label htmlFor={settingKey}>{label}</Label>
          <div className="flex items-center gap-2">
            <Input
              id={settingKey}
              type="number"
              min={spec?.min}
              max={spec?.max}
              value={values[settingKey]}
              onChange={(e) => onChange(settingKey, e.target.value)}
            />
            {suffix && <span className="text-muted-foreground text-sm">{suffix}</span>}
          </div>
          {hint && <p className="text-muted-foreground text-xs">{hint}</p>}
        </div>
      </CardContent>
    </Card>
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
  const currentQuota = (settings.default_space_quota_bytes?.value as number) ?? 0;
  const currentInts = Object.fromEntries(
    INT_SETTINGS.map((s) => [s.key, String((settings[s.key]?.value as number) ?? s.fallback)]),
  ) as IntSettingValues;
  const [instanceName, setInstanceName] = useState(currentInstanceName);
  const [maxUploadGb, setMaxUploadGb] = useState(
    currentMaxUpload > 0 ? String(Math.round(currentMaxUpload / 1024 ** 3)) : "0",
  );
  const [ints, setInts] = useState(currentInts);
  const [defaultSpaceQuotaGb, setDefaultSpaceQuotaGb] = useState(
    currentQuota > 0 ? String(Math.round(currentQuota / 1024 ** 3)) : "0",
  );
  const setInt = (key: IntSettingKey, value: string) =>
    setInts((prev) => ({ ...prev, [key]: value }));

  const initialMaxUploadGb =
    currentMaxUpload > 0 ? String(Math.round(currentMaxUpload / 1024 ** 3)) : "0";
  const initialQuotaGb = currentQuota > 0 ? String(Math.round(currentQuota / 1024 ** 3)) : "0";

  const isDirty =
    instanceName !== currentInstanceName ||
    maxUploadGb !== initialMaxUploadGb ||
    INT_SETTINGS.some((s) => ints[s.key] !== currentInts[s.key]) ||
    defaultSpaceQuotaGb !== initialQuotaGb;

  const intErrors: Record<IntSettingKey, string> = {
    audit_retention_days: t`Audit retention must be between 1 and 3650 days`,
    read_audit_retention_days: t`File access retention must be between 1 and 3650 days`,
    alert_failed_login_threshold: t`Alert threshold must be between 0 and 10000`,
    alert_failed_login_window_minutes: t`Alert window must be between 1 and 1440 minutes`,
    version_retention_count: t`Versions kept per file must be between 0 and 10000`,
  };

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

    for (const s of INT_SETTINGS) {
      if (ints[s.key] === currentInts[s.key]) continue;
      const parsed = Number(ints[s.key]);
      if (
        ints[s.key].trim() === "" ||
        !Number.isInteger(parsed) ||
        parsed < s.min ||
        parsed > s.max
      ) {
        toast.error(intErrors[s.key]);
        return;
      }
      patch[s.key] = parsed;
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
      const next = await remove.mutateAsync(key);
      toast.success(t`Setting reverted to deployment default`);
      return next;
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t`Failed to reset setting`);
    }
  };

  // The field still holds the override it was showing; follow the value the
  // reset landed on, or the form reads as dirty with a value nobody saved.
  const resetInt = async (key: IntSettingKey) => {
    const next = await handleReset(key);
    if (next?.[key]) setInt(key, String(next[key].value));
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

          <IntSettingCard
            // eslint-disable-next-line lingui/no-unlocalized-strings -- setting key
            settingKey="version_retention_count"
            settings={settings}
            values={ints}
            title={<Trans>Version history</Trans>}
            label={<Trans>Versions kept per file (0 = keep all)</Trans>}
            hint={
              <Trans>
                Saving a new version removes the oldest ones beyond this number. The current version
                is always kept.
              </Trans>
            }
            onChange={setInt}
            onReset={resetInt}
            resetDisabled={remove.isPending}
          />
        </div>
      </section>

      <section className="flex flex-col gap-3">
        <SectionLabel>
          <Trans>Security & Auditing</Trans>
        </SectionLabel>
        <IntSettingCard
          // eslint-disable-next-line lingui/no-unlocalized-strings -- setting key
          settingKey="audit_retention_days"
          settings={settings}
          values={ints}
          title={<Trans>Security log retention</Trans>}
          label={<Trans>Retention period (days)</Trans>}
          suffix={<Trans>days</Trans>}
          onChange={setInt}
          onReset={resetInt}
          resetDisabled={remove.isPending}
        />
        <IntSettingCard
          // eslint-disable-next-line lingui/no-unlocalized-strings -- setting key
          settingKey="read_audit_retention_days"
          settings={settings}
          values={ints}
          title={<Trans>File access log retention</Trans>}
          label={<Trans>Retention period (days)</Trans>}
          suffix={<Trans>days</Trans>}
          onChange={setInt}
          onReset={resetInt}
          resetDisabled={remove.isPending}
        />
        <IntSettingCard
          // eslint-disable-next-line lingui/no-unlocalized-strings -- setting key
          settingKey="alert_failed_login_threshold"
          settings={settings}
          values={ints}
          title={<Trans>Failed sign-in alert threshold</Trans>}
          label={<Trans>Failed sign-ins from one address</Trans>}
          hint={
            <Trans>
              0 turns alerts off. Alerts are emailed to administrators when mail is configured.
            </Trans>
          }
          onChange={setInt}
          onReset={resetInt}
          resetDisabled={remove.isPending}
        />
        <IntSettingCard
          // eslint-disable-next-line lingui/no-unlocalized-strings -- setting key
          settingKey="alert_failed_login_window_minutes"
          settings={settings}
          values={ints}
          title={<Trans>Alert window</Trans>}
          label={<Trans>Window (minutes)</Trans>}
          suffix={<Trans>minutes</Trans>}
          onChange={setInt}
          onReset={resetInt}
          resetDisabled={remove.isPending}
        />
      </section>

      <div className="flex items-center justify-end gap-3 border-t pt-4">
        <Button
          type="button"
          variant="outline"
          disabled={!isDirty || update.isPending}
          onClick={() => {
            setInstanceName(currentInstanceName);
            setMaxUploadGb(initialMaxUploadGb);
            setInts(currentInts);
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

const TLS_OPTIONS: { label: MessageDescriptor; value: "starttls" | "implicit" | "none" }[] = [
  { label: msg`STARTTLS (port 587)`, value: "starttls" },
  { label: msg`Implicit TLS / SMTPS (port 465)`, value: "implicit" },
  { label: msg`None — local relay only`, value: "none" },
];

// eslint-disable-next-line lingui/no-unlocalized-strings -- setting key and defaults
const SETTING_KEY_SMTP = "smtp";
// eslint-disable-next-line lingui/no-unlocalized-strings -- setting key and defaults
const DEFAULT_TLS = "starttls";
// eslint-disable-next-line lingui/no-unlocalized-strings -- setting key and defaults
const DEFAULT_SOURCE = "default";
// eslint-disable-next-line lingui/no-unlocalized-strings -- placeholder example
const defaultHostPlaceholder = "smtp.example.com";
// eslint-disable-next-line lingui/no-unlocalized-strings -- placeholder example
const defaultFromPlaceholder = "pp-hub <noreply@example.com>";
function MailSettingsSection({ setting }: { setting?: AdminSetting }) {
  const { i18n, t } = useLingui();
  const { data: me } = useMe();
  const update = useUpdateAdminSettings();
  const remove = useDeleteAdminSetting();
  const sendTest = useSendTestEmail();

  const value = (setting?.value as SmtpSettingValue | undefined) ?? {
    host: "",
    port: 587,
    username: "",
    from: "",
    tls: DEFAULT_TLS,
    password_set: false,
  };
  const source = setting?.source ?? DEFAULT_SOURCE;

  const [host, setHost] = useState(value.host ?? "");
  const [port, setPort] = useState(String(value.port || 587));
  const [tls, setTls] = useState<"starttls" | "implicit" | "none">(value.tls || DEFAULT_TLS);
  const [username, setUsername] = useState(value.username ?? "");
  const [password, setPassword] = useState("");
  const [clearPassword, setClearPassword] = useState(false);
  const [from, setFrom] = useState(value.from ?? "");

  const isDirty =
    host !== (value.host ?? "") ||
    port !== String(value.port || 587) ||
    tls !== (value.tls || DEFAULT_TLS) ||
    username !== (value.username ?? "") ||
    password !== "" ||
    clearPassword ||
    from !== (value.from ?? "");

  const handleReset = async () => {
    try {
      await remove.mutateAsync(SETTING_KEY_SMTP);
      toast.success(t`Outgoing mail settings reset`);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t`Failed to reset setting`);
    }
  };

  const handleSave = async (e: React.FormEvent) => {
    e.preventDefault();
    const trimmedHost = host.trim();
    const trimmedFrom = from.trim();
    const portNum = parseInt(port, 10);

    if (trimmedHost !== "") {
      if (!trimmedFrom) {
        toast.error(t`From address is required when an SMTP host is set.`);
        return;
      }
      if (isNaN(portNum) || portNum < 1 || portNum > 65535) {
        toast.error(t`Port must be between 1 and 65535.`);
        return;
      }
    }

    if (source === "env" && value.password_set && username.trim() && !password) {
      toast.error(t`Enter the password: saving here replaces the environment configuration.`);
      return;
    }

    try {
      await update.mutateAsync({
        smtp: {
          host: trimmedHost,
          port: isNaN(portNum) ? 587 : portNum,
          username: username.trim(),
          from: trimmedFrom,
          tls,
          ...(password ? { password } : clearPassword ? { password: "" } : {}),
        },
      });
      toast.success(t`Settings saved`);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t`Failed to save settings`);
    }
  };

  const handleSendTest = async () => {
    try {
      const res = await sendTest.mutateAsync();
      const to = res.to;
      toast.success(t`Test email sent to ${to}`);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t`Failed to send test email`);
    }
  };

  return (
    <form onSubmit={handleSave} className="flex flex-col gap-6">
      <section className="flex flex-col gap-3">
        <SectionLabel>
          <Trans>Mail</Trans>
        </SectionLabel>
        <Card>
          <CardHeader>
            <div className="flex items-center gap-2">
              <CardTitle>
                <Trans>Outgoing mail (SMTP)</Trans>
              </CardTitle>
              {setting && <SourceBadge source={setting.source} />}
            </div>
            <CardDescription>
              <Trans>
                Used to email people when a file or folder is shared with them. Leave the host empty
                to turn mail off.
              </Trans>
            </CardDescription>
            {source === "database" && (
              <CardAction>
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  onClick={handleReset}
                  disabled={remove.isPending}
                >
                  <RotateCcw className="size-3.5" />
                  <Trans>Reset</Trans>
                </Button>
              </CardAction>
            )}
          </CardHeader>
          <CardContent>
            <div className="grid gap-4 sm:grid-cols-2">
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="smtp-host">
                  <Trans>Host</Trans>
                </Label>
                <Input
                  id="smtp-host"
                  placeholder={defaultHostPlaceholder}
                  value={host}
                  onChange={(e) => setHost(e.target.value)}
                />
              </div>

              <div className="flex flex-col gap-1.5">
                <Label htmlFor="smtp-port">
                  <Trans>Port</Trans>
                </Label>
                <Input
                  id="smtp-port"
                  type="number"
                  min="1"
                  max="65535"
                  value={port}
                  onChange={(e) => setPort(e.target.value)}
                />
              </div>

              <div className="flex flex-col gap-1.5">
                <Label htmlFor="smtp-tls">
                  <Trans>Security</Trans>
                </Label>
                <Select
                  value={tls}
                  items={TLS_OPTIONS.map((opt) => ({ value: opt.value, label: i18n._(opt.label) }))}
                  onValueChange={(v) =>
                    setTls((v as "starttls" | "implicit" | "none") ?? "starttls")
                  }
                >
                  <SelectTrigger id="smtp-tls" className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {TLS_OPTIONS.map((opt) => (
                      <SelectItem key={opt.value} value={opt.value}>
                        {i18n._(opt.label)}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>

              <div className="flex flex-col gap-1.5">
                <Label htmlFor="smtp-username">
                  <Trans>Username</Trans>
                </Label>
                <Input
                  id="smtp-username"
                  value={username}
                  onChange={(e) => setUsername(e.target.value)}
                />
              </div>

              <div className="flex flex-col gap-1.5">
                <Label htmlFor="smtp-password">
                  <Trans>Password</Trans>
                </Label>
                <div className="flex items-center gap-2">
                  <Input
                    id="smtp-password"
                    type="password"
                    autoComplete="new-password"
                    placeholder={
                      value.password_set
                        ? clearPassword
                          ? t`Cleared`
                          : t`Stored — leave blank to keep`
                        : t`Not set`
                    }
                    value={password}
                    disabled={clearPassword}
                    onChange={(e) => setPassword(e.target.value)}
                  />
                  {value.password_set && source === "database" && (
                    <Button
                      type="button"
                      variant="ghost"
                      size="sm"
                      onClick={() => {
                        setClearPassword((prev) => !prev);
                        setPassword("");
                      }}
                    >
                      {clearPassword ? (
                        <Trans>Keep password</Trans>
                      ) : (
                        <Trans>Remove password</Trans>
                      )}
                    </Button>
                  )}
                </div>
              </div>

              <div className="flex flex-col gap-1.5">
                <Label htmlFor="smtp-from">
                  <Trans>From address</Trans>
                </Label>
                <Input
                  id="smtp-from"
                  placeholder={defaultFromPlaceholder}
                  value={from}
                  onChange={(e) => setFrom(e.target.value)}
                />
              </div>
            </div>

            <div className="mt-6 flex flex-col gap-1.5 border-t pt-4">
              <div className="flex items-center justify-between">
                <Button
                  type="button"
                  variant="outline"
                  onClick={handleSendTest}
                  disabled={isDirty || !value.host || sendTest.isPending}
                >
                  <Send className="size-4" />
                  <Trans>Send test email</Trans>
                </Button>
                <div className="flex items-center gap-3">
                  <Button
                    type="button"
                    variant="outline"
                    disabled={!isDirty || update.isPending}
                    onClick={() => {
                      setHost(value.host ?? "");
                      setPort(String(value.port || 587));
                      setTls(value.tls || DEFAULT_TLS);
                      setUsername(value.username ?? "");
                      setPassword("");
                      setClearPassword(false);
                      setFrom(value.from ?? "");
                    }}
                  >
                    <Trans>Discard</Trans>
                  </Button>
                  <Button type="submit" disabled={!isDirty || update.isPending}>
                    <Save className="size-4" />
                    <Trans>Save changes</Trans>
                  </Button>
                </div>
              </div>
              {(() => {
                const targetEmail = me?.email ?? "";
                return (
                  <p className="text-muted-foreground text-xs">
                    {t`Sends to ${targetEmail}. Save changes first.`}
                  </p>
                );
              })()}
            </div>
          </CardContent>
        </Card>
      </section>
    </form>
  );
}
