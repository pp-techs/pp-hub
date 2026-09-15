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
import { canEdit, fmtRelative, fmtSize, openEditor, type SearchResult } from "@/lib/api";
import { PreviewModal } from "@/components/preview-modal";
import { NodeIcon } from "@/components/node-table";

interface CrossSpaceTableProps {
  nodes: SearchResult[];
  isPending: boolean;
  emptyIcon: React.ReactNode;
  emptyTitle: string;
  emptyDescription: string;
  /** Extra column between Space and Modified — "Action" on the Recent table. */
  extraColumn?: { header: string; cell: (node: SearchResult) => React.ReactNode };
}

/**
 * A listing whose rows come from several spaces at once — Recent, Starred,
 * Shared with me.
 *
 * Distinct from `NodeTable`, which lists one folder and can therefore leave
 * the location implicit. Here every row has to say where it lives, and opening
 * one has to navigate to another space rather than just changing folder.
 */
export function CrossSpaceTable({
  nodes,
  isPending,
  emptyIcon,
  emptyTitle,
  emptyDescription,
  extraColumn,
}: CrossSpaceTableProps) {
  const navigate = useNavigate();

  const open = (node: SearchResult) => {
    if (node.kind === "dir") {
      void navigate({
        to: "/drive/$spaceId",
        params: { spaceId: node.space_id },
        search: { dir: node.id },
      });
    } else if (canEdit(node)) {
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
          <TableHead className="w-32">Space</TableHead>
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
            <TableCell className="text-muted-foreground truncate" title={n.parent_name}>
              {n.space_name}
            </TableCell>
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
