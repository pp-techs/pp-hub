import { useState } from "react";
import { ChevronsUpDown } from "lucide-react";
import { Button } from "@workspace/ui/components/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@workspace/ui/components/dropdown-menu";
import { Input } from "@workspace/ui/components/input";
import { Avatar, AvatarFallback, AvatarImage } from "@workspace/ui/components/avatar";
import { useUserSearch } from "@/lib/queries";
import type { UserSearchResult } from "@/lib/api/types";

interface MemberPickerProps {
  onSelect: (user: UserSearchResult) => void;
  placeholder?: string;
  excludeIds?: string[];
}

export function MemberPicker({
  onSelect,
  placeholder = "Add member...",
  excludeIds = [],
}: MemberPickerProps) {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const { data: users = [], isLoading } = useUserSearch(query, open);

  const filtered = users.filter((u) => !excludeIds.includes(u.id));

  return (
    <DropdownMenu open={open} onOpenChange={setOpen}>
      <DropdownMenuTrigger render={<Button variant="outline" className="w-full justify-between" />}>
        {placeholder}
        <ChevronsUpDown className="ml-2 h-4 w-4 shrink-0 opacity-50" />
      </DropdownMenuTrigger>
      <DropdownMenuContent className="w-[300px] p-2" align="start">
        <Input
          placeholder="Search users..."
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          className="mb-2"
          autoFocus
        />
        <DropdownMenuGroup className="max-h-[200px] overflow-y-auto">
          {isLoading && (
            <div className="text-muted-foreground py-2 text-center text-sm">Searching...</div>
          )}
          {!isLoading && filtered.length === 0 && (
            <div className="text-muted-foreground py-2 text-center text-sm">No users found.</div>
          )}
          {filtered.map((user) => (
            <DropdownMenuItem
              key={user.id}
              onSelect={() => {
                onSelect(user);
                setOpen(false);
                setQuery("");
              }}
              className="flex items-center gap-2"
            >
              <Avatar className="h-6 w-6">
                <AvatarImage src={user.avatar_url} />
                <AvatarFallback className="text-xs">
                  {(user.display_name || user.name || user.email).slice(0, 2).toUpperCase()}
                </AvatarFallback>
              </Avatar>
              <div className="flex flex-col">
                <span>{user.display_name || user.name}</span>
                <span className="text-muted-foreground text-xs">{user.email}</span>
              </div>
            </DropdownMenuItem>
          ))}
        </DropdownMenuGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
