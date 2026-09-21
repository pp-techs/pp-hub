import { memo, type MouseEvent } from "react";
import { Trans, useLingui } from "@lingui/react/macro";
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
import type { Node } from "@/lib/api";
import { useFormatters } from "@/lib/format";
import {
  useNodeActions,
  type NodeAction,
  type NodeActionHandlers,
} from "@/components/node-actions";
import { NodeIcon } from "@/components/node-icon";
import { useUiStore } from "@/lib/ui-store";

interface NodeListProps extends NodeActionHandlers {
  /** Node ids the caller has starred, so each row can show its own state. */
  starredIds: Set<string>;
  nodes: Node[];
  /** Whether the caller may add to the folder these rows belong to. */
  canWriteHere: boolean;
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
  canWriteHere,
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
            <Trans>Name</Trans>
          </th>
          <th scope="col" className="w-36 px-2 text-right font-medium">
            <Trans>Modified</Trans>
          </th>
          <th scope="col" className="w-24 px-2 text-right font-medium">
            <Trans>Size</Trans>
          </th>
          <th scope="col" className="w-32 px-2 font-medium">
            <Trans>Owner</Trans>
          </th>
          <th scope="col" className="w-20 px-3 text-right font-medium">
            <Trans>Actions</Trans>
          </th>
        </tr>
      </thead>
      <tbody className="divide-border divide-y">
        {nodes.map((n) => (
          <NodeListRow
            key={n.id}
            node={n}
            selected={selectedIds.has(n.id)}
            starred={starredIds.has(n.id)}
            shared={n.shared ?? false}
            canWriteHere={canWriteHere}
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
  shared: boolean;
  node: Node;
  canWriteHere: boolean;
  selected: boolean;
  onClick: (node: Node, event: MouseEvent) => void;
  onDoubleClick: (node: Node) => void;
}

const NodeListRow = memo(function NodeListRow({
  node,
  canWriteHere,
  selected,
  starred,
  shared,
  onClick,
  onDoubleClick,
  ...handlers
}: NodeListRowProps) {
  const openPanel = useUiStore((s) => s.openPanel);
  const { t, i18n } = useLingui();
  const f = useFormatters();
  const actions = useNodeActions(node, handlers, openPanel, starred, canWriteHere);
  // Named local so the extracted message reads `{nodeName}` rather than `{0}`.
  const nodeName = node.name;

  return (
    <ContextMenu>
      {/* Stops the event reaching the listing's background trigger, whose menu
          acts on the folder: a right-click on a row is about the row, and two
          menus must never open at once. The row's own handler is on this same
          element, so it still runs. */}
      <ContextMenuTrigger className="contents" onContextMenu={(e) => e.stopPropagation()}>
        <tr
          data-node-id={node.id}
          className={cn(
            "group hover:bg-muted/40 h-9 cursor-pointer transition-colors [content-visibility:auto] [contain-intrinsic-size:auto_36px]",
            // Soft wash rather than a border, to match the grid's cards.
            selected && "bg-primary/10 hover:bg-primary/15",
          )}
          onClick={(e) => onClick(node, e)}
          onDoubleClick={() => onDoubleClick(node)}
        >
          <td className="px-3">
            <div className="flex min-w-0 items-center gap-2">
              <NodeIcon node={node} isShared={shared} />
              <span
                className="flex min-w-0 items-center gap-1.5 truncate font-medium"
                title={node.name}
              >
                <span className="truncate">{node.name}</span>
              </span>
            </div>
          </td>
          <td className="text-muted-foreground px-2 text-right tabular-nums">
            {node.modified_at ? f.date(node.modified_at) : "—"}
          </td>
          <td className="text-muted-foreground px-2 text-right tabular-nums">
            {node.size != null ? f.size(node.size) : "—"}
          </td>
          {/* Inside a team folder `owner` is only a byline — access comes from
              the folder's grants, not from whoever uploaded. Printing a name
              here would invite the reader to think that person controls the
              row, so the column names the holder instead. */}
          <td className="text-muted-foreground truncate px-2">
            {node.in_team ? t`Team` : (node.owner ?? "—")}
          </td>
          <td className="px-3 text-right">
            <DropdownMenu>
              <DropdownMenuTrigger
                render={
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    aria-label={t`Actions for ${nodeName}`}
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
              <DropdownMenuContent align="end" className="w-56">
                {actions.map((action) => (
                  <DropdownMenuItem
                    key={action.key}
                    variant={action.destructive ? "destructive" : undefined}
                    onClick={(e) => {
                      e.stopPropagation();
                      action.run();
                    }}
                  >
                    <action.icon /> {i18n._(action.label)}
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

      <ContextMenuContent className="w-56">
        {actions.map((action) => (
          <ContextMenuItemWithSeparator key={action.key} action={action} />
        ))}
      </ContextMenuContent>
    </ContextMenu>
  );
});

function ContextMenuItemWithSeparator({ action }: { action: NodeAction }) {
  const { i18n } = useLingui();
  return (
    <>
      {action.startsGroup && <ContextMenuSeparator />}
      <ContextMenuItem
        variant={action.destructive ? "destructive" : undefined}
        onClick={action.run}
      >
        <action.icon data-icon="inline-start" /> {i18n._(action.label)}
        {action.shortcut && (
          <span className="text-muted-foreground ml-auto pl-4 text-[10px]">{action.shortcut}</span>
        )}
      </ContextMenuItem>
    </>
  );
}
