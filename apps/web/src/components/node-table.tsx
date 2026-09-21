import { Trans } from "@lingui/react/macro";
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
import type { Node } from "@/lib/api";
import { useFormatters } from "@/lib/format";
import { NodeIcon } from "@/components/node-icon";

interface NodeTableProps {
  nodes: Node[];
  isPending: boolean;
  emptyIcon: React.ReactNode;
  emptyTitle: string;
  emptyDescription: string;
  /** Rendered per row in a trailing cell. Omit for a read-only listing. */
  renderActions?: (node: Node) => React.ReactNode;
  /** Whether all nodes are shared (shows share indicator on folder icons). */
  allShared?: boolean;
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
  allShared,
}: NodeTableProps) {
  const f = useFormatters();

  if (isPending) {
    return (
      <p className="text-muted-foreground py-8 text-xs">
        <Trans>Loading…</Trans>
      </p>
    );
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
          <TableHead>
            <Trans>Name</Trans>
          </TableHead>
          <TableHead className="w-36">
            <Trans>Modified</Trans>
          </TableHead>
          <TableHead className="w-24">
            <Trans>Size</Trans>
          </TableHead>
          <TableHead className="w-36">
            <Trans>Owner</Trans>
          </TableHead>
          {renderActions ? <TableHead className="w-24" /> : null}
        </TableRow>
      </TableHeader>
      <TableBody>
        {nodes.map((n) => (
          <TableRow key={n.id}>
            <TableCell>
              <div className="flex items-center gap-2">
                <NodeIcon node={n} isShared={allShared} />
                <span className="truncate font-medium">{n.name}</span>
              </div>
            </TableCell>
            <TableCell className="text-muted-foreground tabular-nums">
              {f.date(n.modified_at)}
            </TableCell>
            <TableCell className="text-muted-foreground tabular-nums">
              {n.size != null ? f.size(n.size) : "—"}
            </TableCell>
            <TableCell className="text-muted-foreground truncate">{n.owner ?? "—"}</TableCell>
            {renderActions ? <TableCell>{renderActions(n)}</TableCell> : null}
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
}
