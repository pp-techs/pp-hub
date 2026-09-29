import { Fragment } from "react";
import { Trans, useLingui } from "@lingui/react/macro";
import { Plus } from "lucide-react";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@workspace/ui/components/dropdown-menu";
import {
  SidebarGroup,
  SidebarGroupContent,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
} from "@workspace/ui/components/sidebar";
import { useDriveWrite } from "@/components/drive-write";
import { folderActions } from "@/components/folder-actions";

/**
 * The rail's primary action. Styled as the accented row the dashboard blocks
 * use for "Quick Create" — it is the one control in the rail that starts
 * something rather than navigating, so it does not read as a nav row.
 *
 * Its items come from `folderActions`, the same list the listing's background
 * menu renders, and act on `useDriveWrite`'s target rather than on a
 * destination of the rail's own — the rail is mounted above every drive
 * surface and cannot know which folder is open.
 */
export function NavCreate() {
  const { i18n } = useLingui();
  const write = useDriveWrite();
  const actions = folderActions(write).filter(
    (a) => a.group === "create" || a.group === "documents",
  );

  return (
    <SidebarGroup>
      <SidebarGroupContent>
        <SidebarMenu>
          <SidebarMenuItem>
            <DropdownMenu>
              <DropdownMenuTrigger
                render={
                  <SidebarMenuButton className="bg-primary text-primary-foreground duration-200 ease-linear hover:bg-primary/90 hover:text-primary-foreground active:bg-primary/90 active:text-primary-foreground">
                    <Plus />
                    <span>
                      <Trans>New</Trans>
                    </span>
                  </SidebarMenuButton>
                }
              />
              <DropdownMenuContent align="start" className="w-48">
                {actions.map((action, i) => (
                  <Fragment key={action.key}>
                    {/* Same grouping the background menu draws: the office and
                        markdown block sits apart from folders and uploads. */}
                    {i > 0 && actions[i - 1]!.group !== action.group && <DropdownMenuSeparator />}
                    <DropdownMenuItem disabled={action.disabled} onClick={action.run}>
                      <action.icon /> {i18n._(action.label)}
                    </DropdownMenuItem>
                  </Fragment>
                ))}
              </DropdownMenuContent>
            </DropdownMenu>
          </SidebarMenuItem>
        </SidebarMenu>
      </SidebarGroupContent>
    </SidebarGroup>
  );
}
