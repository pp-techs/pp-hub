import { useEffect, useState } from "react";
import { Trans, useLingui } from "@lingui/react/macro";
import { Check, Plus, X } from "lucide-react";
import type { Person } from "@pp-hub/app-sdk";
import { Avatar, AvatarFallback } from "@workspace/ui/components/avatar";
import { Button } from "@workspace/ui/components/button";
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from "@workspace/ui/components/command";
import { Popover, PopoverContent, PopoverTrigger } from "@workspace/ui/components/popover";
import { cn } from "@workspace/ui/lib/utils";
import { useBoard } from "./board-context";
import { useDebounced } from "./use-debounced";

/** The two letters a person's avatar shows. */
export const initials = (p: Pick<Person, "name" | "email">) =>
  (p.name || p.email || "?").slice(0, 2).toUpperCase();

/** A person's initials in a circle (the frame cannot load the host's avatar images). */
export function PersonAvatar({ person, className }: { person: Person; className?: string }) {
  return (
    <Avatar size="sm" title={person.name} aria-label={person.name} className={className}>
      <AvatarFallback className="text-[0.625rem]">{initials(person)}</AvatarFallback>
    </Avatar>
  );
}

/** Up to 20 people who can open the board matching `query`, searched while `enabled`. */
function usePeopleSearch(query: string, enabled: boolean) {
  const { app, rememberPeople } = useBoard();
  const [state, setState] = useState<{ query: string; people: Person[] } | null>(null);
  useEffect(() => {
    if (!enabled) return;
    let live = true;
    app.people.search(query, 20).then(
      (people) => {
        rememberPeople(people);
        if (live) setState({ query, people });
      },
      () => live && setState({ query, people: [] }),
    );
    return () => {
      live = false;
    };
  }, [app, query, enabled, rememberPeople]);
  return { people: state?.people ?? [], pending: state?.query !== query };
}

interface PeoplePickerProps {
  value: Person[];
  onChange: (people: Person[]) => void;
  disabled?: boolean;
}

/** The card's assignees: chips with a remove button, and a searchable popover to add more. */
export function PeoplePicker({ value, onChange, disabled }: PeoplePickerProps) {
  const { t } = useLingui();
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const settled = useDebounced(query.trim());
  const found = usePeopleSearch(settled, open);
  const chosen = new Set(value.map((p) => p.id));

  const toggle = (person: Person) =>
    onChange(chosen.has(person.id) ? value.filter((p) => p.id !== person.id) : [...value, person]);

  return (
    <div className="flex flex-wrap items-center gap-1.5">
      {value.map((person) => {
        const personName = person.name;
        return (
          <span
            key={person.id}
            className="bg-muted flex h-8 items-center gap-1.5 rounded-full pr-2.5 pl-0 text-xs"
          >
            <PersonAvatar person={person} />
            <span className="max-w-32 truncate">{person.name}</span>
            {!disabled && (
              <button
                type="button"
                className="text-muted-foreground hover:text-foreground"
                aria-label={t`Remove ${personName}`}
                onClick={() => toggle(person)}
              >
                <X className="size-3" />
              </button>
            )}
          </span>
        );
      })}
      {!disabled && (
        <Popover open={open} onOpenChange={setOpen}>
          <PopoverTrigger
            render={
              <Button
                type="button"
                variant="outline"
                size="sm"
                className="rounded-full border-dashed"
              >
                <Plus />
                <Trans>Assign</Trans>
              </Button>
            }
          />
          <PopoverContent align="start" className="w-72 p-0">
            <Command shouldFilter={false}>
              <CommandInput
                value={query}
                onValueChange={setQuery}
                placeholder={t`Search people…`}
              />
              <CommandList>
                <CommandEmpty>
                  {found.pending ? <Trans>Searching…</Trans> : <Trans>No one found</Trans>}
                </CommandEmpty>
                <CommandGroup>
                  {found.people.map((person) => (
                    <CommandItem
                      key={person.id}
                      value={person.id}
                      onSelect={() => toggle(person)}
                      data-checked={chosen.has(person.id)}
                    >
                      <PersonAvatar person={person} />
                      <span className="flex min-w-0 flex-col">
                        <span className="truncate">{person.name}</span>
                        {person.email && (
                          <span className="text-muted-foreground truncate text-[0.625rem]">
                            {person.email}
                          </span>
                        )}
                      </span>
                      <span
                        aria-hidden
                        className={cn(
                          "ml-auto flex size-4 shrink-0 items-center justify-center rounded-[4px]",
                          chosen.has(person.id) ? "bg-primary text-primary-foreground" : "border",
                        )}
                      >
                        {chosen.has(person.id) && <Check className="size-3" />}
                      </span>
                    </CommandItem>
                  ))}
                </CommandGroup>
              </CommandList>
            </Command>
          </PopoverContent>
        </Popover>
      )}
    </div>
  );
}
