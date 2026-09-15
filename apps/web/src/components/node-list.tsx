import { MoreHorizontal } from "lucide-react";
import { Button } from "@workspace/ui/components/button";
import { Checkbox } from "@workspace/ui/components/checkbox";
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
import { nodeActions, type NodeActionHandlers } from "@/components/node-actions";
import { NodeIcon } from "@/components/node-table";
import { useUiStore } from "@/lib/ui-store";

interface NodeListProps extends NodeActionHandlers {
  /** Node ids the caller has starred, so each row can show its own state. */
  starredIds: Set<string>;
  nodes: Node[];
  selectedIds: Set<string>;
  focusedId?: string;
  onToggle: (node: Node, checked: boolean) => void;
  onToggleAll: (checked: boolean) => void;
  onClick: (node: Node) => void;
  onDoubleClick: (node: Node) => void;
}

/**
 * The dense listing: 36px rows, a checkbox gutter, right-aligned numeric
 * columns and an actions cell that only shows on hover or selection.
 */
export function NodeList({
  nodes,
  selectedIds,
  starredIds,
  focusedId,
  onToggle,
  onToggleAll,
  onClick,
  onDoubleClick,
  ...handlers
}: NodeListProps) {
  const allSelected = nodes.length > 0 && nodes.every((n) => selectedIds.has(n.id));
  const someSelected = nodes.some((n) => selectedIds.has(n.id));

  return (
    <table className="w-full border-collapse text-left text-xs">
      <thead className="bg-background border-border text-muted-foreground sticky top-0 z-10 border-b text-[11px] tracking-wider uppercase">
        <tr className="h-9">
          <th scope="col" className="w-8 px-3">
            <Checkbox
              checked={allSelected}
              indeterminate={!allSelected && someSelected}
              onCheckedChange={(checked) => onToggleAll(checked === true)}
              aria-label="Select all"
              className="size-3.5"
            />
          </th>
          <th scope="col" className="min-w-60 px-2 font-medium">
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
            focused={focusedId === node.id}
            onToggle={onToggle}
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
  focused: boolean;
  onToggle: (node: Node, checked: boolean) => void;
  onClick: (node: Node) => void;
  onDoubleClick: (node: Node) => void;
}

function NodeListRow({
  node,
  selected,
  starred,
  focused,
  onToggle,
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
          className={cn(
            "group hover:bg-muted/40 h-9 transition-colors",
            (selected || focused) && "bg-muted/60",
          )}
          onClick={() => onClick(node)}
          onDoubleClick={() => onDoubleClick(node)}
        >
          <td className="w-8 px-3">
            <Checkbox
              checked={selected}
              onCheckedChange={(checked) => onToggle(node, checked === true)}
              onClick={(e) => e.stopPropagation()}
              aria-label={`Select ${node.name}`}
              className="size-3.5"
            />
          </td>
          <td className="px-2">
            <div className="flex min-w-0 items-center gap-2">
              <NodeIcon node={node} />
              <span className="truncate font-medium" title={node.name}>
                {node.name}
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

function ContextMenuItemWithSeparator({
  action,
}: {
  action: ReturnType<typeof nodeActions>[number];
}) {
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
