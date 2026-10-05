import { useState } from "react";
import { Trans, useLingui } from "@lingui/react/macro";
import { ChevronsUpDown, Lock, Mail, ShieldCheck, Users } from "lucide-react";
import { Button } from "@workspace/ui/components/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuTrigger,
} from "@workspace/ui/components/dropdown-menu";
import { Input } from "@workspace/ui/components/input";
import { Avatar, AvatarFallback, AvatarImage } from "@workspace/ui/components/avatar";

/**
 * Something access can be given to: a group in the space, or one of its
 * members. Both resolve to a single row in `shares`, which is why one picker
 * offers both rather than the two dialogs this replaced.
 */
export type Grantee =
  | { kind: "group"; id: string; label: string; isEveryone: boolean; isAdmin: boolean }
  | { kind: "user"; id: string; label: string; email: string; avatarUrl?: string }
  /** A typed address; `id` and `label` are the address itself. */
  | { kind: "email"; id: string; label: string };

interface GranteePickerProps {
  /** Everything still available — the caller filters out what already holds access. */
  options: Grantee[];
  onPick: (grantee: Grantee) => void;
  placeholder?: string;
  disabled?: boolean;
  /** Offer "Invite {email}" for a typed address that matches nobody listed. */
  allowEmail?: boolean;
}

/** Loose on purpose: the server is the one that rejects a bad address. */
const EMAIL = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;

function matches(g: Grantee, needle: string) {
  if (g.label.toLowerCase().includes(needle)) return true;
  return g.kind === "user" && g.email.toLowerCase().includes(needle);
}

/**
 * Picks a group or a person to share with.
 *
 * Both lists are already in the query cache — `/spaces/{id}/groups` and
 * `/spaces/{id}/members` — so filtering is local: a round trip per keystroke
 * would buy nothing, and scoping the search to the space is what keeps one
 * company's staff list out of another's autocomplete.
 */
export function GranteePicker({
  options,
  onPick,
  placeholder,
  disabled,
  allowEmail = false,
}: GranteePickerProps) {
  const { t } = useLingui();
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");

  const needle = query.trim().toLowerCase();
  const visible = needle ? options.filter((g) => matches(g, needle)) : options;
  const groups = visible.filter((g) => g.kind === "group");
  const people = visible.filter((g) => g.kind === "user");
  // A typed address nobody listed matches: offered as an invitation, for the
  // server to resolve to a member, an outside account or a pending invite.
  const inviteEmail =
    allowEmail &&
    EMAIL.test(needle) &&
    !options.some((g) => g.kind === "user" && g.email.toLowerCase() === needle)
      ? needle
      : null;

  function pick(grantee: Grantee) {
    onPick(grantee);
    setOpen(false);
    setQuery("");
  }

  return (
    <DropdownMenu open={open} onOpenChange={setOpen}>
      <DropdownMenuTrigger
        disabled={disabled}
        render={<Button variant="outline" className="w-full justify-between font-normal" />}
      >
        {placeholder ?? t`Add people or groups…`}
        <ChevronsUpDown className="ml-2 h-4 w-4 shrink-0 opacity-50" />
      </DropdownMenuTrigger>
      <DropdownMenuContent className="w-[320px] p-2" align="start">
        <Input
          placeholder={allowEmail ? t`Search, or type an email…` : t`Search groups and people…`}
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          className="mb-2"
          autoFocus
          // base-ui's Menu popup runs a bubble-phase keydown handler for
          // roving focus and typeahead that calls preventDefault() on
          // printable keys — with the popup still the ancestor, that swallows
          // every keystroke before this input's own default action (typing)
          // can run, so the value never changed however long autoFocus kept
          // the caret here. Stopping propagation here keeps the keystroke
          // from ever reaching that handler; Escape still bubbles so the menu
          // can still be dismissed from the keyboard.
          onKeyDown={(e) => e.key !== "Escape" && e.stopPropagation()}
        />
        <DropdownMenuGroup className="max-h-[260px] overflow-y-auto">
          {visible.length === 0 && !inviteEmail && (
            <p className="text-muted-foreground py-2 text-center text-sm">
              {options.length === 0 && !allowEmail
                ? t`Everyone here already has access.`
                : t`Nothing matches.`}
            </p>
          )}
          {groups.length > 0 && (
            <DropdownMenuLabel className="text-xs">
              <Trans>Groups</Trans>
            </DropdownMenuLabel>
          )}
          {groups.map((g) => (
            <DropdownMenuItem
              key={g.id}
              // base-ui's Menu.Item exposes onClick, not onSelect: an onSelect
              // prop lands on the underlying div, where it means the
              // text-selection event and never fires for a click.
              onClick={() => pick(g)}
              className="flex items-center gap-2"
            >
              <Users className="text-muted-foreground h-4 w-4 shrink-0" />
              <span className="truncate">{g.label}</span>
              {g.kind === "group" && g.isEveryone && (
                <Lock className="text-muted-foreground ml-auto h-3 w-3 shrink-0" />
              )}
              {g.kind === "group" && g.isAdmin && (
                <ShieldCheck className="text-muted-foreground ml-auto h-3 w-3 shrink-0" />
              )}
            </DropdownMenuItem>
          ))}
          {people.length > 0 && (
            <DropdownMenuLabel className="text-xs">
              <Trans>People</Trans>
            </DropdownMenuLabel>
          )}
          {people.map((p) => (
            <DropdownMenuItem
              key={p.id}
              onClick={() => pick(p)}
              className="flex items-center gap-2"
            >
              <Avatar className="h-6 w-6">
                {p.kind === "user" && <AvatarImage src={p.avatarUrl} />}
                <AvatarFallback className="text-xs">
                  {p.label.slice(0, 2).toUpperCase()}
                </AvatarFallback>
              </Avatar>
              <div className="flex min-w-0 flex-col">
                <span className="truncate">{p.label}</span>
                {p.kind === "user" && (
                  <span className="text-muted-foreground truncate text-xs">{p.email}</span>
                )}
              </div>
            </DropdownMenuItem>
          ))}
          {inviteEmail && (
            <DropdownMenuItem
              onClick={() =>
                // eslint-disable-next-line lingui/no-unlocalized-strings -- Grantee discriminator
                pick({ kind: "email", id: inviteEmail, label: inviteEmail })
              }
              className="flex items-center gap-2"
            >
              <Mail className="text-muted-foreground h-4 w-4 shrink-0" />
              <span className="truncate">
                <Trans>Invite {inviteEmail}</Trans>
              </span>
            </DropdownMenuItem>
          )}
        </DropdownMenuGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
