import { CloudUpload, FolderPlus, FolderUp, Plus } from "lucide-react";
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

/**
 * The rail's primary action. Styled as the accented row the dashboard blocks
 * use for "Quick Create" — it is the one control in the rail that starts
 * something rather than navigating, so it does not read as a nav row.
 */
export function NavCreate({
  onNewFolder,
  onUploadFiles,
  onUploadFolder,
}: {
  onNewFolder: () => void;
  onUploadFiles: () => void;
  onUploadFolder: () => void;
}) {
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
                <DropdownMenuItem onClick={onUploadFiles}>
                  <CloudUpload /> Upload files
                </DropdownMenuItem>
                <DropdownMenuItem onClick={onUploadFolder}>
                  <FolderUp /> Upload folder
                </DropdownMenuItem>
                <DropdownMenuItem onClick={onNewFolder}>
                  <FolderPlus /> New folder
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          </SidebarMenuItem>
        </SidebarMenu>
      </SidebarGroupContent>
    </SidebarGroup>
  );
}
