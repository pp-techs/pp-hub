import { createFileRoute } from "@tanstack/react-router";
import { useState } from "react";
import { toast } from "sonner";
import { Trans, useLingui } from "@lingui/react/macro";
import { msg } from "@lingui/core/macro";
import type { MessageDescriptor } from "@lingui/core";
import { Save, Send, ShieldAlert, Trash2 } from "lucide-react";
import { Alert, AlertDescription, AlertTitle } from "@workspace/ui/components/alert";
import { Button } from "@workspace/ui/components/button";
import { Input } from "@workspace/ui/components/input";
import { Textarea } from "@workspace/ui/components/textarea";
import { Label } from "@workspace/ui/components/label";
import { Skeleton } from "@workspace/ui/components/skeleton";
import { Switch } from "@workspace/ui/components/switch";
import { Card } from "@workspace/ui/components/card";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@workspace/ui/components/tabs";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@workspace/ui/components/select";
import { AdminPage } from "@/components/admin-page";
import { SettingRow, SettingsSection, UnitInput } from "@/components/admin-settings-ui";
import { LoadingStatus } from "@/components/loading";
import {
  adminSettingsOptions,
  useAdminSettings,
  useDeleteAdminSetting,
  useArchivedPersonalSpaces,
  useMe,
  usePurgeArchivedPersonalSpace,
  useSendTestEmail,
  useUpdateAdminSettings,
} from "@/lib/queries";
import { pageTitle } from "@/lib/i18n";
import { useFormatters } from "@/lib/format";
import { ConfirmDialog } from "@/components/confirm-dialog";
import type {
  AdminSetting,
  AdminSettingsMap,
  ArchivedPersonalSpace,
  SmtpSettingValue,
} from "@/lib/api";

export const Route = createFileRoute("/admin/settings")({
  head: () => ({ meta: [{ title: pageTitle(msg`Instance settings`) }] }),
  // An unknown or missing tab falls back to the first one rather than failing
  // the route, so a stale link still opens the page.
  validateSearch: (search: Record<string, unknown>): { tab?: SettingsTab } => ({
    tab: SETTINGS_TABS.some((s) => s.value === search.tab)
      ? (search.tab as SettingsTab)
      : undefined,
  }),
  // The `/admin` layout has already authenticated the caller as an admin.
  loader: ({ context }) => {
    void context.queryClient.ensureQueryData(adminSettingsOptions());
  },
  component: AdminSettingsPage,
});

function AdminSettingsPage() {
  const { data: settings, isLoading } = useAdminSettings();
  const { i18n } = useLingui();
  const { tab = SETTINGS_TABS[0].value } = Route.useSearch();
  const navigate = Route.useNavigate();

  return (
    <AdminPage
      title={<Trans>Instance settings</Trans>}
      description={<Trans>Operational policies and configuration across this install</Trans>}
    >
      <Tabs
        value={tab}
        className="gap-6"
        onValueChange={(next) =>
          void navigate({ search: { tab: next as SettingsTab }, replace: true })
        }
      >
        {/* Scrolls sideways on a narrow screen instead of wrapping the tabs
            onto a second line. */}
        <div className="-mx-4 overflow-x-auto px-4 md:mx-0 md:px-0">
          <TabsList variant="line" className="h-9 w-max min-w-full justify-start gap-4 border-b">
            {SETTINGS_TABS.map((s) => (
              <TabsTrigger key={s.value} value={s.value} className="flex-none px-1">
                {i18n._(s.label)}
              </TabsTrigger>
            ))}
          </TabsList>
        </div>

        {isLoading || !settings ? (
          <SettingsSkeleton />
        ) : (
          <>
            <SettingsForm settings={settings} onMailTab={tab === "mail"} />
            {/* Its own form with its own Save, so it stays mounted while another
                tab is open: half-typed credentials survive a glance elsewhere. */}
            <TabsContent value="mail" keepMounted className={PANEL_CLASS}>
              <MailSettingsSection
                key={JSON.stringify(settings.smtp?.value)}
                setting={settings.smtp}
              />
            </TabsContent>
          </>
        )}
      </Tabs>
    </AdminPage>
  );
}

/** The tabs, in the order they read: what the install is, then what it allows. */
const SETTINGS_TABS = [
  { value: "general", label: msg`General` },
  { value: "storage", label: msg`Storage` },
  { value: "security", label: msg`Security` },
  { value: "sharing", label: msg`Sharing & agents` },
  { value: "mail", label: msg`Mail` },
] as const;

type SettingsTab = (typeof SETTINGS_TABS)[number]["value"];

/** A tab's panel: its sections stacked, at the page's text size rather than the tabs' own. */
// eslint-disable-next-line lingui/no-unlocalized-strings -- class names
const PANEL_CLASS = "flex flex-col gap-8 text-sm";

/**
 * The page's opening sections as they will land: a heading over one card whose
 * rows are a title, a description and a control. Real `Card`s, so the padding
 * and borders are the ones the form renders with.
 */
function SettingsSkeleton() {
  return (
    <LoadingStatus className="flex flex-col gap-8">
      {[2, 3].map((rows, s) => (
        <section key={s} className="flex flex-col gap-3">
          <Skeleton className="h-4 w-32" />
          <Card className="gap-0 divide-y py-0">
            {Array.from({ length: rows }, (_, r) => (
              <div key={r} className="flex items-center justify-between gap-8 p-4">
                <div className="flex flex-1 flex-col gap-1.5">
                  <Skeleton className="h-4 w-40" />
                  <Skeleton className="h-3.5 w-3/4" />
                </div>
                <Skeleton className="h-7 w-40" />
              </div>
            ))}
          </Card>
        </section>
      ))}
    </LoadingStatus>
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

/** The on/off settings this form edits; absent from the server means its default. */
/* eslint-disable lingui/no-unlocalized-strings -- setting keys */
const BOOL_SETTINGS = [
  "mcp_enabled",
  "mcp_public_links",
  "mcp_tokens",
  "mcp_external_shares",
  "external_sharing_enabled",
  "guest_access",
  "webdav_enabled",
  "artifacts_enabled",
] as const;

/** The keys the rows below are built from, named once instead of spelled at each use. */
const KEY = {
  versionRetention: "version_retention_count",
  auditRetention: "audit_retention_days",
  readAuditRetention: "read_audit_retention_days",
  alertThreshold: "alert_failed_login_threshold",
  alertWindow: "alert_failed_login_window_minutes",
  externalSharing: "external_sharing_enabled",
  guestAccess: "guest_access",
  mcpEnabled: "mcp_enabled",
  mcpPublicLinks: "mcp_public_links",
  mcpTokens: "mcp_tokens",
  mcpExternalShares: "mcp_external_shares",
  webdavEnabled: "webdav_enabled",
  artifactsEnabled: "artifacts_enabled",
  artifactCdnAllowlist: "artifact_cdn_allowlist",
  instanceName: "instance_name",
} as const;
/* eslint-enable lingui/no-unlocalized-strings */

type BoolSettingKey = (typeof BOOL_SETTINGS)[number];
type BoolSettingValues = Record<BoolSettingKey, boolean>;

/** What the server assumes when a boolean key has never been stored. */
const BOOL_DEFAULTS: BoolSettingValues = {
  mcp_enabled: false,
  mcp_public_links: false,
  mcp_tokens: true,
  mcp_external_shares: false,
  external_sharing_enabled: false,
  guest_access: false,
  webdav_enabled: false,
  artifacts_enabled: false,
};

/** Who is given a personal space; the server's `personal_spaces` values. */
type PersonalSpacesMode = "off" | "members" | "everyone";

function personalModeOf(value: unknown): PersonalSpacesMode {
  // eslint-disable-next-line lingui/no-unlocalized-strings -- setting values
  return value === "members" || value === "everyone" ? value : "off";
}

/** Bytes as the whole-GB string the quota inputs hold; "0" for unlimited. */
function gbOf(bytes: number): string {
  return bytes > 0 ? String(Math.round(bytes / 1024 ** 3)) : "0";
}

/**
 * One switch row of a boolean setting: its source, a Reset when it is a
 * database override, and the switch itself.
 */
function BoolSettingRow({
  settingKey,
  settings,
  values,
  label,
  disabled,
  onChange,
  onReset,
  resetDisabled,
}: {
  settingKey: BoolSettingKey;
  settings: AdminSettingsMap;
  values: BoolSettingValues;
  label: React.ReactNode;
  disabled?: boolean;
  onChange: (key: BoolSettingKey, value: boolean) => void;
  onReset: (key: BoolSettingKey) => void;
  resetDisabled: boolean;
}) {
  const setting = settings[settingKey];
  return (
    <SettingRow
      title={label}
      htmlFor={settingKey}
      description={setting?.description}
      source={setting?.source}
      onReset={setting?.source === "database" ? () => onReset(settingKey) : undefined}
      resetDisabled={resetDisabled}
    >
      <Switch
        id={settingKey}
        checked={values[settingKey]}
        disabled={disabled}
        onCheckedChange={(v) => onChange(settingKey, v)}
      />
    </SettingRow>
  );
}

/**
 * One integer setting: where its value comes from, a Reset back to the
 * deployment default when it is a database override, and the input itself.
 */
function IntSettingRow({
  settingKey,
  settings,
  values,
  title,
  unit,
  hint,
  onChange,
  onReset,
  resetDisabled,
}: {
  settingKey: IntSettingKey;
  settings: AdminSettingsMap;
  values: IntSettingValues;
  title: React.ReactNode;
  unit?: React.ReactNode;
  hint?: React.ReactNode;
  onChange: (key: IntSettingKey, value: string) => void;
  onReset: (key: IntSettingKey) => void;
  resetDisabled: boolean;
}) {
  const setting = settings[settingKey];
  const spec = INT_SETTINGS.find((s) => s.key === settingKey);
  return (
    <SettingRow
      title={title}
      htmlFor={settingKey}
      description={
        <>
          {setting?.description} {hint}
        </>
      }
      source={setting?.source}
      onReset={setting?.source === "database" ? () => onReset(settingKey) : undefined}
      resetDisabled={resetDisabled}
    >
      <UnitInput
        id={settingKey}
        min={spec?.min}
        max={spec?.max}
        value={values[settingKey]}
        onChange={(e) => onChange(settingKey, e.target.value)}
        unit={unit}
      />
    </SettingRow>
  );
}

/**
 * Personal spaces whose owner was deleted at the identity provider. Nobody
 * can open them any more; they hold storage until an administrator purges
 * them, which is permanent — hence the confirmation.
 */
function ArchivedPersonalSpacesRow() {
  const { t } = useLingui();
  const f = useFormatters();
  const archived = useArchivedPersonalSpaces();
  const purge = usePurgeArchivedPersonalSpace();
  const [pending, setPending] = useState<ArchivedPersonalSpace | null>(null);
  const pendingOwner = pending?.owner_email ?? "";
  const pendingSize = f.size(pending?.used_bytes ?? 0);

  return (
    <div className="flex flex-col gap-3 p-4">
      <div className="flex flex-col gap-1">
        <span className="text-sm font-medium">
          <Trans>Archived personal spaces</Trans>
        </span>
        <p className="text-muted-foreground max-w-prose text-xs/relaxed">
          <Trans>
            Personal spaces of deleted accounts. Nobody can open them; purge one to free its
            storage.
          </Trans>
        </p>
      </div>
      {archived.isPending ? (
        <LoadingStatus className="flex flex-col gap-2">
          <Skeleton className="h-12 w-full" />
          <Skeleton className="h-12 w-full" />
        </LoadingStatus>
      ) : archived.isError ? (
        <p className="text-destructive text-sm">{archived.error.message}</p>
      ) : archived.data.length === 0 ? (
        <p className="text-muted-foreground bg-muted/40 rounded-lg px-3 py-4 text-center text-xs">
          <Trans>No archived personal spaces.</Trans>
        </p>
      ) : (
        <ul className="divide-y rounded-lg border">
          {archived.data.map((s) => {
            const size = f.size(s.used_bytes);
            const archivedOn = f.date(s.archived_at);
            return (
              <li key={s.id} className="flex items-center gap-3 p-3 text-sm">
                <div className="min-w-0 flex-1">
                  <p className="truncate font-medium">{s.owner_email || s.name}</p>
                  <p className="text-muted-foreground text-xs tabular-nums">
                    <Trans>
                      {size} · archived {archivedOn}
                    </Trans>
                  </p>
                </div>
                <Button
                  type="button"
                  variant="destructive"
                  size="sm"
                  disabled={purge.isPending}
                  onClick={() => setPending(s)}
                >
                  <Trash2 className="size-3.5" />
                  <Trans>Purge</Trans>
                </Button>
              </li>
            );
          })}
        </ul>
      )}
      <ConfirmDialog
        open={pending !== null}
        onOpenChange={(open) => !open && setPending(null)}
        title={t`Purge this personal space?`}
        description={t`Everything in ${pendingOwner}'s personal space (${pendingSize}) is deleted permanently. This cannot be undone.`}
        confirmLabel={t`Purge permanently`}
        onConfirm={() => {
          if (!pending) return;
          purge.mutate(pending.id, {
            onSuccess: () => toast.success(t`Personal space purged`),
            onError: (e) => toast.error(e.message || t`Could not purge the personal space`),
          });
          setPending(null);
        }}
      />
    </div>
  );
}

function SettingsForm({ settings, onMailTab }: { settings: AdminSettingsMap; onMailTab: boolean }) {
  const { t } = useLingui();
  const { data: me } = useMe();
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
  const currentBools = Object.fromEntries(
    BOOL_SETTINGS.map((k) => [k, (settings[k]?.value as boolean | undefined) ?? BOOL_DEFAULTS[k]]),
  ) as BoolSettingValues;
  const [bools, setBools] = useState(currentBools);
  const setBool = (key: BoolSettingKey, value: boolean) =>
    setBools((prev) => ({ ...prev, [key]: value }));
  const [instanceName, setInstanceName] = useState(currentInstanceName);
  // Space-separated origins. Absent means the server's default list, so the
  // field shows what the server reports rather than a client-side copy of it.
  const currentCdnAllowlist = (settings.artifact_cdn_allowlist?.value as string | undefined) ?? "";
  const [cdnAllowlist, setCdnAllowlist] = useState(currentCdnAllowlist);
  const [maxUploadGb, setMaxUploadGb] = useState(
    currentMaxUpload > 0 ? String(Math.round(currentMaxUpload / 1024 ** 3)) : "0",
  );
  const [ints, setInts] = useState(currentInts);
  const [defaultSpaceQuotaGb, setDefaultSpaceQuotaGb] = useState(
    currentQuota > 0 ? String(Math.round(currentQuota / 1024 ** 3)) : "0",
  );
  const setInt = (key: IntSettingKey, value: string) =>
    setInts((prev) => ({ ...prev, [key]: value }));
  const currentPersonalSpaces = personalModeOf(settings.personal_spaces?.value);
  const [personalSpaces, setPersonalSpaces] = useState(currentPersonalSpaces);
  const initialPersonalQuotaGb = gbOf(
    (settings.default_personal_quota_bytes?.value as number) ?? 0,
  );
  const [personalQuotaGb, setPersonalQuotaGb] = useState(initialPersonalQuotaGb);

  const initialMaxUploadGb =
    currentMaxUpload > 0 ? String(Math.round(currentMaxUpload / 1024 ** 3)) : "0";
  const initialQuotaGb = currentQuota > 0 ? String(Math.round(currentQuota / 1024 ** 3)) : "0";

  const isDirty =
    instanceName !== currentInstanceName ||
    cdnAllowlist !== currentCdnAllowlist ||
    maxUploadGb !== initialMaxUploadGb ||
    INT_SETTINGS.some((s) => ints[s.key] !== currentInts[s.key]) ||
    BOOL_SETTINGS.some((k) => bools[k] !== currentBools[k]) ||
    defaultSpaceQuotaGb !== initialQuotaGb ||
    personalSpaces !== currentPersonalSpaces ||
    personalQuotaGb !== initialPersonalQuotaGb;

  const personalOptions: { value: PersonalSpacesMode; label: string; description: string }[] = [
    {
      // eslint-disable-next-line lingui/no-unlocalized-strings -- setting value
      value: "off",
      label: t`Off`,
      description: t`Nobody gets a personal space. Existing ones stay as they are.`,
    },
    {
      // eslint-disable-next-line lingui/no-unlocalized-strings -- setting value
      value: "members",
      label: t`Members of an organization`,
      description: t`Everyone who belongs to at least one organization space gets their own drive.`,
    },
    {
      // eslint-disable-next-line lingui/no-unlocalized-strings -- setting value
      value: "everyone",
      label: t`Everyone`,
      description: t`Every account gets its own drive, including people who belong to no organization.`,
    },
  ];

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

    if (cdnAllowlist !== currentCdnAllowlist) {
      // One space between origins, whatever was typed or pasted between them.
      const origins = cdnAllowlist.split(/\s+/).filter(Boolean);
      const bad = origins.find((o) => !/^https:\/\/[^/\s]+$/.test(o));
      if (bad) {
        toast.error(t`Each CDN must be an https:// origin with no path: ${bad}`);
        return;
      }
      patch.artifact_cdn_allowlist = origins.join(" ");
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

    for (const k of BOOL_SETTINGS) {
      if (bools[k] !== currentBools[k]) patch[k] = bools[k];
    }

    if (defaultSpaceQuotaGb !== initialQuotaGb) {
      const parsedQuota = Number.parseFloat(defaultSpaceQuotaGb);
      if (Number.isNaN(parsedQuota) || parsedQuota < 0) {
        toast.error(t`Default quota must be a valid number (0 for unlimited)`);
        return;
      }
      patch.default_space_quota_bytes = Math.round(parsedQuota * 1024 ** 3);
    }

    if (personalSpaces !== currentPersonalSpaces) patch.personal_spaces = personalSpaces;

    if (personalQuotaGb !== initialPersonalQuotaGb) {
      const parsed = Number.parseFloat(personalQuotaGb);
      if (Number.isNaN(parsed) || parsed < 0) {
        toast.error(t`Personal space quota must be a valid number (0 for unlimited)`);
        return;
      }
      patch.default_personal_quota_bytes = Math.round(parsed * 1024 ** 3);
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

  const resetBool = async (key: BoolSettingKey) => {
    const next = await handleReset(key);
    if (next) setBool(key, (next[key]?.value as boolean | undefined) ?? BOOL_DEFAULTS[key]);
  };

  const resetPersonalSpaces = async () => {
    // eslint-disable-next-line lingui/no-unlocalized-strings -- setting key
    const next = await handleReset("personal_spaces");
    if (next) setPersonalSpaces(personalModeOf(next.personal_spaces?.value));
  };

  const resetPersonalQuota = async () => {
    // eslint-disable-next-line lingui/no-unlocalized-strings -- setting key
    const next = await handleReset("default_personal_quota_bytes");
    if (next) setPersonalQuotaGb(gbOf((next.default_personal_quota_bytes?.value as number) ?? 0));
  };

  const resetCdnAllowlist = async () => {
    const next = await handleReset(KEY.artifactCdnAllowlist);
    if (next) setCdnAllowlist((next.artifact_cdn_allowlist?.value as string | undefined) ?? "");
  };

  const resetPending = remove.isPending;
  const intRow = (
    key: IntSettingKey,
    title: React.ReactNode,
    extra?: { unit?: React.ReactNode; hint?: React.ReactNode },
  ) => (
    <IntSettingRow
      settingKey={key}
      settings={settings}
      values={ints}
      title={title}
      unit={extra?.unit}
      hint={extra?.hint}
      onChange={setInt}
      onReset={resetInt}
      resetDisabled={resetPending}
    />
  );
  const boolRow = (key: BoolSettingKey, label: React.ReactNode, disabled?: boolean) => (
    <BoolSettingRow
      settingKey={key}
      settings={settings}
      values={bools}
      label={label}
      disabled={disabled}
      onChange={setBool}
      onReset={resetBool}
      resetDisabled={resetPending}
    />
  );

  return (
    <form onSubmit={handleSave} className="flex flex-col gap-8">
      <TabsContent value="general" className={PANEL_CLASS}>
        <SettingsSection title={<Trans>General & Branding</Trans>}>
          <SettingRow
            title={<Trans>Instance display name</Trans>}
            htmlFor="instance_name"
            description={settings.instance_name?.description}
            source={settings.instance_name?.source}
            onReset={
              settings.instance_name?.source === "database"
                ? () => void handleReset(KEY.instanceName)
                : undefined
            }
            resetDisabled={resetPending}
          >
            <Input
              id="instance_name"
              className="w-full sm:w-56"
              value={instanceName}
              onChange={(e) => setInstanceName(e.target.value)}
              maxLength={64}
              placeholder={defaultInstanceName}
            />
          </SettingRow>
        </SettingsSection>
        <SettingsSection title={<Trans>Personal use</Trans>}>
          <SettingRow
            title={<Trans>Personal spaces</Trans>}
            htmlFor="personal_spaces"
            description={
              <>
                {settings.personal_spaces?.description}{" "}
                {personalOptions.find((o) => o.value === personalSpaces)?.description}
              </>
            }
            source={settings.personal_spaces?.source}
            onReset={
              settings.personal_spaces?.source === "database"
                ? () => void resetPersonalSpaces()
                : undefined
            }
            resetDisabled={resetPending}
          >
            <Select
              value={personalSpaces}
              items={personalOptions.map((o) => ({ value: o.value, label: o.label }))}
              onValueChange={(v) => setPersonalSpaces((v as PersonalSpacesMode) ?? "off")}
            >
              <SelectTrigger id="personal_spaces" className="w-full sm:w-56">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {personalOptions.map((o) => (
                  <SelectItem key={o.value} value={o.value}>
                    {o.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </SettingRow>

          <SettingRow
            title={<Trans>Personal space quota</Trans>}
            htmlFor="default_personal_quota_bytes"
            description={
              <>
                {settings.default_personal_quota_bytes?.description}{" "}
                <Trans>
                  0 means unlimited. Offering personal spaces to everyone requires a quota above 0.
                </Trans>
              </>
            }
            source={settings.default_personal_quota_bytes?.source}
            onReset={
              settings.default_personal_quota_bytes?.source === "database"
                ? () => void resetPersonalQuota()
                : undefined
            }
            resetDisabled={resetPending}
          >
            <UnitInput
              id="default_personal_quota_bytes"
              min="0"
              step="1"
              value={personalQuotaGb}
              onChange={(e) => setPersonalQuotaGb(e.target.value)}
              unit={<Trans>GB</Trans>}
            />
          </SettingRow>

          <ArchivedPersonalSpacesRow />
        </SettingsSection>
      </TabsContent>

      <TabsContent value="storage" className={PANEL_CLASS}>
        <SettingsSection title={<Trans>Storage & Uploads</Trans>}>
          <SettingRow
            title={<Trans>Maximum upload size</Trans>}
            htmlFor="max_upload_bytes"
            description={settings.max_upload_bytes?.description}
            source={settings.max_upload_bytes?.source}
            onReset={
              settings.max_upload_bytes?.source === "database"
                ? // eslint-disable-next-line lingui/no-unlocalized-strings -- setting key
                  () => void handleReset("max_upload_bytes")
                : undefined
            }
            resetDisabled={resetPending}
          >
            <UnitInput
              id="max_upload_bytes"
              min="0"
              step="0.5"
              value={maxUploadGb}
              onChange={(e) => setMaxUploadGb(e.target.value)}
              unit={<Trans>GB</Trans>}
            />
          </SettingRow>

          <SettingRow
            title={<Trans>Default space quota</Trans>}
            htmlFor="default_space_quota_bytes"
            description={
              <>
                {settings.default_space_quota_bytes?.description} <Trans>0 means unlimited.</Trans>
              </>
            }
            source={settings.default_space_quota_bytes?.source}
            onReset={
              settings.default_space_quota_bytes?.source === "database"
                ? // eslint-disable-next-line lingui/no-unlocalized-strings -- setting key
                  () => void handleReset("default_space_quota_bytes")
                : undefined
            }
            resetDisabled={resetPending}
          >
            <UnitInput
              id="default_space_quota_bytes"
              min="0"
              step="1"
              value={defaultSpaceQuotaGb}
              onChange={(e) => setDefaultSpaceQuotaGb(e.target.value)}
              unit={<Trans>GB</Trans>}
            />
          </SettingRow>

          {intRow(KEY.versionRetention, <Trans>Version history</Trans>, {
            unit: <Trans>versions</Trans>,
            hint: (
              <Trans>
                Versions kept per file; 0 keeps all. Saving a new version removes the oldest ones
                beyond this number. The current version is always kept.
              </Trans>
            ),
          })}
        </SettingsSection>
      </TabsContent>

      <TabsContent value="security" className={PANEL_CLASS}>
        <SettingsSection title={<Trans>Security & Auditing</Trans>}>
          {intRow(KEY.auditRetention, <Trans>Security log retention</Trans>, {
            unit: <Trans>days</Trans>,
          })}
          {intRow(KEY.readAuditRetention, <Trans>File access log retention</Trans>, {
            unit: <Trans>days</Trans>,
          })}
          {intRow(KEY.alertThreshold, <Trans>Failed sign-in alert threshold</Trans>, {
            unit: <Trans>sign-ins</Trans>,
            hint: (
              <Trans>
                Failed sign-ins from one address. 0 turns alerts off. Alerts are emailed to
                administrators when mail is configured.
              </Trans>
            ),
          })}
          {intRow(KEY.alertWindow, <Trans>Alert window</Trans>, {
            unit: <Trans>minutes</Trans>,
          })}
        </SettingsSection>
      </TabsContent>

      <TabsContent value="sharing" className={PANEL_CLASS}>
        <SettingsSection
          title={<Trans>External sharing</Trans>}
          description={
            <Trans>
              Lets people share files and folders by email with anyone, including people outside
              their space or without an account. Each space admin still decides for their space.
              Guest access lets somebody without an account open a view-only share by proving their
              address with a code mailed to it; it needs outgoing mail set up, and only view and
              comment are possible.
            </Trans>
          }
        >
          {boolRow(KEY.externalSharing, <Trans>Allow sharing outside a space</Trans>)}
          {boolRow(
            KEY.guestAccess,
            <Trans>Let people without an account open what was shared</Trans>,
          )}
        </SettingsSection>

        <SettingsSection
          title={<Trans>WebDAV</Trans>}
          description={
            <Trans>
              Lets people mount their drive in Finder, File Explorer or rclone, signing in with a
              personal token made for it. Each person still sees only what they can open.
            </Trans>
          }
        >
          {boolRow(KEY.webdavEnabled, <Trans>Let people mount their drive over WebDAV</Trans>)}
        </SettingsSection>

        <SettingsSection
          title={<Trans>Publishing</Trans>}
          description={
            <Trans>
              Lets people turn an HTML file or a folder into a public web site that anyone with the
              link can open, scripts included. Needs public links allowed in the space.
            </Trans>
          }
        >
          {me && !me.publishing_isolated && (
            <Alert>
              <ShieldAlert />
              <AlertTitle>
                <Trans>Published sites share this address</Trans>
              </AlertTitle>
              <AlertDescription>
                <Trans>
                  Without a separate address for published content (PP_ARTIFACT_URL on the server),
                  sites are served under /a/ here and only the browser's sandbox keeps their scripts
                  away from people's sessions. Set PP_ARTIFACT_URL to a domain of its own before
                  letting untrusted people publish.
                </Trans>
              </AlertDescription>
            </Alert>
          )}
          {boolRow(
            KEY.artifactsEnabled,
            <Trans>Let people publish HTML pages and folders as public sites</Trans>,
          )}
          <SettingRow
            title={<Trans>Allowed CDNs</Trans>}
            htmlFor={KEY.artifactCdnAllowlist}
            description={
              <>
                {settings.artifact_cdn_allowlist?.description}{" "}
                <Trans>
                  The https:// origins published pages may load scripts, styles, images and fonts
                  from, separated by spaces. Leave empty to allow none: pages then load only their
                  own files.
                </Trans>
              </>
            }
            source={settings.artifact_cdn_allowlist?.source}
            onReset={
              settings.artifact_cdn_allowlist?.source === "database"
                ? () => void resetCdnAllowlist()
                : undefined
            }
            resetDisabled={resetPending}
          >
            <Textarea
              id={KEY.artifactCdnAllowlist}
              className="w-full font-mono text-xs sm:w-80"
              rows={4}
              value={cdnAllowlist}
              onChange={(e) => setCdnAllowlist(e.target.value)}
              // eslint-disable-next-line lingui/no-unlocalized-strings -- example origin
              placeholder="https://cdn.jsdelivr.net"
            />
          </SettingRow>
        </SettingsSection>

        <SettingsSection
          title={<Trans>AI agents</Trans>}
          description={
            <Trans>
              Lets people connect Claude, ChatGPT and other AI agents to their account. Public links
              let an agent publish files that anyone with the link can open without signing in.
            </Trans>
          }
        >
          {boolRow(KEY.mcpEnabled, <Trans>Let people connect AI agents to their account</Trans>)}
          {boolRow(
            KEY.mcpPublicLinks,
            <Trans>Let agents create public links</Trans>,
            !bools.mcp_enabled,
          )}
          {boolRow(
            KEY.mcpTokens,
            <Trans>Let people create personal access tokens for agents</Trans>,
            !bools.mcp_enabled,
          )}
          {boolRow(
            KEY.mcpExternalShares,
            <Trans>Let agents share with people outside a space</Trans>,
            !bools.mcp_enabled,
          )}
        </SettingsSection>
      </TabsContent>

      {/* Sticky so the way to save is always in reach of the field being
          edited, and only present once there is something to save. It stays
          on every tab, since a change made on another one is still pending —
          except Mail, whose card has a Save of its own for its own form. */}
      {isDirty && !onMailTab && (
        <div
          role="region"
          aria-label={t`Unsaved changes`}
          className="bg-popover text-popover-foreground sticky bottom-4 z-10 flex items-center justify-between gap-3 rounded-xl p-2 pl-4 shadow-lg ring-1 ring-foreground/10"
        >
          <span className="text-sm font-medium">
            <Trans>You have unsaved changes</Trans>
          </span>
          <div className="flex items-center gap-2">
            <Button
              type="button"
              variant="ghost"
              disabled={update.isPending}
              onClick={() => {
                setInstanceName(currentInstanceName);
                setCdnAllowlist(currentCdnAllowlist);
                setMaxUploadGb(initialMaxUploadGb);
                setInts(currentInts);
                setBools(currentBools);
                setDefaultSpaceQuotaGb(initialQuotaGb);
                setPersonalSpaces(currentPersonalSpaces);
                setPersonalQuotaGb(initialPersonalQuotaGb);
              }}
            >
              <Trans>Discard</Trans>
            </Button>
            <Button type="submit" disabled={update.isPending}>
              <Save className="size-4" />
              <Trans>Save changes</Trans>
            </Button>
          </div>
        </div>
      )}
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

  const targetEmail = me?.email ?? "";

  return (
    <form onSubmit={handleSave}>
      <SettingsSection title={<Trans>Mail</Trans>}>
        <SettingRow
          title={<Trans>Outgoing mail (SMTP)</Trans>}
          description={
            <Trans>
              Used to email people when a file or folder is shared with them. Leave the host empty
              to turn mail off.
            </Trans>
          }
          source={setting?.source}
          onReset={source === "database" ? handleReset : undefined}
          resetDisabled={remove.isPending}
        />

        <div className="grid gap-4 p-4 sm:grid-cols-2">
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="smtp-host" className="text-sm">
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
            <Label htmlFor="smtp-port" className="text-sm">
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
            <Label htmlFor="smtp-tls" className="text-sm">
              <Trans>Security</Trans>
            </Label>
            <Select
              value={tls}
              items={TLS_OPTIONS.map((opt) => ({ value: opt.value, label: i18n._(opt.label) }))}
              onValueChange={(v) => setTls((v as "starttls" | "implicit" | "none") ?? "starttls")}
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
            <Label htmlFor="smtp-from" className="text-sm">
              <Trans>From address</Trans>
            </Label>
            <Input
              id="smtp-from"
              placeholder={defaultFromPlaceholder}
              value={from}
              onChange={(e) => setFrom(e.target.value)}
            />
          </div>

          <div className="flex flex-col gap-1.5">
            <Label htmlFor="smtp-username" className="text-sm">
              <Trans>Username</Trans>
            </Label>
            <Input
              id="smtp-username"
              value={username}
              onChange={(e) => setUsername(e.target.value)}
            />
          </div>

          <div className="flex flex-col gap-1.5">
            <Label htmlFor="smtp-password" className="text-sm">
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
                  {clearPassword ? <Trans>Keep password</Trans> : <Trans>Remove password</Trans>}
                </Button>
              )}
            </div>
          </div>
        </div>

        <div className="bg-muted/30 flex flex-wrap items-center justify-between gap-3 p-3 pl-4">
          <div className="flex flex-col gap-1">
            <Button
              type="button"
              variant="outline"
              onClick={handleSendTest}
              disabled={isDirty || !value.host || sendTest.isPending}
            >
              <Send className="size-4" />
              <Trans>Send test email</Trans>
            </Button>
            <p className="text-muted-foreground text-xs">
              {t`Sends to ${targetEmail}. Save changes first.`}
            </p>
          </div>
          <div className="flex items-center gap-2">
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
      </SettingsSection>
    </form>
  );
}
