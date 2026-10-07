import type { ComponentProps } from "react";
import { Link, useLocation } from "@tanstack/react-router";
import { Trans, useLingui } from "@lingui/react/macro";
import { msg } from "@lingui/core/macro";
import type { MessageDescriptor } from "@lingui/core";
import { ArrowLeft, Server, ShieldAlert, SlidersHorizontal, Users } from "lucide-react";
import type { LucideIcon } from "lucide-react";
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarGroupContent,
  SidebarGroupLabel,
  SidebarHeader,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
} from "@workspace/ui/components/sidebar";
import { useMe } from "@/lib/queries";
import { UserAvatar } from "@/components/user-avatar";

export interface AdminNavItem {
  to: "/admin/users" | "/admin/security" | "/admin/settings";
  label: MessageDescriptor;
  icon: LucideIcon;
}

/**
 * The three server-administration surfaces. A descriptor rather than a string:
 * this is module scope and runs before any catalog is active. The layout's
 * breadcrumb reads the same list, so a page is added in one place.
 */
export const ADMIN_NAV: AdminNavItem[] = [
  { to: "/admin/users", label: msg`Users`, icon: Users },
  { to: "/admin/security", label: msg`Security log`, icon: ShieldAlert },
  { to: "/admin/settings", label: msg`Instance settings`, icon: SlidersHorizontal },
];

/**
 * The administration rail: where this install's controls live, a way back to
 * the drive, and who is signed in. It mirrors the drive rail's structure so
 * the two read as one product.
 */
export function AdminSidebar(props: ComponentProps<typeof Sidebar>) {
  const { t, i18n } = useLingui();
  const { pathname } = useLocation();
  const me = useMe();
  const name = me.data?.display_name || me.data?.name || t`User`;

  return (
    <Sidebar collapsible="offcanvas" className="px-0" {...props}>
      <SidebarHeader>
        <div className="flex items-center gap-2.5 px-2 py-1.5">
          <div className="bg-primary text-primary-foreground flex size-8 shrink-0 items-center justify-center rounded-lg">
            <Server className="size-4" />
          </div>
          <div className="grid flex-1 leading-tight">
            <span className="truncate text-sm font-medium">
              <Trans>Administration</Trans>
            </span>
            <span className="text-muted-foreground truncate text-[10px]">
              <Trans>This install</Trans>
            </span>
          </div>
        </div>
      </SidebarHeader>

      <SidebarContent>
        <SidebarGroup>
          <SidebarGroupLabel>
            <Trans>Server</Trans>
          </SidebarGroupLabel>
          <SidebarGroupContent>
            <SidebarMenu>
              {ADMIN_NAV.map((item) => (
                <SidebarMenuItem key={item.to}>
                  <SidebarMenuButton
                    isActive={pathname.startsWith(item.to)}
                    render={<Link to={item.to} />}
                  >
                    <item.icon />
                    <span>{i18n._(item.label)}</span>
                  </SidebarMenuButton>
                </SidebarMenuItem>
              ))}
            </SidebarMenu>
          </SidebarGroupContent>
        </SidebarGroup>
      </SidebarContent>

      <SidebarFooter>
        <SidebarMenu>
          <SidebarMenuItem>
            <SidebarMenuButton render={<Link to="/drive" />}>
              <ArrowLeft />
              <span>
                <Trans>Back to Drive</Trans>
              </span>
            </SidebarMenuButton>
          </SidebarMenuItem>
        </SidebarMenu>
        <div className="flex items-center gap-2 border-t px-2 pt-3 pb-1">
          <UserAvatar src={me.data?.avatar_url} name={name} email={me.data?.email} size="sm" />
          <div className="grid min-w-0 flex-1 leading-tight">
            <span className="truncate text-sm font-medium">{name}</span>
            <span className="text-muted-foreground truncate text-[10px]">{me.data?.email}</span>
          </div>
        </div>
      </SidebarFooter>
    </Sidebar>
  );
}
