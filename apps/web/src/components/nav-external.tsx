import { Link, useLocation } from "@tanstack/react-router";
import { Trans } from "@lingui/react/macro";
import { Globe } from "lucide-react";
import {
  SidebarGroup,
  SidebarGroupContent,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
} from "@workspace/ui/components/sidebar";
import { useExternalShared } from "@/lib/queries";

/**
 * The one rail row that belongs to no space: what people in other spaces have
 * shared with this account. Absent until there is something to open, so a
 * person with no outside shares never sees an empty page offered.
 */
export function NavExternal() {
  const external = useExternalShared();
  const active = useLocation({ select: (l) => /^\/drive\/shared\/?$/.test(l.pathname) });
  if (!external.data?.length) return null;
  return (
    <SidebarGroup>
      <SidebarGroupContent>
        <SidebarMenu>
          <SidebarMenuItem>
            <SidebarMenuButton
              isActive={active}
              render={<Link to="/drive/shared" search={{ dir: undefined }} />}
            >
              <Globe />
              <span>
                <Trans>Shared from outside</Trans>
              </span>
            </SidebarMenuButton>
          </SidebarMenuItem>
        </SidebarMenu>
      </SidebarGroupContent>
    </SidebarGroup>
  );
}
