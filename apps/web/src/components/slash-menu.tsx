import { useActiveItemScroll, useSlashMenu } from "@slash-editor/react";
import type { SlashItem } from "@slash-editor/core";
import type { Editor } from "@tiptap/core";
import { Trans } from "@lingui/react/macro";
import { useMemo } from "react";
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandItem,
  CommandList,
  CommandShortcut,
} from "@workspace/ui/components/command";
import { Popover, PopoverContent } from "@workspace/ui/components/popover";
import { resolveIcon } from "@/lib/icons";

/**
 * The `/` block menu. Its items arrive already in the reader's language —
 * the editor's slash extension is fed `localizedSlashItems`, so ranking runs
 * against the titles shown here, not against the English registry.
 */
export function SlashMenu({ editor }: { editor: Editor }) {
  const menu = useSlashMenu(editor);
  const listRef = useActiveItemScroll(menu.activeItem?.id);
  const query = menu.query;

  const groups = useMemo(() => {
    const byGroup = new Map<string, SlashItem[]>();
    for (const item of menu.items) {
      const existing = byGroup.get(item.group);
      if (existing) existing.push(item);
      else byGroup.set(item.group, [item]);
    }
    return [...byGroup];
  }, [menu.items]);

  return (
    <Popover
      open={menu.open}
      onOpenChange={(open) => {
        if (!open) menu.close();
      }}
    >
      <PopoverContent
        anchor={menu.anchor ?? undefined}
        align="start"
        side="bottom"
        sideOffset={6}
        // The editor keeps focus: the suggestion plugin drives navigation.
        initialFocus={false}
        finalFocus={false}
        className="w-72 p-0"
      >
        <Command
          shouldFilter={false}
          value={menu.activeItem?.id ?? ""}
          onValueChange={(value) => {
            menu.setActiveIndex(menu.items.findIndex((item) => item.id === value));
          }}
        >
          <CommandList ref={listRef}>
            <CommandEmpty>
              <Trans>No blocks match “{query}”.</Trans>
            </CommandEmpty>
            {groups.map(([group, items]) => (
              <CommandGroup key={group} heading={group}>
                {items.map((item) => {
                  const Icon = resolveIcon(item.icon);
                  return (
                    <CommandItem
                      key={item.id}
                      value={item.id}
                      // The row is one line; the description survives as a tooltip.
                      title={item.description}
                      onSelect={() => menu.select(menu.items.indexOf(item))}
                    >
                      <Icon />
                      <span className="truncate">{item.title}</span>
                      {item.shortcut ? <CommandShortcut>{item.shortcut}</CommandShortcut> : null}
                    </CommandItem>
                  );
                })}
              </CommandGroup>
            ))}
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  );
}
