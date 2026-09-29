import type { ChainedCommands, Editor } from "@tiptap/core";
import { useEditorState } from "@tiptap/react";
import { useLingui } from "@lingui/react/macro";
import {
  BetweenHorizontalEndIcon,
  BetweenHorizontalStartIcon,
  BetweenVerticalEndIcon,
  BetweenVerticalStartIcon,
  ChevronDownIcon,
  Columns3Icon,
  PanelTopIcon,
  Rows3Icon,
  Trash2Icon,
} from "lucide-react";
import type { ReactElement } from "react";
import { Button } from "@workspace/ui/components/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@workspace/ui/components/dropdown-menu";
import { Popover, PopoverContent } from "@workspace/ui/components/popover";
import { Separator } from "@workspace/ui/components/separator";
import { Toggle } from "@workspace/ui/components/toggle";
import { Tooltip, TooltipContent, TooltipTrigger } from "@workspace/ui/components/tooltip";

/** Keeps the caret in the cell: a toolbar press must not take focus from the editor. */
const keepFocus = (event: { preventDefault: () => void }) => event.preventDefault();

/** The table holding the selection — its position and whether its first row is a header. */
function tableAtSelection(editor: Editor): { pos: number; header: boolean } | null {
  if (!editor.isEditable) return null;
  const { $from } = editor.state.selection;
  for (let depth = $from.depth; depth > 0; depth--) {
    const node = $from.node(depth);
    if (node.type.name !== "table") continue;
    return {
      pos: $from.before(depth),
      header: node.firstChild?.firstChild?.type.name === "tableHeader",
    };
  }
  return null;
}

/**
 * Row, column and header controls over the table the caret is in — Tiptap's
 * table commands, which slash-editor ships without any UI. Shown only while
 * the selection is inside a table and the document is editable.
 */
export function TableControls({ editor }: { editor: Editor }) {
  const { t } = useLingui();
  const table = useEditorState({ editor, selector: ({ editor: e }) => tableAtSelection(e) });
  const anchor = table ? (editor.view.nodeDOM(table.pos) as HTMLElement | null) : null;
  const run = (command: (chain: ChainedCommands) => ChainedCommands) =>
    command(editor.chain().focus()).run();

  return (
    <Popover open={!!table && !!anchor}>
      <PopoverContent
        anchor={anchor ?? undefined}
        align="end"
        side="top"
        sideOffset={4}
        initialFocus={false}
        finalFocus={false}
        className="w-auto flex-row items-center gap-0.5 p-1"
      >
        <DropdownMenu modal={false}>
          <DropdownMenuTrigger
            render={
              <Button variant="ghost" size="sm" onMouseDown={keepFocus}>
                <Rows3Icon data-icon="inline-start" />
                <span>{t`Row`}</span>
                <ChevronDownIcon data-icon="inline-end" />
              </Button>
            }
          />
          <DropdownMenuContent align="start" className="w-48">
            <DropdownMenuGroup>
              <DropdownMenuItem
                onMouseDown={keepFocus}
                onClick={() => run((c) => c.addRowBefore())}
              >
                <BetweenHorizontalStartIcon />
                <span>{t`Insert row above`}</span>
              </DropdownMenuItem>
              <DropdownMenuItem onMouseDown={keepFocus} onClick={() => run((c) => c.addRowAfter())}>
                <BetweenHorizontalEndIcon />
                <span>{t`Insert row below`}</span>
              </DropdownMenuItem>
            </DropdownMenuGroup>
            <DropdownMenuSeparator />
            <DropdownMenuGroup>
              <DropdownMenuItem
                variant="destructive"
                onMouseDown={keepFocus}
                onClick={() => run((c) => c.deleteRow())}
              >
                <Trash2Icon />
                <span>{t`Delete row`}</span>
              </DropdownMenuItem>
            </DropdownMenuGroup>
          </DropdownMenuContent>
        </DropdownMenu>
        <DropdownMenu modal={false}>
          <DropdownMenuTrigger
            render={
              <Button variant="ghost" size="sm" onMouseDown={keepFocus}>
                <Columns3Icon data-icon="inline-start" />
                <span>{t`Column`}</span>
                <ChevronDownIcon data-icon="inline-end" />
              </Button>
            }
          />
          <DropdownMenuContent align="start" className="w-48">
            <DropdownMenuGroup>
              <DropdownMenuItem
                onMouseDown={keepFocus}
                onClick={() => run((c) => c.addColumnBefore())}
              >
                <BetweenVerticalStartIcon />
                <span>{t`Insert column left`}</span>
              </DropdownMenuItem>
              <DropdownMenuItem
                onMouseDown={keepFocus}
                onClick={() => run((c) => c.addColumnAfter())}
              >
                <BetweenVerticalEndIcon />
                <span>{t`Insert column right`}</span>
              </DropdownMenuItem>
            </DropdownMenuGroup>
            <DropdownMenuSeparator />
            <DropdownMenuGroup>
              <DropdownMenuItem
                variant="destructive"
                onMouseDown={keepFocus}
                onClick={() => run((c) => c.deleteColumn())}
              >
                <Trash2Icon />
                <span>{t`Delete column`}</span>
              </DropdownMenuItem>
            </DropdownMenuGroup>
          </DropdownMenuContent>
        </DropdownMenu>
        <Separator orientation="vertical" className="mx-0.5 h-4" />
        <ControlTooltip label={t`Header row`}>
          <Toggle
            aria-label={t`Header row`}
            pressed={!!table?.header}
            onMouseDown={keepFocus}
            onPressedChange={() => run((c) => c.toggleHeaderRow())}
          >
            <PanelTopIcon />
          </Toggle>
        </ControlTooltip>
        <ControlTooltip label={t`Delete table`}>
          <Button
            variant="ghost"
            size="icon-sm"
            aria-label={t`Delete table`}
            onMouseDown={keepFocus}
            onClick={() => run((c) => c.deleteTable())}
          >
            <Trash2Icon />
          </Button>
        </ControlTooltip>
      </PopoverContent>
    </Popover>
  );
}

function ControlTooltip({ label, children }: { label: string; children: ReactElement }) {
  return (
    <Tooltip>
      <TooltipTrigger render={children} />
      <TooltipContent>{label}</TooltipContent>
    </Tooltip>
  );
}
