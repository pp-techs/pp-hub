import { Outlet, createFileRoute, redirect, useLocation } from "@tanstack/react-router";
import type { CSSProperties } from "react";
import { toast } from "sonner";
import { useLingui } from "@lingui/react/macro";
import { msg } from "@lingui/core/macro";
import {
  Breadcrumb,
  BreadcrumbItem,
  BreadcrumbList,
  BreadcrumbPage,
  BreadcrumbSeparator,
} from "@workspace/ui/components/breadcrumb";
import { SidebarInset, SidebarProvider, SidebarTrigger } from "@workspace/ui/components/sidebar";
import { AdminSidebar, ADMIN_NAV } from "@/components/admin-sidebar";
import { requireAuth } from "@/lib/queries";
import { i18n as globalI18n } from "@/lib/i18n";

export const Route = createFileRoute("/admin")({
  // Gate here, once, for every page under /admin. A page that resolves and
  // then says "admin only" has already told the caller that this URL exists
  // and what it is for; the redirect says nothing.
  beforeLoad: async ({ context, location }) => {
    const me = await requireAuth(context.queryClient, location.href);
    if (!me.is_admin) {
      // `beforeLoad` runs outside React, so the singleton answers here.
      toast.error(globalI18n._(msg`Only an administrator can open server administration.`));
      throw redirect({ to: "/drive" });
    }
  },
  component: AdminLayout,
});

/**
 * The shell of server administration: the drive's own sidebar-and-inset
 * frame with the admin rail in place of the drive one. It is a layout route so
 * the rail stays mounted while moving between pages instead of redrawing.
 */
function AdminLayout() {
  const { t, i18n } = useLingui();
  const { pathname } = useLocation();
  const current = ADMIN_NAV.find((item) => pathname.startsWith(item.to));

  return (
    // Pinned to the viewport and scrolled inside the inset, as the drive is,
    // so the header stays put while a long settings form scrolls.
    <SidebarProvider
      className="h-svh overflow-hidden"
      style={
        {
          "--sidebar-width": "calc(var(--spacing) * 64)",
          "--header-height": "calc(var(--spacing) * 12)",
        } as CSSProperties
      }
    >
      <AdminSidebar variant="inset" />
      <SidebarInset className="min-h-0 overflow-hidden">
        <header className="flex h-(--header-height) shrink-0 items-center gap-2 border-b px-4">
          <SidebarTrigger className="-ml-1" />
          <Breadcrumb>
            <BreadcrumbList>
              <BreadcrumbItem className="text-muted-foreground hidden sm:inline-flex">
                {t`Administration`}
              </BreadcrumbItem>
              {current && (
                <>
                  <BreadcrumbSeparator className="hidden sm:block" />
                  <BreadcrumbItem>
                    <BreadcrumbPage className="font-medium">{i18n._(current.label)}</BreadcrumbPage>
                  </BreadcrumbItem>
                </>
              )}
            </BreadcrumbList>
          </Breadcrumb>
        </header>
        <main className="min-h-0 flex-1 overflow-y-auto">
          <Outlet />
        </main>
      </SidebarInset>
    </SidebarProvider>
  );
}
