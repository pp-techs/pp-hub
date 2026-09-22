import { useNavigate } from "@tanstack/react-router";
import { useQueryClient } from "@tanstack/react-query";
import { Trans, useLingui } from "@lingui/react/macro";
import { toast } from "sonner";
import {
  Building2,
  ChevronsUpDown,
  LogOut,
  Settings,
  Shield,
  ShieldAlert,
  SlidersHorizontal,
  User,
} from "lucide-react";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@workspace/ui/components/dropdown-menu";
import { SidebarMenu, SidebarMenuButton, SidebarMenuItem } from "@workspace/ui/components/sidebar";
import { signOut, useMe, useSpace } from "@/lib/queries";
import { useActiveSpaceId } from "@/lib/use-active-space";
import { useSettingsNavigation } from "@/lib/use-settings-hash";
import { UserAvatar } from "@/components/user-avatar";

/**
 * The rail's footer: who is signed in, with every way out of the drive behind
 * the whole row — there is nothing else the row could do, so a separate
 * trailing button would only be a smaller target for the same action.
 *
 * This is the single entry point into settings. The space switcher used to
 * carry its own pair of admin links, which meant the answer to "where do I
 * change something" depended on which thing you wanted to change.
 */
export function NavUser() {
  const navigate = useNavigate();
  const qc = useQueryClient();
  const me = useMe();
  const space = useSpace(useActiveSpaceId());
  const { openSettings } = useSettingsNavigation();
  const { t } = useLingui();

  const name = me.data?.display_name || me.data?.name || t`User`;

  /**
   * A failed sign-out stays put: navigating to /login on a session the server
   * still honours only hides it.
   */
  async function handleSignOut() {
    try {
      if (await signOut(qc)) return;
      void navigate({ to: "/login" });
    } catch {
      toast.error(t`Could not sign out. Please try again.`);
    }
  }

  return (
    <SidebarMenu>
      <SidebarMenuItem>
        <DropdownMenu>
          <DropdownMenuTrigger
            render={
              <SidebarMenuButton
                size="lg"
                aria-label={t`Account menu`}
                className="data-popup-open:bg-sidebar-accent data-popup-open:text-sidebar-accent-foreground"
              >
                <UserAvatar
                  src={me.data?.avatar_url}
                  name={name}
                  email={me.data?.email}
                  size="sm"
                />
                <div className="grid flex-1 text-left leading-tight">
                  <span className="truncate text-sm font-medium">{name}</span>
                  <span className="text-muted-foreground truncate text-[10px]">
                    {me.data?.email}
                  </span>
                </div>
                <ChevronsUpDown className="ml-auto" />
              </SidebarMenuButton>
            }
          />
          {/* Upwards and left-aligned: the trigger is the last row of the
              rail, so anything below it is off-screen. Width comes from
              `--anchor-width` in the popup's own classes, so the menu is
              exactly as wide as the row it belongs to. */}
          <DropdownMenuContent side="top" align="start" sideOffset={8}>
            <DropdownMenuGroup>
              <DropdownMenuLabel className="font-normal">
                <div className="flex flex-col gap-0.5">
                  <p className="text-xs font-medium">{name}</p>
                  <p className="text-muted-foreground text-[10px]">{me.data?.email}</p>
                </div>
              </DropdownMenuLabel>
            </DropdownMenuGroup>
            <DropdownMenuSeparator />
            <DropdownMenuItem onClick={() => openSettings("general")}>
              <Settings /> <Trans>Settings</Trans>
            </DropdownMenuItem>
            <DropdownMenuItem onClick={() => openSettings("profile")}>
              <User /> <Trans>Profile</Trans>
            </DropdownMenuItem>
            {space?.is_admin && (
              <>
                <DropdownMenuSeparator />
                <DropdownMenuItem onClick={() => openSettings("space")}>
                  <Building2 /> <Trans>Space settings</Trans>
                </DropdownMenuItem>
              </>
            )}
            {/* The two server-administration surfaces are reachable nowhere
                else in the shell, and the routes gate on `is_admin`
                themselves — this only keeps the entry out of a menu where it
                would always bounce. They stay full pages: both are paged
                tables about the instance, not settings of this account. */}
            {me.data?.is_admin && (
              <>
                <DropdownMenuSeparator />
                <DropdownMenuItem onClick={() => void navigate({ to: "/admin/users" })}>
                  <Shield /> <Trans>Server administration</Trans>
                </DropdownMenuItem>
                <DropdownMenuItem onClick={() => void navigate({ to: "/admin/security" })}>
                  <ShieldAlert /> <Trans>Security log</Trans>
                </DropdownMenuItem>
                <DropdownMenuItem onClick={() => void navigate({ to: "/admin/settings" })}>
                  <SlidersHorizontal /> <Trans>Instance settings</Trans>
                </DropdownMenuItem>
              </>
            )}
            <DropdownMenuSeparator />
            <DropdownMenuItem onClick={() => void handleSignOut()}>
              <LogOut /> <Trans>Sign out</Trans>
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </SidebarMenuItem>
    </SidebarMenu>
  );
}
