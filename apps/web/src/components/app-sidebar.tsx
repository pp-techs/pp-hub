import type { ComponentProps } from "react";
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarHeader,
  SidebarSeparator,
} from "@workspace/ui/components/sidebar";
import { NavApps } from "@/components/nav-apps";
import { NavCreate } from "@/components/nav-create";
import { NavExternal } from "@/components/nav-external";
import { NavSpace } from "@/components/nav-space";
import { NavUser } from "@/components/nav-user";
import { SpaceSwitcher } from "@/components/space-switcher";
import { StorageMeter } from "@/components/storage-meter";
import { BrandMark } from "@/components/brand-mark";
import { useMe } from "@/lib/queries";
import { PRODUCT_NAME } from "@/lib/i18n";

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
  // A guest has no space, storage or settings: the rail is the brand, what
  // was shared with them, and the way out.
  const guest = useMe().data?.kind === "guest";
  return (
    // The inset container pads by p-2 and every header/group/footer pads by
    // p-2 again, doubling the gutter. The vertical half stays so the rail
    // lines up with the inset card's m-2; the horizontal half goes.
    <Sidebar collapsible="offcanvas" className="px-0" {...props}>
      <SidebarHeader>
        {guest ? (
          <div className="flex items-center gap-2.5 px-2 py-1">
            <BrandMark />
            <span className="text-sm font-medium tracking-tight">{PRODUCT_NAME}</span>
          </div>
        ) : (
          <SpaceSwitcher spaceId={spaceId} />
        )}
      </SidebarHeader>
      <SidebarContent>
        {spaceId && (
          <>
            <NavCreate />
            <NavSpace spaceId={spaceId}>
              <NavApps spaceId={spaceId} />
            </NavSpace>
          </>
        )}
        <NavExternal />
      </SidebarContent>
      <SidebarFooter>
        {!guest && <StorageMeter />}
        {!guest && <SidebarSeparator className="mx-0" />}
        <NavUser />
      </SidebarFooter>
    </Sidebar>
  );
}
