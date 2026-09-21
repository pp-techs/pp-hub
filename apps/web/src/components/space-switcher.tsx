import { Trans, useLingui } from "@lingui/react/macro";
import { useNavigate } from "@tanstack/react-router";
import { ChevronsUpDown, Plus, Settings, Users } from "lucide-react";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@workspace/ui/components/dropdown-menu";
import {
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  useSidebar,
} from "@workspace/ui/components/sidebar";
import { useMe, useSpaces } from "@/lib/queries";
import { useUiStore } from "@/lib/ui-store";
import { cn } from "@workspace/ui/lib/utils";

/** Two-letter monogram for a space, e.g. "Acme Core" -> "AC". */
function monogram(name: string) {
  const words = name.trim().split(/\s+/).filter(Boolean);
  if (words.length === 0) return "?";
  if (words.length === 1) return words[0].slice(0, 2).toUpperCase();
  return (words[0][0] + words[1][0]).toUpperCase();
}

/**
 * The rail's header: the active space as a monogram tile with its name, and
 * the only place to change space — the space list is not duplicated anywhere
 * further down.
 *
 * `spaceId` is passed in rather than read from the route: the rail is mounted
 * above `/drive/$spaceId` and also renders on `/drive/account`, where the
 * space is the one the user will go back to.
 */
export function SpaceSwitcher({ spaceId }: { spaceId: string | undefined }) {
  const spaces = useSpaces();
  const navigate = useNavigate();
  const setNewSpaceOpen = useUiStore((s) => s.setNewSpaceOpen);
  // A space is a company. When organizations own membership, only an
  // instance admin may mint one, so the item is absent rather than present
  // and answering 403.
  const canCreateSpaces = useMe().data?.can_create_spaces ?? false;
  const { isMobile } = useSidebar();
  const { t } = useLingui();

  const current = spaces.data?.find((s) => s.id === spaceId);

  return (
    <SidebarMenu>
      <SidebarMenuItem>
        <DropdownMenu>
          <DropdownMenuTrigger
            render={
              <SidebarMenuButton
                size="lg"
                aria-label={t`Switch space`}
                className="data-popup-open:bg-sidebar-accent data-popup-open:text-sidebar-accent-foreground"
              >
                <div className="bg-foreground text-background flex aspect-square size-8 shrink-0 items-center justify-center rounded-lg text-[11px] font-semibold">
                  {current ? monogram(current.name) : "—"}
                </div>
                <div className="grid flex-1 text-left leading-tight">
                  <span className="truncate text-sm font-medium">
                    {current?.name ?? t`Select a space`}
                  </span>
                  {current?.is_admin && (
                    <span className="text-muted-foreground truncate text-[10px] tracking-wider uppercase">
                      <Trans>Admin</Trans>
                    </span>
                  )}
                </div>
                <ChevronsUpDown className="ml-auto" />
              </SidebarMenuButton>
            }
          />
          <DropdownMenuContent
            className="min-w-56"
            side={isMobile ? "bottom" : "right"}
            align="start"
          >
            <DropdownMenuGroup>
              <DropdownMenuLabel className="text-muted-foreground text-[11px] tracking-wider uppercase">
                <Trans>Spaces</Trans>
              </DropdownMenuLabel>
              {spaces.data?.map((space) => (
                <DropdownMenuItem
                  key={space.id}
                  className={cn(space.id === spaceId && "bg-accent")}
                  onClick={() =>
                    void navigate({
                      to: "/drive/$spaceId",
                      params: { spaceId: space.id },
                      search: { dir: undefined },
                    })
                  }
                >
                  <span className="truncate">{space.name}</span>
                  {space.is_admin && (
                    <span className="text-muted-foreground ml-auto text-[10px] uppercase">
                      <Trans>Admin</Trans>
                    </span>
                  )}
                </DropdownMenuItem>
              ))}
            </DropdownMenuGroup>
            {current?.is_admin && (
              <>
                <DropdownMenuSeparator />
                {/* The only way into space settings. It used to be a gear in
                    the main header, which sat next to a Members icon that
                    opened a second, competing copy of the same screen. */}
                <DropdownMenuItem
                  onClick={() =>
                    void navigate({
                      to: "/drive/$spaceId/settings/general",
                      params: { spaceId: current.id },
                    })
                  }
                >
                  <Settings /> <Trans>Space settings</Trans>
                </DropdownMenuItem>
                <DropdownMenuItem
                  onClick={() =>
                    void navigate({
                      to: "/drive/$spaceId/settings/people",
                      params: { spaceId: current.id },
                      search: { group: undefined },
                    })
                  }
                >
                  <Users /> <Trans>People</Trans>
                </DropdownMenuItem>
              </>
            )}
            {canCreateSpaces && (
              <>
                <DropdownMenuSeparator />
                <DropdownMenuItem onClick={() => setNewSpaceOpen(true)}>
                  <Plus /> <Trans>Create new space</Trans>
                </DropdownMenuItem>
              </>
            )}
          </DropdownMenuContent>
        </DropdownMenu>
      </SidebarMenuItem>
    </SidebarMenu>
  );
}
