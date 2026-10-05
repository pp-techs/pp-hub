import { plural } from "@lingui/core/macro";
import { useLingui } from "@lingui/react/macro";
import { FileTextIcon } from "lucide-react";
import { Badge } from "@workspace/ui/components/badge";
import type { Node } from "@/lib/api";

/**
 * How many sub-pages a document holds, beside its name in the listing. The
 * listing shows only top-level documents; the pages under one live in the
 * editor's page tree, so this is the only sign of them here. Nothing for a
 * document without any, or anything that is not a document.
 */
export function SubPageCount({ node }: { node: Node }) {
  const { t } = useLingui();
  const count = node.child_page_count ?? 0;
  if (count <= 0) return null;
  const label = t`${plural(count, { one: "# sub-page", other: "# sub-pages" })}`;

  return (
    <Badge variant="secondary" title={label} aria-label={label} className="tabular-nums">
      <FileTextIcon data-icon="inline-start" />
      {count}
    </Badge>
  );
}
