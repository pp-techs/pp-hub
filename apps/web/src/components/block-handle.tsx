import { defaultBlockTypes, type BlockTarget } from "@slash-editor/core";
import { useBlockDrag } from "@slash-editor/react";
import type { Editor } from "@tiptap/core";
import { Trans, useLingui } from "@lingui/react/macro";
import {
  ClipboardPasteIcon,
  CopyIcon,
  CopyPlusIcon,
  GripVerticalIcon,
  PlusIcon,
  Trash2Icon,
  TypeIcon,
} from "lucide-react";
import { useEffect, useReducer, useRef, useState } from "react";
import { Button } from "@workspace/ui/components/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuSub,
  DropdownMenuSubContent,
  DropdownMenuSubTrigger,
  DropdownMenuTrigger,
} from "@workspace/ui/components/dropdown-menu";
import { Tooltip, TooltipContent, TooltipTrigger } from "@workspace/ui/components/tooltip";
import { blockTitle } from "@/components/doc-editor/labels";
import { resolveIcon } from "@/lib/icons";

const GUTTER_OFFSET = 72;

/**
 * How far left of the text the pointer still counts as over a block — the
 * `drag.gutterWidth` the editor is built with. Core defaults to 48 px, which
 * ends short of the handle's first button (72 px out), so moving onto the +
 * cleared the hover and the handle vanished under the pointer.
 */
export const BLOCK_GUTTER_WIDTH = GUTTER_OFFSET + 8;

export function BlockHandle({ editor }: { editor: Editor }) {
  const { t, i18n } = useLingui();
  const drag = useBlockDrag(editor);
  const gripRef = useRef<HTMLButtonElement>(null);
  const gripOffsetRef = useRef<DOMRect | null>(null);
  const pointerRef = useRef<{ x: number; y: number } | null>(null);
  const [, rerender] = useReducer((count: number) => count + 1, 0);
  if (drag.menuTarget && drag.menuAnchor && gripRef.current && !gripOffsetRef.current) {
    const grip = gripRef.current.getBoundingClientRect();
    const row = drag.menuAnchor.getBoundingClientRect();
    gripOffsetRef.current = new DOMRect(
      grip.left - row.left,
      grip.top - row.top,
      grip.width,
      grip.height,
    );
  } else if (!drag.menuTarget && gripOffsetRef.current) {
    gripOffsetRef.current = null;
  }

  useEffect(() => {
    if (!drag.menuTarget) return;
    document.addEventListener("scroll", rerender, { capture: true, passive: true });
    return () => document.removeEventListener("scroll", rerender, { capture: true });
  }, [drag.menuTarget]);

  const activeAnchor = drag.menuTarget ? drag.menuAnchor : drag.hoverAnchor;
  const activeRect =
    drag.menuTarget || (drag.hovered && !drag.dragging)
      ? activeAnchor?.getBoundingClientRect()
      : null;
  const dropRect = drag.dragging ? drag.dropAnchor?.getBoundingClientRect() : null;
  const target = drag.menuTarget;

  return (
    <>
      {activeRect && (drag.menuTarget || drag.hovered) ? (
        <div
          className="fixed flex items-center gap-0.5"
          style={{
            top: activeRect.top,
            left: activeRect.left - GUTTER_OFFSET,
            height: activeRect.height,
          }}
        >
          <Tooltip>
            <TooltipTrigger
              render={
                <Button
                  type="button"
                  variant="ghost"
                  size="icon-sm"
                  aria-label={t`Insert block below`}
                  onClick={() => {
                    if (!drag.hovered) return;
                    const pos = drag.hovered.pos + drag.hovered.size;
                    editor
                      .chain()
                      .focus()
                      .insertContentAt(pos, { type: "paragraph" })
                      .setTextSelection(pos + 1)
                      .run();
                    editor.storage.slashCommand?.openAtCaret();
                  }}
                >
                  <PlusIcon />
                </Button>
              }
            />
            <TooltipContent>
              <Trans>Insert block below</Trans>
            </TooltipContent>
          </Tooltip>
          <Tooltip disabled={!!drag.menuTarget}>
            <TooltipTrigger
              render={
                <Button
                  ref={gripRef}
                  type="button"
                  variant="ghost"
                  size="icon-sm"
                  aria-label={t`Drag to reorder, click to open the block menu`}
                  className="cursor-grab active:cursor-grabbing"
                  {...drag.handleProps}
                  onPointerDown={(event) => {
                    pointerRef.current = { x: event.clientX, y: event.clientY };
                    drag.handleProps.onPointerDown(event);
                  }}
                >
                  <GripVerticalIcon />
                </Button>
              }
            />
            <TooltipContent>
              <Trans>Drag to move, click to open menu</Trans>
            </TooltipContent>
          </Tooltip>
        </div>
      ) : null}
      {dropRect ? (
        <div
          className="bg-primary fixed h-0.5 rounded-full"
          style={{ top: dropRect.top, left: dropRect.left, width: dropRect.width }}
        />
      ) : null}
      {drag.dragging ? (
        <BlockDragPreview target={drag.dragging} initialPointer={pointerRef.current} />
      ) : null}
      <DropdownMenu
        open={!!target}
        modal={false}
        onOpenChange={(open, details) => {
          if (!open && details?.reason !== "sibling-open") drag.closeMenu();
        }}
      >
        <DropdownMenuTrigger
          aria-hidden="true"
          tabIndex={-1}
          className="pointer-events-none absolute h-0 w-0 opacity-0"
        />
        <DropdownMenuContent
          anchor={{
            getBoundingClientRect: () => {
              const row = activeAnchor?.getBoundingClientRect();
              if (!row) return new DOMRect(0, 0, 0, 0);
              const offset = gripOffsetRef.current;
              return offset
                ? new DOMRect(row.left + offset.x, row.top + offset.y, offset.width, offset.height)
                : new DOMRect(row.left - GUTTER_OFFSET + 28, row.top, 28, 28);
            },
          }}
          align="start"
          side="right"
          sideOffset={8}
          className="w-68 min-w-64 max-w-72 p-1"
        >
          <DropdownMenuSub>
            <DropdownMenuSubTrigger>
              <TypeIcon />
              <span>
                <Trans>Turn into</Trans>
              </span>
            </DropdownMenuSubTrigger>
            <DropdownMenuSubContent className="w-56 p-1">
              <DropdownMenuGroup>
                {defaultBlockTypes
                  .filter((type) => type.when?.(editor) ?? true)
                  .map((type) => {
                    const Icon = resolveIcon(type.icon);
                    return (
                      <DropdownMenuItem
                        key={type.id}
                        onClick={() => {
                          if (!target) return;
                          const pos = target.pos;
                          drag.closeMenu();
                          type.convert({ editor, pos });
                        }}
                      >
                        <Icon />
                        <span>{blockTitle(i18n, type.id, type.title)}</span>
                      </DropdownMenuItem>
                    );
                  })}
              </DropdownMenuGroup>
            </DropdownMenuSubContent>
          </DropdownMenuSub>
          <DropdownMenuSeparator />
          <DropdownMenuGroup>
            <DropdownMenuItem
              onClick={() => {
                if (!target) return;
                editor.commands.copyBlock({ pos: target.pos, size: target.size });
                drag.closeMenu();
              }}
            >
              <CopyIcon />
              <span>
                <Trans>Copy</Trans>
              </span>
            </DropdownMenuItem>
            <DropdownMenuItem
              onClick={() => {
                if (!target) return;
                editor.commands.pasteBlockBelow({ pos: target.pos, size: target.size });
                drag.closeMenu();
              }}
            >
              <ClipboardPasteIcon />
              <span>
                <Trans>Paste below</Trans>
              </span>
            </DropdownMenuItem>
            <DropdownMenuItem
              onClick={() => {
                if (!target) return;
                editor.commands.duplicateBlock({ pos: target.pos, size: target.size });
                drag.closeMenu();
              }}
            >
              <CopyPlusIcon />
              <span>
                <Trans>Duplicate</Trans>
              </span>
            </DropdownMenuItem>
            <DropdownMenuItem
              variant="destructive"
              onClick={() => {
                if (!target) return;
                editor.commands.deleteBlock({ pos: target.pos, size: target.size });
                drag.closeMenu();
              }}
            >
              <Trash2Icon />
              <span>
                <Trans>Delete</Trans>
              </span>
            </DropdownMenuItem>
          </DropdownMenuGroup>
        </DropdownMenuContent>
      </DropdownMenu>
    </>
  );
}

/* eslint-disable lingui/no-unlocalized-strings -- the drag preview below handles DOM
   attributes, selectors and transforms, never copy */
function BlockDragPreview({
  target,
  initialPointer,
}: {
  target: BlockTarget;
  initialPointer?: { x: number; y: number } | null;
}) {
  const containerRef = useRef<HTMLDivElement>(null);
  const contentRef = useRef<HTMLDivElement>(null);
  const [isTruncated, setIsTruncated] = useState(false);

  const fallbackPointer =
    initialPointer ??
    (() => {
      const rect = target.getClientRect();
      return rect ? { x: rect.left, y: rect.top } : null;
    })();

  useEffect(() => {
    const container = containerRef.current;
    if (!container) return;

    let currentX = fallbackPointer?.x ?? 0;
    let currentY = fallbackPointer?.y ?? 0;
    let rafId = 0;

    const updateTransform = () => {
      rafId = 0;
      if (container) {
        container.style.transform = `translate3d(${currentX + 12}px, ${currentY + 12}px, 0)`;
      }
    };

    if (fallbackPointer) {
      updateTransform();
    }

    const onPointerMove = (e: PointerEvent) => {
      currentX = e.clientX;
      currentY = e.clientY;
      if (!rafId) {
        rafId = requestAnimationFrame(updateTransform);
      }
    };

    window.addEventListener("pointermove", onPointerMove, { passive: true });
    return () => {
      window.removeEventListener("pointermove", onPointerMove);
      if (rafId) cancelAnimationFrame(rafId);
    };
  }, [fallbackPointer]);

  useEffect(() => {
    const content = contentRef.current;
    const source = target.getDOMNode();
    if (!content || !source) return;

    const clone = source.cloneNode(true) as HTMLElement;
    clone.removeAttribute("data-dragging");
    clone.removeAttribute("contenteditable");
    clone.setAttribute("inert", "");
    clone.setAttribute("aria-hidden", "true");

    const editables = clone.querySelectorAll("[contenteditable]");
    for (const el of editables) {
      el.removeAttribute("contenteditable");
    }

    const originalCheckboxes = source.querySelectorAll<HTMLInputElement>('input[type="checkbox"]');
    const clonedCheckboxes = clone.querySelectorAll<HTMLInputElement>('input[type="checkbox"]');
    originalCheckboxes.forEach((orig, idx) => {
      if (clonedCheckboxes[idx]) {
        clonedCheckboxes[idx].checked = orig.checked;
        clonedCheckboxes[idx].defaultChecked = orig.checked;
      }
    });

    content.innerHTML = "";

    if (clone.tagName === "LI") {
      const isTask =
        clone.getAttribute("data-type") === "taskItem" ||
        clone.getAttribute("data-block-type") === "taskItem";
      if (!isTask) {
        const listWrapper = document.createElement("ul");
        listWrapper.className = "list-disc pl-5 my-0";
        listWrapper.appendChild(clone);
        content.appendChild(listWrapper);
      } else {
        content.appendChild(clone);
      }
    } else {
      content.appendChild(clone);
    }

    if (content.scrollHeight > 192) {
      setIsTruncated(true);
    }
  }, [target]);

  const initialStyle = fallbackPointer
    ? { transform: `translate3d(${fallbackPointer.x + 12}px, ${fallbackPointer.y + 12}px, 0)` }
    : { transform: "translate3d(-9999px, -9999px, 0)" };

  return (
    <div
      ref={containerRef}
      data-block-drag-preview
      className="pointer-events-none fixed top-0 left-0 z-50 transition-transform duration-75 ease-out select-none will-change-transform"
      style={initialStyle}
    >
      <div className="relative max-h-48 max-w-[min(540px,80vw)] min-w-[120px] overflow-hidden rounded-lg border border-border/80 bg-background/90 p-3 shadow-2xl backdrop-blur-sm rotate-[1.5deg] scale-[1.02] opacity-85">
        <div ref={contentRef} className="slash-content text-sm" />
        {isTruncated ? (
          <div className="pointer-events-none absolute inset-x-0 bottom-0 h-10 bg-gradient-to-t from-background via-background/60 to-transparent" />
        ) : null}
      </div>
    </div>
  );
}
