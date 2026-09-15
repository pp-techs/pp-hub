import { FileSpreadsheet, FileText, Folder } from "lucide-react";
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
import { fmtDate, fmtSize, isOfficeFile, type Node } from "@/lib/api";

export function NodeIcon({ node, className }: { node: Node; className?: string }) {
  const cls = className ?? "text-muted-foreground size-4 shrink-0";
  if (node.kind === "dir") return <Folder className={cls} />;
  if (isOfficeFile(node.name)) return <FileSpreadsheet className={cls} />;
  return <FileText className={cls} />;
}

interface NodeTableProps {
  nodes: Node[];
  isPending: boolean;
  emptyIcon: React.ReactNode;
  emptyTitle: string;
  emptyDescription: string;
  /** Rendered per row in a trailing cell. Omit for a read-only listing. */
  renderActions?: (node: Node) => React.ReactNode;
}

/**
 * Read-only dense listing used by the Trash and Shared panels. The interactive
 * browser in the space view keeps its own row component because it carries
 * selection, context menus and double-click navigation.
 */
export function NodeTable({
  nodes,
  isPending,
  emptyIcon,
  emptyTitle,
  emptyDescription,
  renderActions,
}: NodeTableProps) {
  if (isPending) {
    return <p className="text-muted-foreground py-8 text-xs">Loading…</p>;
  }

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
          <TableHead className="w-36">Modified</TableHead>
          <TableHead className="w-24">Size</TableHead>
          <TableHead className="w-36">Owner</TableHead>
          {renderActions ? <TableHead className="w-24" /> : null}
        </TableRow>
      </TableHeader>
      <TableBody>
        {nodes.map((n) => (
          <TableRow key={n.id}>
            <TableCell>
              <div className="flex items-center gap-2">
                <NodeIcon node={n} />
                <span className="truncate font-medium">{n.name}</span>
              </div>
            </TableCell>
            <TableCell className="text-muted-foreground tabular-nums">
              {fmtDate(n.modified_at)}
            </TableCell>
            <TableCell className="text-muted-foreground tabular-nums">
              {n.size != null ? fmtSize(n.size) : "—"}
            </TableCell>
            <TableCell className="text-muted-foreground truncate">{n.owner ?? "—"}</TableCell>
            {renderActions ? <TableCell>{renderActions(n)}</TableCell> : null}
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
}
