import { Outlet, createFileRoute, redirect } from "@tanstack/react-router";
import { useLingui } from "@lingui/react/macro";
import { msg } from "@lingui/core/macro";
import { toast } from "sonner";
import { fetchSpaces } from "@/lib/queries";
import { i18n, pageTitle } from "@/lib/i18n";
import { SettingsTabs } from "@/components/settings-tabs";

/**
 * Space settings: one page per concern, each its own route.
 *
 * Admin is checked here rather than inside a pane. The old panel rendered
 * "only an admin can change these settings" *after* resolving the URL, which
 * told a member a settings page exists at an address they cannot use; a member
 * is now sent back to the listing instead.
 */
export const Route = createFileRoute("/drive/$spaceId/settings")({
  head: () => ({ meta: [{ title: pageTitle(msg`Space settings`) }] }),
  beforeLoad: async ({ context, params }) => {
    const spaces = await fetchSpaces(context.queryClient);
    const space = spaces.find((s) => s.id === params.spaceId);
    if (space && !space.is_admin) {
      // `beforeLoad` runs outside React, so the singleton is the only i18n
      // there is here — the same reason `head` reaches for it.
      toast.error(i18n._(msg`Only a space admin can open settings`));
      throw redirect({
        to: "/drive/$spaceId",
        params: { spaceId: params.spaceId },
        search: { dir: undefined },
      });
    }
  },
  component: SettingsLayout,
});

function SettingsLayout() {
  const { t } = useLingui();
  const { spaceId } = Route.useParams();

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <SettingsTabs
        tabs={[
          { label: t`General`, to: "/drive/$spaceId/settings/general", params: { spaceId } },
          { label: t`People`, to: "/drive/$spaceId/settings/people", params: { spaceId } },
          {
            label: t`Danger zone`,
            to: "/drive/$spaceId/settings/danger",
            params: { spaceId },
            danger: true,
          },
        ]}
      />
      <Outlet />
    </div>
  );
}
