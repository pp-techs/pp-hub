import type { NodeViewProps } from "@tiptap/react";
import { NodeViewContent, NodeViewWrapper } from "@tiptap/react";
import { useLingui } from "@lingui/react/macro";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@workspace/ui/components/select";
import { cn } from "@workspace/ui/lib/utils";
import { mdocLowlight } from "@/lib/collab/mdoc-kit";

/** The select's value for a block with no `language` attribute. */
const PLAIN = "";

// Registered once, at import: the grammars a block can be highlighted with.
const LANGUAGES = mdocLowlight.listLanguages().sort();

/**
 * A code block with a language picker. The block's text is the node's own
 * content; the picker writes the `language` attribute, which is what the
 * markdown fence carries (` ```ts `) and what picks the highlighter. Plain
 * text clears the attribute rather than storing a name, so the fence stays
 * bare and the block stays uncolored.
 *
 * Read-only, the language shows as a label; there is nothing to pick.
 */
export function CodeBlockNodeView({ editor, node, updateAttributes }: NodeViewProps) {
  const { t } = useLingui();
  const language = typeof node.attrs.language === "string" ? node.attrs.language : PLAIN;
  // A fence can name a language by alias (`js`) or one lowlight does not
  // register; it stays selectable instead of silently reading as plain text.
  const names = language && !LANGUAGES.includes(language) ? [language, ...LANGUAGES] : LANGUAGES;
  const items = [
    { value: PLAIN, label: t`Plain text` },
    ...names.map((v) => ({ value: v, label: v })),
  ];

  return (
    <NodeViewWrapper className="group/code relative">
      <div
        contentEditable={false}
        className={cn(
          "absolute top-1.5 right-1.5 z-10 transition-opacity",
          !language && "opacity-0 group-focus-within/code:opacity-100 group-hover/code:opacity-100",
        )}
      >
        {editor.isEditable ? (
          <Select
            value={language}
            items={items}
            onValueChange={(value) =>
              updateAttributes({ language: typeof value === "string" && value ? value : null })
            }
          >
            <SelectTrigger
              size="sm"
              aria-label={t`Code language`}
              className="text-muted-foreground hover:text-foreground border-transparent bg-transparent px-1.5 dark:bg-transparent"
            >
              <SelectValue />
            </SelectTrigger>
            <SelectContent align="end" className="max-h-72">
              {items.map((item) => (
                <SelectItem key={item.value} value={item.value}>
                  {item.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        ) : language ? (
          <span className="text-muted-foreground px-1.5 text-xs">{language}</span>
        ) : null}
      </div>
      <pre>
        {/* eslint-disable-next-line lingui/no-unlocalized-strings -- element name */}
        <NodeViewContent<"code"> as="code" spellCheck={false} />
      </pre>
    </NodeViewWrapper>
  );
}
