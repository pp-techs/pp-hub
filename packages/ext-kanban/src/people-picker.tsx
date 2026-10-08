import { useState } from "react";
import { Trans, useLingui } from "@lingui/react/macro";
import { Check, Plus, X } from "lucide-react";
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
import { useDebouncedValue, UserAvatar } from "@pp-hub/ext-sdk";
import type { Person } from "./api";
import { usePeople } from "./queries";

interface PeoplePickerProps {
  nodeId: string;
  value: Person[];
  onChange: (people: Person[]) => void;
  disabled?: boolean;
}

/** The card's assignees: chips with a remove button, and a searchable popover to add more. */
export function PeoplePicker({ nodeId, value, onChange, disabled }: PeoplePickerProps) {
  const { t } = useLingui();
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const settled = useDebouncedValue(query.trim());
  const people = usePeople(nodeId, settled, open);
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
            <UserAvatar name={person.name} email={person.email} size="sm" />
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
                  {people.isPending ? <Trans>Searching…</Trans> : <Trans>No one found</Trans>}
                </CommandEmpty>
                <CommandGroup>
                  {(people.data ?? []).map((person) => (
                    <CommandItem
                      key={person.id}
                      value={person.id}
                      onSelect={() => toggle(person)}
                      data-checked={chosen.has(person.id)}
                    >
                      <UserAvatar name={person.name} email={person.email} size="sm" />
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
                        className={
                          chosen.has(person.id)
                            ? "bg-primary text-primary-foreground ml-auto flex size-4 shrink-0 items-center justify-center rounded-[4px]"
                            : "ml-auto size-4 shrink-0 rounded-[4px] border"
                        }
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
