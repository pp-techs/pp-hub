import { useNavigate } from "@tanstack/react-router";
import { Trans } from "@lingui/react/macro";
import { Button } from "@workspace/ui/components/button";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@workspace/ui/components/table";
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@workspace/ui/components/empty";
import { opensInEditor, type SearchResult } from "@/lib/api";
import { openFile } from "@/lib/open-file";
import { useFormatters } from "@/lib/format";
import { PreviewModal } from "@/components/preview-modal";
import { ExtOffBadge } from "@/components/ext-off-badge";
import { NodeIcon } from "@/components/node-icon";
import { SkeletonTableRows } from "@/components/loading";

interface SpaceNodeTableProps {
  nodes: SearchResult[];
  isPending: boolean;
  emptyIcon: React.ReactNode;
  emptyTitle: string;
  emptyDescription: string;
  /** Extra column between Location and Modified — "Action" on Recent. */
  extraColumn?: { header: string; cell: (node: SearchResult) => React.ReactNode };
}

/**
 * A flat listing of files gathered from across one space — Recent, Starred.
 *
 * Distinct from `NodeTable`, which lists the contents of one folder and can
 * leave the location implicit. Here the rows come from anywhere in the space,
 * so each says which folder holds it; opening a folder row navigates there.
 */
export function SpaceNodeTable({
  nodes,
  isPending,
  emptyIcon,
  emptyTitle,
  emptyDescription,
  extraColumn,
}: SpaceNodeTableProps) {
  const navigate = useNavigate();
  const f = useFormatters();

  const open = (node: SearchResult) => {
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

  if (!isPending && nodes.length === 0) {
    return (
      <Empty className="py-12">
        <EmptyHeader>
          <EmptyMedia>{emptyIcon}</EmptyMedia>
          <EmptyTitle>{emptyTitle}</EmptyTitle>
          <EmptyDescription>{emptyDescription}</EmptyDescription>
        </EmptyHeader>
      </Empty>
    );
  }

  return (
    <Table className="text-xs">
      <TableHeader>
        <TableRow>
          <TableHead>
            <Trans>Name</Trans>
          </TableHead>
          <TableHead className="w-40">
            <Trans>Location</Trans>
          </TableHead>
          {extraColumn && <TableHead className="w-36">{extraColumn.header}</TableHead>}
          <TableHead className="w-32">
            <Trans>Modified</Trans>
          </TableHead>
          <TableHead className="w-24">
            <Trans>Size</Trans>
          </TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {isPending ? (
          <SkeletonTableRows columns={extraColumn ? 5 : 4} />
        ) : (
          nodes.map((n) => (
            <TableRow key={n.id}>
              <TableCell>
                <Button
                  variant="ghost"
                  size="xs"
                  className="h-auto max-w-full justify-start px-0 font-medium hover:bg-transparent hover:underline"
                  onClick={() => open(n)}
                >
                  <NodeIcon node={n} />
                  <span className="truncate">{n.name}</span>
                  <ExtOffBadge node={n} />
                </Button>
              </TableCell>
              {/* An empty parent name means the space root, which has no folder
                to name — an em dash reads better than the space name repeated
                on every row. */}
              <TableCell className="text-muted-foreground truncate">
                {n.parent_name || "—"}
              </TableCell>
              {extraColumn && (
                <TableCell className="text-muted-foreground truncate">
                  {extraColumn.cell(n)}
                </TableCell>
              )}
              <TableCell className="text-muted-foreground tabular-nums">
                {f.relative(n.modified_at)}
              </TableCell>
              <TableCell className="text-muted-foreground tabular-nums">
                {n.kind === "file" && n.size != null ? f.size(n.size) : "—"}
              </TableCell>
            </TableRow>
          ))
        )}
      </TableBody>
    </Table>
  );
}
