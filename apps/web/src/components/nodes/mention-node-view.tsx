import { NodeViewWrapper, type NodeViewProps } from "@tiptap/react";

/* eslint-disable lingui/no-unlocalized-strings -- class names */
const CHIP =
  "bg-primary/10 text-primary rounded-sm px-1 py-px font-medium whitespace-nowrap in-data-selected:ring-2 in-data-selected:ring-ring";
/* eslint-enable lingui/no-unlocalized-strings */

/**
 * An inline `@` chip naming a person. A link to a document is a `pageLink`
 * (`PageLinkNodeView`): the server reads a stored `doc:<uuid>` mention as one.
 */
export function MentionNodeView({ node, selected }: NodeViewProps) {
  const id = (node.attrs.id as string | null) ?? "";
  const label = (node.attrs.label as string | null) ?? "";

  return (
    // eslint-disable-next-line lingui/no-unlocalized-strings -- element name
    <NodeViewWrapper as="span" data-selected={selected || undefined} data-mention-id={id}>
      <span className={CHIP}>@{label}</span>
    </NodeViewWrapper>
  );
}
