import { Outlet, createFileRoute } from "@tanstack/react-router";
import { useLingui } from "@lingui/react/macro";
import { msg } from "@lingui/core/macro";
import { pageTitle } from "@/lib/i18n";
import { SettingsTabs } from "@/components/settings-tabs";

/**
 * The signed-in user's own settings, inside the drive shell.
 *
 * It sits beside `$spaceId` rather than under it because an account is not
 * owned by a space; the rail still shows the space you came from, so leaving
 * this page is one click rather than a full-page detour. That is also why the
 * previous `/profile` route is gone — a standalone page dropped the rail, the
 * breadcrumb and any way back.
 *
 * Three tabs: Profile is the account the server stores, Preferences is how
 * this browser draws the app, and Sessions lists the devices currently
 * signed in so one can be ended from another.
 */
export const Route = createFileRoute("/drive/account")({
  head: () => ({ meta: [{ title: pageTitle(msg`Account`) }] }),
  component: AccountLayout,
});

function AccountLayout() {
  const { t } = useLingui();

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <SettingsTabs
        tabs={[
          { label: t`Profile`, to: "/drive/account/profile" },
          { label: t`Preferences`, to: "/drive/account/preferences" },
          { label: t`Sessions`, to: "/drive/account/sessions" },
        ]}
      />
      <Outlet />
    </div>
  );
}
