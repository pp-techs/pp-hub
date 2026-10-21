import { Trans, useLingui } from "@lingui/react/macro";
import { msg } from "@lingui/core/macro";
import { toast } from "sonner";
import type { MessageDescriptor } from "@lingui/core";
import { useQueryClient } from "@tanstack/react-query";
import { Label } from "@workspace/ui/components/label";
import { Switch } from "@workspace/ui/components/switch";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@workspace/ui/components/select";
import { updatePreferences } from "@/lib/api/users";
import type { NodeView, SortField, SortOrder, UserPreferences, UserProfile } from "@/lib/api";
import { useUiStore, type OfficeOpenIn } from "@/lib/ui-store";
import { LOCALES, type Locale } from "@/lib/i18n";
import { useTheme } from "@/components/theme-provider";
import { useLocale, useSetLocale } from "@/components/locale-provider";
import { keys, useMe } from "@/lib/queries";
import { SectionLabel } from "@/components/section-label";

/**
 * Option labels are message descriptors, not strings. These lists live at
 * module scope, which runs once at import time and before any catalog is
 * active, so a rendered label here would freeze whichever language happened
 * to load first and never follow a switch.
 */
type Choice = { label: MessageDescriptor; value: string };

const THEMES: Choice[] = [
  { label: msg`System`, value: "system" },
  { label: msg`Light`, value: "light" },
  { label: msg`Dark`, value: "dark" },
];

const VIEWS: Choice[] = [
  { label: msg`List`, value: "list" },
  { label: msg`Grid`, value: "grid" },
];

const SORTS: Choice[] = [
  { label: msg`Name`, value: "name" },
  { label: msg`Modified`, value: "modified" },
  { label: msg`Size`, value: "size" },
];

const ORDERS: Choice[] = [
  { label: msg`Ascending`, value: "asc" },
  { label: msg`Descending`, value: "desc" },
];

const OFFICE_OPEN: Choice[] = [
  { label: msg`This tab`, value: "current" },
  { label: msg`A new browser tab`, value: "new-tab" },
];

/**
 * How the app is drawn and read.
 *
 * The three sections differ in where they are stored, and deliberately so.
 * Theme and the default listing stay local to this browser: a theme that
 * travelled between machines would be a surprise rather than a feature.
 * Language is the opposite — it is a fact about the person, not the device,
 * so it lives on the account (`users.settings`) and follows them to any
 * browser they sign in from.
 */
export function AccountGeneralPane() {
  const { i18n } = useLingui();
  const { theme, setTheme } = useTheme();
  const locale = useLocale();
  const setLocale = useSetLocale();
  const qc = useQueryClient();
  const { data: me } = useMe();
  const prefs = useUiStore((s) => s.listingPrefs);
  const setPrefs = useUiStore((s) => s.setListingPrefs);
  const officeOpenIn = useUiStore((s) => s.officeOpenIn);
  const setOfficeOpenIn = useUiStore((s) => s.setOfficeOpenIn);

  // Language names are written in the language they name — a reader looking
  // for their own language finds "Tiếng Việt", not "Vietnamese" spelled in a
  // language they cannot read. So these labels are never translated.
  const localeItems = Object.entries(LOCALES).map(([value, label]) => ({ label, value }));

  return (
    <div className="flex max-w-xl flex-col gap-6">
      <section className="flex flex-col gap-3">
        <SectionLabel>
          <Trans>Language</Trans>
        </SectionLabel>
        <div className="flex items-center justify-between gap-4">
          {/* `Label` is a row by default; the hint belongs under the name,
              not beside it, or the pair competes with the control opposite. */}
          <Label htmlFor="pref-locale" className="flex-col items-start gap-0.5 font-normal">
            <Trans>Display language</Trans>
            <span className="text-muted-foreground block text-[10px]">
              <Trans>
                Saved to your account, so it follows you to any browser you sign in from.
              </Trans>
            </span>
          </Label>
          <Select
            value={locale}
            items={localeItems}
            onValueChange={(v) => {
              const next = ((v as string) ?? locale) as Locale;
              void setLocale(next).catch(() => {
                // The catalog is already active — only the account write
                // failed, so say what did not stick rather than reverting a
                // language the reader can now see working.
                toast.error(
                  i18n._(msg`Language changed here, but could not be saved to your account.`),
                );
              });
            }}
          >
            <SelectTrigger id="pref-locale" className="w-44 shrink-0">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {localeItems.map((item) => (
                <SelectItem key={item.value} value={item.value}>
                  {item.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
      </section>

      <section className="flex flex-col gap-3">
        <SectionLabel>
          <Trans>Notifications</Trans>
        </SectionLabel>
        <div className="flex items-center justify-between gap-4">
          <Label htmlFor="pref-email-on-share" className="flex-col items-start gap-0.5 font-normal">
            <Trans>Email me when something is shared with me</Trans>
            <span className="text-muted-foreground block text-[10px]">
              <Trans>
                Saved to your account. Only sent when your administrator has set up mail.
              </Trans>
            </span>
          </Label>
          <Switch
            id="pref-email-on-share"
            checked={me?.preferences.email_on_share !== false}
            onCheckedChange={(v) => {
              updatePreferences({ email_on_share: v })
                .then((updated: UserPreferences) => {
                  qc.setQueryData<UserProfile>(keys.me, (prev) =>
                    prev ? { ...prev, preferences: updated } : prev,
                  );
                })
                .catch(() => {
                  toast.error(i18n._(msg`Could not save your notification setting.`));
                });
            }}
          />
        </div>
      </section>

      <section className="flex flex-col gap-3">
        <SectionLabel>
          <Trans>Appearance</Trans>
        </SectionLabel>
        <Field
          id="pref-theme"
          label={msg`Theme`}
          hint={msg`Press D anywhere outside a text field to flip between light and dark.`}
          value={theme}
          items={THEMES}
          onChange={(v) => setTheme(v as "system" | "light" | "dark")}
        />
      </section>

      <section className="flex flex-col gap-3">
        <SectionLabel>
          <Trans>Default listing</Trans>
        </SectionLabel>
        <p className="text-muted-foreground text-[10px]">
          <Trans>
            Used when a link says nothing about how to draw the folder. A link that carries a view
            or a sort keeps the one it was copied with.
          </Trans>
        </p>
        <Field
          id="pref-view"
          label={msg`View`}
          value={prefs.view}
          items={VIEWS}
          onChange={(v) => setPrefs({ view: v as NodeView })}
        />
        <Field
          id="pref-sort"
          label={msg`Sort by`}
          value={prefs.sort}
          items={SORTS}
          onChange={(v) => setPrefs({ sort: v as SortField })}
        />
        <Field
          id="pref-order"
          label={msg`Direction`}
          value={prefs.order}
          items={ORDERS}
          onChange={(v) => setPrefs({ order: v as SortOrder })}
        />
      </section>

      <section className="flex flex-col gap-3">
        <SectionLabel>
          <Trans>Opening files</Trans>
        </SectionLabel>
        <Field
          id="pref-office-open"
          label={msg`Open office files in`}
          hint={msg`Word, Excel, PowerPoint and PDF files. Saved to this browser only.`}
          value={officeOpenIn}
          items={OFFICE_OPEN}
          onChange={(v) => setOfficeOpenIn(v as OfficeOpenIn)}
        />
      </section>
    </div>
  );
}

function Field({
  id,
  label,
  hint,
  value,
  items,
  onChange,
}: {
  id: string;
  label: MessageDescriptor;
  hint?: MessageDescriptor;
  value: string;
  items: Choice[];
  onChange: (value: string) => void;
}) {
  const { i18n } = useLingui();
  const resolved = items.map((item) => ({ label: i18n._(item.label), value: item.value }));

  return (
    <div className="flex items-center justify-between gap-4">
      <Label htmlFor={id} className="flex-col items-start gap-0.5 font-normal">
        {i18n._(label)}
        {hint && <span className="text-muted-foreground block text-[10px]">{i18n._(hint)}</span>}
      </Label>
      <Select
        value={value}
        items={resolved}
        onValueChange={(v) => onChange((v as string) ?? value)}
      >
        <SelectTrigger id={id} className="w-44 shrink-0">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          {resolved.map((item) => (
            <SelectItem key={item.value} value={item.value}>
              {item.label}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </div>
  );
}
