import { Trans } from "@lingui/react/macro";
import { Badge } from "@workspace/ui/components/badge";
import type { Node } from "@/lib/api";
import { useExtensions } from "@/lib/queries";

/**
 * Marks an extension node whose app is switched off for its space — by the
 * instance admin or the space admin. The node still lists and its data is
 * kept; opening it answers 403 until the app is back on. Nothing for any
 * other node, and no request is made for one.
 */
export function ExtOffBadge({ node }: { node: Node }) {
  const isExt = node.kind === "ext";
  const extensions = useExtensions(node.space_id, isExt);
  if (!isExt || !node.ext_kind) return null;
  const ext = extensions.data?.find((e) => e.node_kinds.includes(node.ext_kind!));
  if (!ext || ext.enabled) return null;
  return (
    <Badge variant="outline" className="text-muted-foreground shrink-0">
      <Trans>App disabled</Trans>
    </Badge>
  );
}
