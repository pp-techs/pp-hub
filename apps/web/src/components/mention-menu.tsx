import { useActiveItemScroll, useMention } from "@slash-editor/react";
import type { Editor } from "@tiptap/core";
import { Trans } from "@lingui/react/macro";
import { Loader2 } from "lucide-react";
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandItem,
  CommandList,
} from "@workspace/ui/components/command";
import { Popover, PopoverContent } from "@workspace/ui/components/popover";
import { resolveIcon } from "@/lib/icons";

/**
 * The `@` menu: people in the document's space and markdown documents, as
 * the editor's mention provider answers for what was typed after the `@`.
 * The editor keeps focus; the suggestion plugin drives the highlight.
 */
export function MentionMenu({ editor }: { editor: Editor }) {
  const menu = useMention(editor);
  const listRef = useActiveItemScroll(menu.activeItem?.id);

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
            {menu.loading && menu.items.length === 0 ? (
              <div role="status" className="text-muted-foreground flex justify-center py-6">
                <Loader2 className="size-4 animate-spin" />
              </div>
            ) : (
              <CommandEmpty>
                <Trans>Nobody or nothing matches.</Trans>
              </CommandEmpty>
            )}
            {menu.items.length > 0 ? (
              <CommandGroup>
                {menu.items.map((item, index) => {
                  const Icon = resolveIcon(item.icon);
                  return (
                    <CommandItem key={item.id} value={item.id} onSelect={() => menu.select(index)}>
                      <Icon />
                      <span className="truncate">{item.label}</span>
                      {item.description ? (
                        <span className="text-muted-foreground ml-auto truncate text-xs">
                          {item.description}
                        </span>
                      ) : null}
                    </CommandItem>
                  );
                })}
              </CommandGroup>
            ) : null}
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  );
}
