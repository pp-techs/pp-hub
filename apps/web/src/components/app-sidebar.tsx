import type { ComponentProps } from "react";
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarHeader,
  SidebarSeparator,
} from "@workspace/ui/components/sidebar";
import { NavCreate } from "@/components/nav-create";
import { NavSpace } from "@/components/nav-space";
import { NavUser } from "@/components/nav-user";
import { SpaceSwitcher } from "@/components/space-switcher";
import { StorageMeter } from "@/components/storage-meter";

/**
 * The drive rail: the active space at the top, the space's surfaces in the
 * middle, storage and account pinned to the bottom.
 *
 * `spaceId` may be undefined only before the first space exists; both nav
 * bands are about one space, so they wait for one rather than rendering an
 * empty frame.
 */
export function AppSidebar({
  spaceId,
  ...props
}: ComponentProps<typeof Sidebar> & {
  spaceId: string | undefined;
}) {
  return (
    <Sidebar collapsible="offcanvas" {...props}>
      <SidebarHeader>
        <SpaceSwitcher spaceId={spaceId} />
      </SidebarHeader>
      <SidebarContent>
        {spaceId && (
          <>
            <NavCreate />
            <NavSpace spaceId={spaceId} />
          </>
        )}
      </SidebarContent>
      <SidebarFooter>
        <StorageMeter />
        <SidebarSeparator className="mx-0" />
        <NavUser />
      </SidebarFooter>
    </Sidebar>
  );
}
