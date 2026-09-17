import { Outlet, createFileRoute } from "@tanstack/react-router";
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
 * Two tabs, not three: there is no session-listing endpoint yet, so a Sessions
 * tab could only be an empty pane.
 */
export const Route = createFileRoute("/drive/account")({
  head: () => ({ meta: [{ title: "Account · pp-hub" }] }),
  component: AccountLayout,
});

function AccountLayout() {
  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <header className="border-border flex h-10 shrink-0 items-center border-b px-4">
        <span className="text-xs font-medium">Account</span>
      </header>
      <SettingsTabs
        tabs={[
          { label: "Profile", to: "/drive/account/profile" },
          { label: "Preferences", to: "/drive/account/preferences" },
        ]}
      />
      <Outlet />
    </div>
  );
}
