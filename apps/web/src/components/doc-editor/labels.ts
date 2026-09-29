import type { I18n, MessageDescriptor } from "@lingui/core";
import { msg } from "@lingui/core/macro";
import {
  defaultSlashItems,
  placeholderKeyFor,
  type PlaceholderContext,
  type PlaceholderKey,
  type SlashItem,
} from "@slash-editor/core";

// slash-editor ships its menus, toolbar and placeholders with English copy
// baked into the item registries. The ids are stable, so the copy is
// replaced here by id and resolved at render (or, for the slash menu, at the
// moment the menu asks for its items) against the active catalog.

interface ItemCopy {
  title: MessageDescriptor;
  description?: MessageDescriptor;
}

/** Block conversions and insertable blocks, keyed by slash-editor item id. */
const BLOCKS: Record<string, ItemCopy> = {
  paragraph: { title: msg`Text`, description: msg`Plain paragraph` },
  "heading-1": { title: msg`Heading 1`, description: msg`Large section title` },
  "heading-2": { title: msg`Heading 2`, description: msg`Medium section title` },
  "heading-3": { title: msg`Heading 3`, description: msg`Small section title` },
  "bullet-list": { title: msg`Bulleted list`, description: msg`Unordered list` },
  "ordered-list": { title: msg`Numbered list`, description: msg`Ordered list` },
  "task-list": { title: msg`To-do list`, description: msg`Checklist with checkboxes` },
  blockquote: { title: msg`Quote`, description: msg`Capture a quotation` },
  callout: { title: msg`Callout`, description: msg`Highlighted aside` },
  toggle: { title: msg`Toggle list`, description: msg`Collapsible content` },
  "code-block": { title: msg`Code block`, description: msg`Monospaced code` },
  "toggle-heading-1": { title: msg`Toggle heading 1`, description: msg`Collapsible large title` },
  "toggle-heading-2": { title: msg`Toggle heading 2`, description: msg`Collapsible medium title` },
  "toggle-heading-3": { title: msg`Toggle heading 3`, description: msg`Collapsible small title` },
  mermaid: { title: msg`Mermaid diagram`, description: msg`Flowcharts, sequences, and more` },
  "horizontal-rule": { title: msg`Divider`, description: msg`Visual separator` },
  image: { title: msg`Image`, description: msg`Upload an image` },
  file: { title: msg`File`, description: msg`Upload or attach a file` },
  video: { title: msg`Video`, description: msg`Upload a video` },
  embed: { title: msg`Link card`, description: msg`Bookmark a web page` },
  table: { title: msg`Table`, description: msg`Rows and columns of cells` },
  columns: { title: msg`Columns`, description: msg`Side-by-side layout` },
};

/** Slash menu sections, keyed by the English group name slash-editor uses as the id. */
const GROUPS: Record<string, MessageDescriptor> = {
  "Basic blocks": msg`Basic blocks`,
  "Advanced blocks": msg`Advanced blocks`,
  Media: msg`Media`,
  Structure: msg`Structure`,
};

/** Bubble toolbar marks, keyed by item id. */
const MARKS: Record<string, MessageDescriptor> = {
  bold: msg`Bold`,
  italic: msg`Italic`,
  strike: msg`Strikethrough`,
  code: msg`Inline code`,
  link: msg`Link`,
};

const PLACEHOLDERS: Record<PlaceholderKey, MessageDescriptor> = {
  paragraph: msg`Type “/” for blocks…`,
  heading1: msg`Heading 1`,
  heading2: msg`Heading 2`,
  heading3: msg`Heading 3`,
  listItem: msg`List`,
  taskItem: msg`To-do`,
  blockquote: msg`Quote`,
  callout: msg`Callout`,
  details: msg`Toggle`,
  toggleHeading1: msg`Toggle heading 1`,
  toggleHeading2: msg`Toggle heading 2`,
  toggleHeading3: msg`Toggle heading 3`,
};

/** Slash items the v1 block set can store — every one this file has copy for. */
const V1_SLASH_ITEMS = defaultSlashItems.filter((item) => item.id in BLOCKS);

export function blockTitle(i18n: I18n, id: string, fallback: string): string {
  const copy = BLOCKS[id];
  return copy ? i18n._(copy.title) : fallback;
}

export function markLabel(i18n: I18n, id: string, fallback: string): string {
  const label = MARKS[id];
  return label ? i18n._(label) : fallback;
}

/**
 * The slash menu's items in the active language. The English title stays
 * among the aliases, so a query typed in English still finds a block in any
 * language. `runs` replaces what an item does, by id.
 */
export function localizedSlashItems(
  i18n: I18n,
  runs: Partial<Record<string, SlashItem["run"]>> = {},
): SlashItem[] {
  return V1_SLASH_ITEMS.map((item) => {
    const copy = BLOCKS[item.id]!;
    const group = GROUPS[item.group];
    return {
      ...item,
      title: i18n._(copy.title),
      description: copy.description ? i18n._(copy.description) : item.description,
      group: group ? i18n._(group) : item.group,
      aliases: [...(item.aliases ?? []), item.title.toLowerCase()],
      run: runs[item.id] ?? item.run,
    };
  });
}

export function localizedPlaceholder(i18n: I18n, context: PlaceholderContext): string | null {
  const key = placeholderKeyFor(context.node, context.parent);
  return key ? i18n._(PLACEHOLDERS[key]) : null;
}

const EXPAND = msg`Expand toggle`;
const COLLAPSE = msg`Collapse toggle`;

/** The toggle block's disclosure button carries only an accessible name; its marker is CSS. */
export function labelToggleButton(i18n: I18n, element: HTMLElement, isOpen: boolean): void {
  element.setAttribute("aria-label", i18n._(isOpen ? COLLAPSE : EXPAND));
}
