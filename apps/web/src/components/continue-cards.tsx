import type { ReactNode } from "react";
import { useNavigate } from "@tanstack/react-router";
import { Trans } from "@lingui/react/macro";
import { opensInEditor, type ActivityKind, type RecentNode } from "@/lib/api";
import { openFile } from "@/lib/open-file";
import { useFormatters } from "@/lib/format";
import { PreviewModal } from "@/components/preview-modal";
import { Card, CardContent } from "@workspace/ui/components/card";
import { NodeIcon } from "@/components/node-icon";

/**
 * The caller's own last touch, as one whole sentence per verb.
 *
 * Not a verb fragment pasted between the space name and the timestamp: word
 * order is part of the sentence, so only the complete line can be translated.
 * Each entry renders `<Trans>`, which resolves at render time and therefore
 * follows a language switch even though the map is built once at import.
 */
type Caption = (parts: { spaceName: string; when: string }) => ReactNode;

const CAPTIONS: Record<ActivityKind, Caption> = {
  uploaded: ({ spaceName, when }) => (
    <Trans>
      {spaceName} · uploaded {when}
    </Trans>
  ),
  created: ({ spaceName, when }) => (
    <Trans>
      {spaceName} · created {when}
    </Trans>
  ),
  renamed: ({ spaceName, when }) => (
    <Trans>
      {spaceName} · renamed {when}
    </Trans>
  ),
  moved: ({ spaceName, when }) => (
    <Trans>
      {spaceName} · moved {when}
    </Trans>
  ),
  trashed: ({ spaceName, when }) => (
    <Trans>
      {spaceName} · trashed {when}
    </Trans>
  ),
  restored: ({ spaceName, when }) => (
    <Trans>
      {spaceName} · restored {when}
    </Trans>
  ),
  purged: ({ spaceName, when }) => (
    <Trans>
      {spaceName} · deleted {when}
    </Trans>
  ),
  shared: ({ spaceName, when }) => (
    <Trans>
      {spaceName} · shared {when}
    </Trans>
  ),
  unshared: ({ spaceName, when }) => (
    <Trans>
      {spaceName} · unshared {when}
    </Trans>
  ),
  reverted: ({ spaceName, when }) => (
    <Trans>
      {spaceName} · reverted {when}
    </Trans>
  ),
  invited: ({ spaceName, when }) => (
    <Trans>
      {spaceName} · invited {when}
    </Trans>
  ),
  joined: ({ spaceName, when }) => (
    <Trans>
      {spaceName} · joined {when}
    </Trans>
  ),
};

/** A kind this build does not know yet still reads as a line. */
const UNKNOWN: Caption = ({ spaceName, when }) => (
  <Trans>
    {spaceName} · updated {when}
  </Trans>
);

/**
 * The row of cards at the top of Home — the handful of files you were last in.
 *
 * Deliberately a different shape from the Recent table below it: this is for
 * resuming one specific thing you remember, so it shows few items large, while
 * the table is for finding something among many.
 */
export function ContinueCards({ nodes }: { nodes: RecentNode[] }) {
  const navigate = useNavigate();
  const f = useFormatters();

  if (nodes.length === 0) return null;

  const open = (node: RecentNode) => {
    if (node.kind === "dir") {
      void navigate({
        to: "/drive/$spaceId",
        params: { spaceId: node.space_id },
        search: { dir: node.id },
      });
    } else if (opensInEditor(node)) {
      openFile(node);
    } else {
      void PreviewModal.show({ node, siblings: nodes });
    }
  };

  return (
    <div className="grid grid-cols-2 gap-3 @xl/main:grid-cols-3 @4xl/main:grid-cols-5">
      {nodes.slice(0, 5).map((node) => {
        const caption = CAPTIONS[node.last_action] ?? UNKNOWN;
        return (
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
                  {caption({
                    spaceName: node.space_name,
                    when: f.relative(node.last_action_at),
                  })}
                </span>
              </div>
            </CardContent>
          </Card>
        );
      })}
    </div>
  );
}
