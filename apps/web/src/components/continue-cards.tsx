import { useNavigate } from "@tanstack/react-router";
import {
  opensInEditor,
  fmtRelative,
  openEditor,
  type ActivityKind,
  type RecentNode,
} from "@/lib/api";
import { PreviewModal } from "@/components/preview-modal";
import { Card, CardContent } from "@workspace/ui/components/card";
import { NodeIcon } from "@/components/node-icon";

/** Past-tense phrasing for the caller's own last touch. */
const VERBS: Record<ActivityKind, string> = {
  uploaded: "uploaded",
  created: "created",
  renamed: "renamed",
  moved: "moved",
  trashed: "trashed",
  restored: "restored",
  purged: "deleted",
  shared: "shared",
  unshared: "unshared",
  reverted: "reverted",
  invited: "invited",
  joined: "joined",
};

/**
 * The row of cards at the top of Home — the handful of files you were last in.
 *
 * Deliberately a different shape from the Recent table below it: this is for
 * resuming one specific thing you remember, so it shows few items large, while
 * the table is for finding something among many.
 */
export function ContinueCards({ nodes }: { nodes: RecentNode[] }) {
  const navigate = useNavigate();

  if (nodes.length === 0) return null;

  const open = (node: RecentNode) => {
    if (node.kind === "dir") {
      void navigate({
        to: "/drive/$spaceId",
        params: { spaceId: node.space_id },
        search: { dir: node.id },
      });
    } else if (opensInEditor(node)) {
      openEditor(node);
    } else {
      void PreviewModal.show({ node, siblings: nodes });
    }
  };

  return (
    <div className="grid grid-cols-2 gap-3 @xl/main:grid-cols-3 @4xl/main:grid-cols-5">
      {nodes.slice(0, 5).map((node) => (
        <Card
          key={node.id}
          role="button"
          tabIndex={0}
          onClick={() => open(node)}
          onKeyDown={(e) => {
            if (e.key === "Enter" || e.key === " ") {
              e.preventDefault();
              open(node);
            }
          }}
          className="hover:bg-muted/50 cursor-pointer gap-0 py-3 transition-colors focus-visible:ring-2 focus-visible:ring-ring/30 focus-visible:outline-none"
        >
          <CardContent className="flex items-center gap-2 px-3">
            <NodeIcon node={node} className="size-8 shrink-0" />
            <div className="flex min-w-0 flex-col text-left">
              <span className="truncate text-xs font-medium">{node.name}</span>
              <span className="text-muted-foreground truncate text-[10px]">
                {node.space_name} · {VERBS[node.last_action] ?? node.last_action}{" "}
                {fmtRelative(node.last_action_at)}
              </span>
            </div>
          </CardContent>
        </Card>
      ))}
    </div>
  );
}
