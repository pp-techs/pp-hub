import { NodeViewWrapper, type NodeViewProps } from "@tiptap/react";
import { Link } from "@tanstack/react-router";
import { FileTextIcon } from "lucide-react";
import { cn } from "@workspace/ui/lib/utils";
import { MENTION_DOC } from "@/lib/collab/mentions";

/* eslint-disable lingui/no-unlocalized-strings -- class names */
const CHIP =
  "bg-primary/10 text-primary rounded-sm px-1 py-px font-medium whitespace-nowrap no-underline in-data-selected:ring-2 in-data-selected:ring-ring";
/* eslint-enable lingui/no-unlocalized-strings */

/**
 * An inline `@` chip. A person is a label; a document links to its editor,
 * where access is decided as for any other link — a reader who cannot open
 * it gets the usual "not available" page.
 */
export function MentionNodeView({ node, selected }: NodeViewProps) {
  const id = (node.attrs.id as string | null) ?? "";
  const label = (node.attrs.label as string | null) ?? "";

  return (
    // eslint-disable-next-line lingui/no-unlocalized-strings -- element name
    <NodeViewWrapper as="span" data-selected={selected || undefined} data-mention-id={id}>
      {id.startsWith(MENTION_DOC) ? (
        <Link
          to="/edit/$nodeId"
          params={{ nodeId: id.slice(MENTION_DOC.length) }}
          draggable={false}
          className={cn(CHIP, "inline-flex items-baseline gap-0.5")}
        >
          <FileTextIcon className="size-3 shrink-0 self-center" />
          {label}
        </Link>
      ) : (
        <span className={CHIP}>@{label}</span>
      )}
    </NodeViewWrapper>
  );
}
