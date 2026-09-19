import { useNavigate } from "@tanstack/react-router";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { ChevronsUpDown, LogOut, User } from "lucide-react";
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

  const name = me.data?.display_name || me.data?.name || "User";
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
      toast.error("Could not sign out. Please try again.");
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
                aria-label="Account menu"
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
              <User /> Account
            </DropdownMenuItem>
            <DropdownMenuSeparator />
            <DropdownMenuItem onClick={() => void handleSignOut()}>
              <LogOut /> Sign out
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </SidebarMenuItem>
    </SidebarMenu>
  );
}
