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
import { SkeletonTableRows } from "@/components/loading";
import { Button } from "@workspace/ui/components/button";

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
  /** Makes the name a button; the caller decides what opening means. */
  onOpen?: (node: Node) => void;
  /** A column right after Name — the space on outside shares. */
  extraColumn?: { header: string; cell: (node: Node) => React.ReactNode };
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
  onOpen,
  extraColumn,
}: NodeTableProps) {
  const f = useFormatters();

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
          {extraColumn && <TableHead className="w-40">{extraColumn.header}</TableHead>}
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
        {isPending ? (
          <SkeletonTableRows columns={4 + (renderActions ? 1 : 0) + (extraColumn ? 1 : 0)} />
        ) : (
          nodes.map((n) => (
            <TableRow key={n.id}>
              <TableCell>
                {onOpen ? (
                  <Button
                    variant="ghost"
                    size="xs"
                    className="h-auto max-w-full justify-start px-0 font-medium hover:bg-transparent hover:underline"
                    onClick={() => onOpen(n)}
                  >
                    <NodeIcon node={n} isShared={allShared} />
                    <span className="truncate">{n.name}</span>
                  </Button>
                ) : (
                  <div className="flex items-center gap-2">
                    <NodeIcon node={n} isShared={allShared} />
                    <span className="truncate font-medium">{n.name}</span>
                  </div>
                )}
              </TableCell>
              {extraColumn && (
                <TableCell className="text-muted-foreground truncate">
                  {extraColumn.cell(n)}
                </TableCell>
              )}
              <TableCell className="text-muted-foreground tabular-nums">
                {f.date(n.modified_at)}
              </TableCell>
              <TableCell className="text-muted-foreground tabular-nums">
                {n.size != null ? f.size(n.size) : "—"}
              </TableCell>
              <TableCell className="text-muted-foreground truncate">{n.owner ?? "—"}</TableCell>
              {renderActions ? <TableCell>{renderActions(n)}</TableCell> : null}
            </TableRow>
          ))
        )}
      </TableBody>
    </Table>
  );
}
