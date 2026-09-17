import { useNavigate } from "@tanstack/react-router";
import { LogOut, MoreHorizontal, User } from "lucide-react";
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
import { Avatar, AvatarFallback } from "@workspace/ui/components/avatar";
import { logout, meOptions } from "@/lib/queries";

/**
 * Bottom band of the left rail: who is signed in, with the account menu behind
 * the trailing button rather than making the whole row a control.
 */
export function NavUser() {
  const navigate = useNavigate();
  const me = meOptions();

  const initials = (me.data?.name || me.data?.email || "?").charAt(0).toUpperCase();

  return (
    <div className="flex items-center justify-between gap-2">
      <div className="flex min-w-0 items-center gap-2">
        <Avatar size="sm" className="shrink-0">
          <AvatarFallback>{initials}</AvatarFallback>
        </Avatar>
        <div className="flex min-w-0 flex-col leading-tight">
          <span className="truncate text-xs font-medium">{me.data?.name || "User"}</span>
          <span className="text-muted-foreground truncate text-[10px]">{me.data?.email}</span>
        </div>
      </div>

      <DropdownMenu>
        <DropdownMenuTrigger
          render={<Button variant="ghost" size="icon-sm" aria-label="Account menu" />}
        >
          <MoreHorizontal />
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end" side="top" className="w-56">
          <DropdownMenuGroup>
            <DropdownMenuLabel className="font-normal">
              <div className="flex flex-col gap-0.5">
                <p className="text-xs font-medium">{me.data?.name || "User"}</p>
                <p className="text-muted-foreground text-[10px]">{me.data?.email}</p>
              </div>
            </DropdownMenuLabel>
          </DropdownMenuGroup>
          <DropdownMenuSeparator />
          <DropdownMenuItem onClick={() => void navigate({ to: "/drive/account/profile" })}>
            <User /> Account
          </DropdownMenuItem>
          <DropdownMenuSeparator />
          <DropdownMenuItem
            onClick={async () => {
              await logout();
              void navigate({ to: "/login" });
            }}
          >
            <LogOut /> Sign out
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
    </div>
  );
}
