import { useParams, useNavigate } from "@tanstack/react-router";
import { HardDrive, ChevronDown, Plus } from "lucide-react";
import { Button } from "@workspace/ui/components/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@workspace/ui/components/dropdown-menu";
import { useSpaces } from "@/lib/queries";
import { useUiStore } from "@/lib/ui-store";
import { cn } from "@workspace/ui/lib/utils";

export function TeamSwitcher() {
  const spaces = useSpaces();
  const { spaceId } = useParams({ strict: false }) as { spaceId?: string };
  const navigate = useNavigate();
  const setNewSpaceOpen = useUiStore((s) => s.setNewSpaceOpen);

  const currentSpace = spaces.data?.find((s) => s.id === spaceId);

  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <Button variant="ghost" className="w-full justify-between gap-2 px-2 py-1.5 text-sm" />
        }
      >
        <span className="flex items-center gap-2 truncate">
          <HardDrive size={16} />
          {currentSpace?.name ?? "Select a space"}
        </span>
        <ChevronDown size={14} className="shrink-0 opacity-60" />
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start" className="w-56">
        <DropdownMenuGroup>
          <DropdownMenuLabel className="text-xs text-muted-foreground">Spaces</DropdownMenuLabel>
          {spaces.data?.map((space) => (
            <DropdownMenuItem
              key={space.id}
              onClick={() =>
                navigate({
                  to: "/drive/$spaceId",
                  params: { spaceId: space.id },
                  search: { dir: undefined },
                })
              }
              className={cn(space.id === spaceId && "bg-accent")}
            >
              <HardDrive size={14} className="mr-2" />
              <span className="truncate">{space.name}</span>
              <span className="ml-auto text-[10px] uppercase text-muted-foreground">
                {space.role}
              </span>
            </DropdownMenuItem>
          ))}
        </DropdownMenuGroup>
        <DropdownMenuSeparator />
        <DropdownMenuItem onClick={() => setNewSpaceOpen(true)}>
          <Plus size={14} className="mr-2" />
          Create new space
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
