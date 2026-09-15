import { useNavigate, useParams } from "@tanstack/react-router";
import { ChevronsUpDown, Plus } from "lucide-react";
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

/** Two-letter monogram for a space, e.g. "Acme Core" -> "AC". */
function monogram(name: string) {
  const words = name.trim().split(/\s+/).filter(Boolean);
  if (words.length === 0) return "?";
  if (words.length === 1) return words[0].slice(0, 2).toUpperCase();
  return (words[0][0] + words[1][0]).toUpperCase();
}

/**
 * Top band of the left rail: the active space as a monogram tile with its
 * name and role, opening the space list. Fixed 48px to line up with the
 * header and toolbar rows in the main column.
 */
export function SpaceSwitcher() {
  const spaces = useSpaces();
  const { spaceId } = useParams({ strict: false }) as { spaceId?: string };
  const navigate = useNavigate();
  const setNewSpaceOpen = useUiStore((s) => s.setNewSpaceOpen);

  const current = spaces.data?.find((s) => s.id === spaceId);

  return (
    <div className="border-border flex h-12 shrink-0 items-center justify-between gap-2 border-b px-3">
      <div className="flex min-w-0 items-center gap-2">
        <div className="bg-foreground text-background flex size-5 shrink-0 items-center justify-center rounded-sm text-[10px] font-semibold">
          {current ? monogram(current.name) : "—"}
        </div>
        <div className="flex min-w-0 flex-col leading-none">
          <span className="truncate text-xs font-medium">{current?.name ?? "Select a space"}</span>
          {current ? (
            <span className="text-muted-foreground mt-0.5 text-[10px] tracking-wider uppercase">
              {current.role}
            </span>
          ) : null}
        </div>
      </div>

      <DropdownMenu>
        <DropdownMenuTrigger
          render={<Button variant="ghost" size="icon-sm" aria-label="Switch space" />}
        >
          <ChevronsUpDown />
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end" className="w-56">
          <DropdownMenuGroup>
            <DropdownMenuLabel className="text-muted-foreground text-[11px] tracking-wider uppercase">
              Spaces
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
                <span className="text-muted-foreground ml-auto text-[10px] uppercase">
                  {space.role}
                </span>
              </DropdownMenuItem>
            ))}
          </DropdownMenuGroup>
          <DropdownMenuSeparator />
          <DropdownMenuItem onClick={() => setNewSpaceOpen(true)}>
            <Plus /> Create new space
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
    </div>
  );
}
