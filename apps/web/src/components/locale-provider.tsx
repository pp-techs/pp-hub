/* eslint-disable react-refresh/only-export-components */
import * as React from "react";
import { I18nProvider, useLingui } from "@lingui/react";
import { useLingui as useLinguiMacro } from "@lingui/react/macro";
import { useQueryClient } from "@tanstack/react-query";
import { UiStringsProvider } from "@workspace/ui/lib/strings";
import { activateLocale, i18n, isLocale, storeLocale, type Locale } from "@/lib/i18n";
import { keys } from "@/lib/queries/keys";
import { updatePreferences } from "@/lib/api/users";
import type { UserProfile } from "@/lib/api";

/**
 * Applies the account's language and keeps the device's copy of it in step.
 *
 * The account is the source of truth, but it only arrives with
 * `GET /users/me`, which is one round trip after the first paint. Writing
 * every applied locale back to `localStorage` is what closes that gap: the
 * next cold start reads it in `detectLocale()` and paints correctly from the
 * first frame instead of flashing English.
 *
 * Reading the profile through `getQueryData` plus a cache subscription rather
 * than `useMe()` is deliberate — this sits above the router, so a `useQuery`
 * here would fire an identity request on `/login` and `/s/{token}`, which
 * are both legitimately signed out.
 */
function useAccountLocale() {
  const qc = useQueryClient();

  React.useEffect(() => {
    const apply = () => {
      const profile = qc.getQueryData<UserProfile>(keys.me);
      const wanted = profile?.preferences?.locale;
      if (!isLocale(wanted) || wanted === i18n.locale) return;
      void activateLocale(wanted).then(() => storeLocale(wanted));
    };
    apply();
    return qc.getQueryCache().subscribe((event) => {
      if (event.query.queryHash === JSON.stringify(keys.me)) apply();
    });
  }, [qc]);
}

/**
 * Switch language for this account. Activates immediately so the UI does not
 * wait on the network, mirrors the choice into `localStorage` for the next
 * cold start, and persists it to the account last.
 */
export function useSetLocale() {
  const qc = useQueryClient();

  return React.useCallback(
    async (locale: Locale) => {
      await activateLocale(locale);
      storeLocale(locale);
      const updated = await updatePreferences({ locale });
      qc.setQueryData<UserProfile>(keys.me, (prev) =>
        prev ? { ...prev, preferences: updated } : prev,
      );
    },
    [qc],
  );
}

/** The active locale, as a reactive value. */
export function useLocale(): Locale {
  const { i18n: active } = useLingui();
  return active.locale as Locale;
}

/**
 * The strings `packages/ui` primitives render themselves — screen-reader
 * labels on icon-only controls, mostly. The package deliberately has no i18n
 * dependency of its own, so the app hands them down; anything omitted keeps
 * the package's English default.
 */
function UiStrings({ children }: { children: React.ReactNode }) {
  const { t } = useLinguiMacro();
  const strings = React.useMemo(
    () => ({
      close: t`Close`,
      more: t`More`,
      toggleSidebar: t`Toggle sidebar`,
      sidebar: t`Sidebar`,
      sidebarDescription: t`Displays the mobile sidebar.`,
      breadcrumb: t`Breadcrumb`,
    }),
    [t],
  );
  return <UiStringsProvider strings={strings}>{children}</UiStringsProvider>;
}

/**
 * Must sit inside the QueryClientProvider: it reads the identity cache.
 * `I18nProvider` is what re-renders the tree when the catalog changes, so
 * everything that depends on the language — including `UiStrings` — belongs
 * beneath it.
 */
export function LocaleProvider({ children }: { children: React.ReactNode }) {
  useAccountLocale();
  return (
    <I18nProvider i18n={i18n}>
      <UiStrings>{children}</UiStrings>
    </I18nProvider>
  );
}
