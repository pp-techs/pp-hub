import { Plus } from "lucide-react";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
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
  const write = useDriveWrite();
  const actions = folderActions(write).filter((a) => a.group === "create");

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
                    <span>New</span>
                  </SidebarMenuButton>
                }
              />
              <DropdownMenuContent align="start" className="w-48">
                {actions.map((action) => (
                  <DropdownMenuItem
                    key={action.key}
                    disabled={action.disabled}
                    onClick={action.run}
                  >
                    <action.icon /> {action.label}
                  </DropdownMenuItem>
                ))}
              </DropdownMenuContent>
            </DropdownMenu>
          </SidebarMenuItem>
        </SidebarMenu>
      </SidebarGroupContent>
    </SidebarGroup>
  );
}
