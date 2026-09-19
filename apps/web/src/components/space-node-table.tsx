import { useNavigate } from "@tanstack/react-router";
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
import { opensInEditor, fmtRelative, fmtSize, openEditor, type SearchResult } from "@/lib/api";
import { PreviewModal } from "@/components/preview-modal";
import { NodeIcon } from "@/components/node-icon";

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

  const open = (node: SearchResult) => {
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

  if (isPending) return <p className="text-muted-foreground py-8 text-xs">Loading…</p>;

  if (nodes.length === 0) {
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
          <TableHead>Name</TableHead>
          <TableHead className="w-40">Location</TableHead>
          {extraColumn && <TableHead className="w-36">{extraColumn.header}</TableHead>}
          <TableHead className="w-32">Modified</TableHead>
          <TableHead className="w-24">Size</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {nodes.map((n) => (
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
              </Button>
            </TableCell>
            {/* An empty parent name means the space root, which has no folder
                to name — an em dash reads better than the space name repeated
                on every row. */}
            <TableCell className="text-muted-foreground truncate">{n.parent_name || "—"}</TableCell>
            {extraColumn && (
              <TableCell className="text-muted-foreground truncate">
                {extraColumn.cell(n)}
              </TableCell>
            )}
            <TableCell className="text-muted-foreground tabular-nums">
              {fmtRelative(n.modified_at)}
            </TableCell>
            <TableCell className="text-muted-foreground tabular-nums">
              {n.kind === "file" && n.size != null ? fmtSize(n.size) : "—"}
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
}
