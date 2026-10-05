import { useActiveItemScroll, useEmoji } from "@slash-editor/react";
import type { Editor } from "@tiptap/core";
import { Trans } from "@lingui/react/macro";
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandItem,
  CommandList,
} from "@workspace/ui/components/command";
import { Popover, PopoverContent } from "@workspace/ui/components/popover";

/**
 * The `:` menu: emoji matching what was typed after the colon. The editor
 * keeps focus; the suggestion plugin drives the highlight. Only the glyph is
 * drawn — every entry in the kit's dataset has one, and its fallback images
 * live on a CDN the CSP does not admit.
 */
export function EmojiMenu({ editor }: { editor: Editor }) {
  const menu = useEmoji(editor);
  const listRef = useActiveItemScroll(menu.activeItem?.name);

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
        initialFocus={false}
        finalFocus={false}
        className="w-64 p-0"
      >
        <Command
          shouldFilter={false}
          value={menu.activeItem?.name ?? ""}
          onValueChange={(value) => {
            menu.setActiveIndex(menu.items.findIndex((item) => item.name === value));
          }}
        >
          <CommandList ref={listRef}>
            <CommandEmpty>
              <Trans>No emoji matches.</Trans>
            </CommandEmpty>
            {menu.items.length > 0 ? (
              <CommandGroup>
                {menu.items.map((item, index) => (
                  <CommandItem
                    key={item.name}
                    value={item.name}
                    onSelect={() => menu.select(index)}
                  >
                    <span aria-hidden className="flex size-5 items-center justify-center text-base">
                      {item.emoji}
                    </span>
                    <span className="truncate">:{item.shortcodes[0] ?? item.name}:</span>
                  </CommandItem>
                ))}
              </CommandGroup>
            ) : null}
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  );
}
