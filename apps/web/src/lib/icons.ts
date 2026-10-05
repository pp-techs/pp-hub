import {
  BoldIcon,
  ChevronRightIcon,
  CodeIcon,
  ColumnsIcon,
  FilePlusIcon,
  FileSymlinkIcon,
  FileTextIcon,
  Heading1Icon,
  Heading2Icon,
  Heading3Icon,
  ImageIcon,
  ItalicIcon,
  LinkIcon,
  ListChecksIcon,
  ListIcon,
  ListOrderedIcon,
  MessageSquareIcon,
  MinusIcon,
  PaperclipIcon,
  QuoteIcon,
  SquareDashedIcon,
  SmilePlusIcon,
  StrikethroughIcon,
  TableIcon,
  TypeIcon,
  UserIcon,
  VideoIcon,
  WorkflowIcon,
} from "lucide-react";
import type { LucideIcon } from "lucide-react";

/**
 * slash-editor's core names its icons by key and ships no components; these
 * are the keys the v1 block set, the page items, the bubble toolbar and the
 * mention menu use.
 */
const ICONS: Record<string, LucideIcon> = {
  bold: BoldIcon,
  "chevron-right": ChevronRightIcon,
  code: CodeIcon,
  columns: ColumnsIcon,
  "file-plus": FilePlusIcon,
  "file-symlink": FileSymlinkIcon,
  "file-text": FileTextIcon,
  "heading-1": Heading1Icon,
  "heading-2": Heading2Icon,
  "heading-3": Heading3Icon,
  image: ImageIcon,
  italic: ItalicIcon,
  link: LinkIcon,
  "list-checks": ListChecksIcon,
  list: ListIcon,
  "list-ordered": ListOrderedIcon,
  "message-square": MessageSquareIcon,
  minus: MinusIcon,
  paperclip: PaperclipIcon,
  quote: QuoteIcon,
  "face-slightly-smiling-plus": SmilePlusIcon,
  strikethrough: StrikethroughIcon,
  table: TableIcon,
  text: TypeIcon,
  user: UserIcon,
  video: VideoIcon,
  workflow: WorkflowIcon,
};

/** An unmapped key still renders an icon, never a blank slot. */
export function resolveIcon(key?: string): LucideIcon {
  return (key && ICONS[key]) || SquareDashedIcon;
}
