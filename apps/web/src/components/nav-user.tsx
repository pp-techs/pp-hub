import { useNavigate } from "@tanstack/react-router";
import { useQueryClient } from "@tanstack/react-query";
import { Trans, useLingui } from "@lingui/react/macro";
import { toast } from "sonner";
import { ChevronsUpDown, LogOut, Shield, ShieldAlert, User } from "lucide-react";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@workspace/ui/components/dropdown-menu";
import { Avatar, AvatarFallback } from "@workspace/ui/components/avatar";
import {
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  useSidebar,
} from "@workspace/ui/components/sidebar";
import { signOut, useMe } from "@/lib/queries";

/**
 * The rail's footer: who is signed in, with the account menu behind the whole
 * row — there is nothing else the row could do, so a separate trailing button
 * would only be a smaller target for the same action.
 */
export function NavUser() {
  const navigate = useNavigate();
  const qc = useQueryClient();
  const me = useMe();
  const { isMobile } = useSidebar();
  const { t } = useLingui();

  const name = me.data?.display_name || me.data?.name || t`User`;
  const initials = (me.data?.name || me.data?.email || "?").charAt(0).toUpperCase();

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
                <Avatar className="size-8 rounded-lg">
                  <AvatarFallback className="rounded-lg">{initials}</AvatarFallback>
                </Avatar>
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
          <DropdownMenuContent
            className="min-w-56"
            side={isMobile ? "bottom" : "right"}
            align="end"
          >
            <DropdownMenuGroup>
              <DropdownMenuLabel className="font-normal">
                <div className="flex flex-col gap-0.5">
                  <p className="text-xs font-medium">{name}</p>
                  <p className="text-muted-foreground text-[10px]">{me.data?.email}</p>
                </div>
              </DropdownMenuLabel>
            </DropdownMenuGroup>
            <DropdownMenuSeparator />
            <DropdownMenuItem onClick={() => void navigate({ to: "/drive/account/profile" })}>
              <User /> <Trans>Account</Trans>
            </DropdownMenuItem>
            {/* The two server-administration surfaces are reachable nowhere
                else in the shell, and the routes gate on `is_admin`
                themselves — this only keeps the entry out of a menu where it
                would always bounce. */}
            {me.data?.is_admin && (
              <>
                <DropdownMenuSeparator />
                <DropdownMenuItem onClick={() => void navigate({ to: "/admin/users" })}>
                  <Shield /> <Trans>Server administration</Trans>
                </DropdownMenuItem>
                <DropdownMenuItem onClick={() => void navigate({ to: "/admin/security" })}>
                  <ShieldAlert /> <Trans>Security log</Trans>
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
