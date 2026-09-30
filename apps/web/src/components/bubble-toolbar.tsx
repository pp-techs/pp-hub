import { defaultBlockTypes, type BubbleToolbarItem } from "@slash-editor/core";
import { useBubbleToolbar } from "@slash-editor/react";
import type { Editor } from "@tiptap/core";
import { useLingui } from "@lingui/react/macro";
import { ChevronDownIcon, MessageSquarePlusIcon, RemoveFormattingIcon } from "lucide-react";
import { Button } from "@workspace/ui/components/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@workspace/ui/components/dropdown-menu";
import { Popover, PopoverContent } from "@workspace/ui/components/popover";
import { Separator } from "@workspace/ui/components/separator";
import { Toggle } from "@workspace/ui/components/toggle";
import { Tooltip, TooltipContent, TooltipTrigger } from "@workspace/ui/components/tooltip";
import { blockTitle, markLabel } from "@/components/doc-editor/labels";
import { resolveIcon } from "@/lib/icons";

function BlockTypeDropdown({ editor }: { editor: Editor }) {
  const { t, i18n } = useLingui();
  const active = defaultBlockTypes.find((type) => type.isActive(editor));
  const ActiveIcon = resolveIcon(active?.icon);

  return (
    <DropdownMenu modal={false}>
      <DropdownMenuTrigger
        render={
          <Button variant="ghost" size="sm" onMouseDown={(event) => event.preventDefault()}>
            <ActiveIcon data-icon="inline-start" />
            <span>{active ? blockTitle(i18n, active.id, active.title) : t`Text`}</span>
            <ChevronDownIcon data-icon="inline-end" />
          </Button>
        }
      />
      <DropdownMenuContent align="start" className="w-48">
        <DropdownMenuGroup>
          {defaultBlockTypes
            .filter((type) => type.when?.(editor) ?? true)
            .map((type) => {
              const Icon = resolveIcon(type.icon);
              return (
                <DropdownMenuItem
                  key={type.id}
                  onMouseDown={(event) => event.preventDefault()}
                  onClick={() => type.convert({ editor })}
                >
                  <Icon />
                  <span>{blockTitle(i18n, type.id, type.title)}</span>
                </DropdownMenuItem>
              );
            })}
        </DropdownMenuGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

function ToolbarButton({ editor, item }: { editor: Editor; item: BubbleToolbarItem }) {
  const { i18n } = useLingui();
  const Icon = resolveIcon(item.icon);
  const label = markLabel(i18n, item.id, item.label);

  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <Toggle
            aria-label={label}
            pressed={item.isActive(editor)}
            onMouseDown={(event) => event.preventDefault()}
            onPressedChange={() => item.run(editor)}
          >
            <Icon />
          </Toggle>
        }
      />
      <TooltipContent className="flex items-center gap-1.5">
        <span>{label}</span>
        {item.shortcut ? <kbd className="text-muted-foreground">{item.shortcut}</kbd> : null}
      </TooltipContent>
    </Tooltip>
  );
}

/**
 * Inline formatting over a selection: block type, the v1 marks (bold,
 * italic, strike, code, link — nothing the markdown cannot keep),
 * clear-formatting, and — when the editor has a comment store — a comment
 * on the selected text.
 */
export function BubbleToolbar({ editor }: { editor: Editor }) {
  const { t } = useLingui();
  const toolbar = useBubbleToolbar(editor);

  return (
    <Popover open={toolbar.open}>
      <PopoverContent
        anchor={toolbar.anchor ?? undefined}
        align="center"
        side="top"
        sideOffset={8}
        initialFocus={false}
        finalFocus={false}
        className="w-auto flex-row items-center gap-0.5 p-1"
      >
        <BlockTypeDropdown editor={editor} />
        <Separator orientation="vertical" className="mx-0.5 h-4 data-vertical:self-center" />
        {toolbar.items.map((item) => (
          <ToolbarButton key={item.id} editor={editor} item={item} />
        ))}
        <Tooltip>
          <TooltipTrigger
            render={
              <Button
                variant="ghost"
                size="icon-sm"
                aria-label={t`Clear formatting`}
                onMouseDown={(event) => event.preventDefault()}
                onClick={() => editor.chain().focus().unsetAllMarks().run()}
              >
                <RemoveFormattingIcon />
              </Button>
            }
          />
          <TooltipContent>{t`Clear formatting`}</TooltipContent>
        </Tooltip>
        {editor.storage.comment?.store ? (
          <>
            <Separator orientation="vertical" className="mx-0.5 h-4 data-vertical:self-center" />
            <Tooltip>
              <TooltipTrigger
                render={
                  <Button
                    variant="ghost"
                    size="sm"
                    onMouseDown={(event) => event.preventDefault()}
                    onClick={() => editor.commands.openCommentComposer()}
                  >
                    <MessageSquarePlusIcon data-icon="inline-start" />
                    <span>{t`Comment`}</span>
                  </Button>
                }
              />
              <TooltipContent>{t`Comment on the selected text`}</TooltipContent>
            </Tooltip>
          </>
        ) : null}
      </PopoverContent>
    </Popover>
  );
}
