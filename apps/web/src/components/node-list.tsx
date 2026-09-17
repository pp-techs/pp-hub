import type { MouseEvent } from "react";
import { MoreHorizontal } from "lucide-react";
import { Button } from "@workspace/ui/components/button";
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuSeparator,
  ContextMenuTrigger,
} from "@workspace/ui/components/context-menu";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@workspace/ui/components/dropdown-menu";
import { cn } from "@workspace/ui/lib/utils";
import { fmtDate, fmtSize, type Node } from "@/lib/api";
import { nodeActions, type NodeAction, type NodeActionHandlers } from "@/components/node-actions";
import { NodeIcon } from "@/components/node-icon";
import { useUiStore } from "@/lib/ui-store";

interface NodeListProps extends NodeActionHandlers {
  /** Node ids the caller has starred, so each row can show its own state. */
  starredIds: Set<string>;
  nodes: Node[];
  selectedIds: Set<string>;
  /** Modifier keys decide replace / toggle / range — the caller reads them. */
  onClick: (node: Node, event: MouseEvent) => void;
  onDoubleClick: (node: Node) => void;
}

/**
 * The dense listing: 36px rows, right-aligned numeric columns and an actions
 * cell that only shows on hover or selection. A click selects the row — there
 * is no checkbox gutter, so selection reads as the row's own wash.
 */
export function NodeList({
  nodes,
  selectedIds,
  starredIds,
  onClick,
  onDoubleClick,
  ...handlers
}: NodeListProps) {
  return (
    <table className="w-full border-collapse text-left text-xs">
      <thead className="bg-background border-border text-muted-foreground sticky top-0 z-10 border-b text-[11px] tracking-wider uppercase">
        <tr className="h-9">
          <th scope="col" className="min-w-60 px-3 font-medium">
            Name
          </th>
          <th scope="col" className="w-36 px-2 text-right font-medium">
            Modified
          </th>
          <th scope="col" className="w-24 px-2 text-right font-medium">
            Size
          </th>
          <th scope="col" className="w-32 px-2 font-medium">
            Owner
          </th>
          <th scope="col" className="w-20 px-3 text-right font-medium">
            Actions
          </th>
        </tr>
      </thead>
      <tbody className="divide-border divide-y">
        {nodes.map((node) => (
          <NodeListRow
            key={node.id}
            node={node}
            selected={selectedIds.has(node.id)}
            starred={starredIds.has(node.id)}
            onClick={onClick}
            onDoubleClick={onDoubleClick}
            {...handlers}
          />
        ))}
      </tbody>
    </table>
  );
}

interface NodeListRowProps extends NodeActionHandlers {
  starred: boolean;
  node: Node;
  selected: boolean;
  onClick: (node: Node, event: MouseEvent) => void;
  onDoubleClick: (node: Node) => void;
}

function NodeListRow({
  node,
  selected,
  starred,
  onClick,
  onDoubleClick,
  ...handlers
}: NodeListRowProps) {
  const openPanel = useUiStore((s) => s.openPanel);
  const actions = nodeActions(node, handlers, openPanel, starred);

  return (
    <ContextMenu>
      <ContextMenuTrigger className="contents">
        <tr
          data-node-id={node.id}
          className={cn(
            "group hover:bg-muted/40 h-9 cursor-pointer transition-colors",
            // Soft wash rather than a border, to match the grid's cards.
            selected && "bg-primary/10 hover:bg-primary/15",
          )}
          onClick={(e) => onClick(node, e)}
          onDoubleClick={() => onDoubleClick(node)}
        >
          <td className="px-3">
            <div className="flex min-w-0 items-center gap-2">
              <NodeIcon node={node} />
              <span
                className="flex min-w-0 items-center gap-1.5 truncate font-medium"
                title={node.name}
              >
                <span className="truncate">{node.name}</span>
              </span>
            </div>
          </td>
          <td className="text-muted-foreground px-2 text-right tabular-nums">
            {node.modified_at ? fmtDate(node.modified_at) : "—"}
          </td>
          <td className="text-muted-foreground px-2 text-right tabular-nums">
            {node.size != null ? fmtSize(node.size) : "—"}
          </td>
          <td className="text-muted-foreground truncate px-2">{node.owner ?? "—"}</td>
          <td className="px-3 text-right">
            <DropdownMenu>
              <DropdownMenuTrigger
                render={
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    aria-label={`Actions for ${node.name}`}
                    className={cn(
                      "opacity-0 transition-opacity group-hover:opacity-100 focus-visible:opacity-100",
                      selected && "opacity-100",
                    )}
                    onClick={(e) => e.stopPropagation()}
                  />
                }
              >
                <MoreHorizontal />
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end">
                {actions.map((action) => (
                  <DropdownMenuItem
                    key={action.key}
                    variant={action.destructive ? "destructive" : undefined}
                    onClick={(e) => {
                      e.stopPropagation();
                      action.run();
                    }}
                  >
                    <action.icon /> {action.label}
                    {action.shortcut && (
                      <span className="text-muted-foreground ml-auto pl-4 text-[10px]">
                        {action.shortcut}
                      </span>
                    )}
                  </DropdownMenuItem>
                ))}
              </DropdownMenuContent>
            </DropdownMenu>
          </td>
        </tr>
      </ContextMenuTrigger>

      <ContextMenuContent>
        {actions.map((action) => (
          <ContextMenuItemWithSeparator key={action.key} action={action} />
        ))}
      </ContextMenuContent>
    </ContextMenu>
  );
}

function ContextMenuItemWithSeparator({ action }: { action: NodeAction }) {
  return (
    <>
      {action.startsGroup && <ContextMenuSeparator />}
      <ContextMenuItem
        variant={action.destructive ? "destructive" : undefined}
        onClick={action.run}
      >
        <action.icon data-icon="inline-start" /> {action.label}
        {action.shortcut && (
          <span className="text-muted-foreground ml-auto pl-4 text-[10px]">{action.shortcut}</span>
        )}
      </ContextMenuItem>
    </>
  );
}
