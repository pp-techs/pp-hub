import { useMemo, useState } from "react";
import { Trans, useLingui } from "@lingui/react/macro";
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
import { useDebounced } from "@/lib/use-debounced";
import type { UserSearchResult } from "@/lib/api/types";

interface MemberPickerProps {
  onSelect: (user: UserSearchResult) => void;
  placeholder?: string;
  excludeIds?: string[];
}

export function MemberPicker({ onSelect, placeholder, excludeIds = [] }: MemberPickerProps) {
  const { t } = useLingui();
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  // The field stays controlled by what was typed; only the search trails it,
  // so the dropdown does not fire a request per keystroke.
  const { data: users = [], isLoading } = useUserSearch(useDebounced(query, 250), open);

  const excluded = useMemo(() => new Set(excludeIds), [excludeIds]);
  const filtered = users.filter((u) => !excluded.has(u.id));

  return (
    <DropdownMenu open={open} onOpenChange={setOpen}>
      <DropdownMenuTrigger render={<Button variant="outline" className="w-full justify-between" />}>
        {placeholder ?? t`Add member...`}
        <ChevronsUpDown className="ml-2 h-4 w-4 shrink-0 opacity-50" />
      </DropdownMenuTrigger>
      <DropdownMenuContent className="w-[300px] p-2" align="start">
        <Input
          placeholder={t`Search users...`}
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          className="mb-2"
          autoFocus
          // base-ui's Menu popup runs a bubble-phase keydown handler for
          // roving focus and typeahead that calls preventDefault() on
          // printable keys, which swallowed every keystroke before this
          // input's own default action (typing) could run — see
          // grantee-picker.tsx for the same fix.
          onKeyDown={(e) => e.key !== "Escape" && e.stopPropagation()}
        />
        <DropdownMenuGroup className="max-h-[200px] overflow-y-auto">
          {isLoading && (
            <div className="text-muted-foreground py-2 text-center text-sm">
              <Trans>Searching...</Trans>
            </div>
          )}
          {!isLoading && filtered.length === 0 && (
            <div className="text-muted-foreground py-2 text-center text-sm">
              <Trans>No users found.</Trans>
            </div>
          )}
          {filtered.map((user) => (
            <DropdownMenuItem
              key={user.id}
              // base-ui's Menu.Item exposes onClick, not onSelect. An
              // onSelect prop lands on the underlying div, where it means the
              // text-selection event and never fires for a click — so picking
              // a person silently did nothing, here and in the invite dialog.
              onClick={() => {
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
