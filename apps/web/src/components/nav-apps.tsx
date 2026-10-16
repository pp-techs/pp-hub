import { Fragment, useMemo, useState } from "react";
import type { FormEvent } from "react";
import { Link, useParams } from "@tanstack/react-router";
import { Trans, useLingui } from "@lingui/react/macro";
import { ChevronRight, MoreHorizontal, Plus, Puzzle } from "lucide-react";
import { toast } from "sonner";
import { cn } from "@workspace/ui/lib/utils";
import { Button } from "@workspace/ui/components/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@workspace/ui/components/dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@workspace/ui/components/dropdown-menu";
import { Input } from "@workspace/ui/components/input";
import {
  SidebarGroup,
  SidebarGroupContent,
  SidebarGroupLabel,
  SidebarMenu,
  SidebarMenuAction,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarMenuSkeleton,
  SidebarMenuSub,
  SidebarMenuSubButton,
  SidebarMenuSubItem,
} from "@workspace/ui/components/sidebar";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { extIconOf } from "@/components/ext-icon-tile";
import { NewExtNodeDialog } from "@/components/new-ext-node-dialog";
import { nodeActions, type NodeActionHandlers } from "@/components/node-actions";
import { extKindOf } from "@/ext/registry";
import type { Extension, Node } from "@/lib/api";
import { openFile } from "@/lib/open-file";
import { sidebarApps } from "@/lib/ext-apps";
import { extensionName } from "@/lib/ext-name";
import { groupActions } from "@/lib/group-actions";
import {
  useCreateExtNode,
  useDeleteNode,
  useExtensions,
  useExtNodes,
  useRenameNode,
  useStarred,
  useToggleStar,
} from "@/lib/queries";
import { useUiStore } from "@/lib/ui-store";

/**
 * The apps (kanban, checklist, …) this space has on, each expanding to its
 * nodes. App nodes are kept out of the Drive listing; this is where they are
 * found and where a new one is made.
 */
export function NavApps({ spaceId }: { spaceId: string }) {
  const extensions = useExtensions(spaceId);
  const apps = useMemo(() => sidebarApps(extensions.data), [extensions.data]);
  const [open, setOpen] = useState(true);

  if (apps.length === 0) return null;

  return (
    <SidebarGroup>
      <SidebarGroupLabel
        render={
          <button
            type="button"
            aria-expanded={open}
            onClick={() => setOpen((v) => !v)}
            className="hover:text-sidebar-accent-foreground w-full cursor-pointer gap-1"
          />
        }
      >
        <ChevronRight className={cn("size-3.5! transition-transform", open && "rotate-90")} />
        <Trans>Apps</Trans>
      </SidebarGroupLabel>
      {open && (
        <SidebarGroupContent>
          <SidebarMenu>
            {apps.map((ext) => (
              <NavApp key={ext.id} spaceId={spaceId} ext={ext} />
            ))}
          </SidebarMenu>
        </SidebarGroupContent>
      )}
    </SidebarGroup>
  );
}

function NavApp({ spaceId, ext }: { spaceId: string; ext: Extension }) {
  const { t, i18n } = useLingui();
  const [expanded, setExpanded] = useState(false);
  const create = useCreateExtNode(spaceId);
  const Icon = extIconOf(ext.node_kinds);
  const name = extensionName(ext, i18n);
  // The app's first kind this build can open is the one the + creates.
  const extKind = ext.node_kinds.find((k) => extKindOf(k) !== undefined);
  const kind = extKindOf(extKind);

  const newNode = () => {
    if (!extKind) return;
    void NewExtNodeDialog.show({ extKind }).then((nodeName) => {
      if (!nodeName) return;
      create.mutate(
        { extKind, name: nodeName },
        {
          onSuccess: (made) => {
            setExpanded(true);
            openFile(made);
            const createdName = made.name;
            toast.success(t`Created “${createdName}”`);
          },
          onError: (e) =>
            toast.error(t`Could not create “${nodeName}”`, { description: e.message }),
        },
      );
    });
  };

  return (
    <SidebarMenuItem>
      <SidebarMenuButton aria-expanded={expanded} onClick={() => setExpanded((v) => !v)}>
        <Icon />
        <span>{name}</span>
        <ChevronRight
          className={cn("ml-auto size-3.5 transition-transform", expanded && "rotate-90")}
        />
      </SidebarMenuButton>
      {kind && (
        <SidebarMenuAction
          aria-label={i18n._(kind.newLabel)}
          disabled={create.isPending}
          onClick={newNode}
        >
          <Plus />
        </SidebarMenuAction>
      )}
      {expanded && <AppNodes spaceId={spaceId} ext={ext} />}
    </SidebarMenuItem>
  );
}

/** Mounted only while the app is expanded, so a collapsed app never queries. */
function AppNodes({ spaceId, ext }: { spaceId: string; ext: Extension }) {
  const nodes = useExtNodes(spaceId, ext.id);

  if (nodes.isPending) {
    return (
      <SidebarMenuSub>
        <SidebarMenuSkeleton />
        <SidebarMenuSkeleton />
      </SidebarMenuSub>
    );
  }

  if (nodes.isError) {
    return (
      <SidebarMenuSub>
        <li className="text-destructive px-2 py-1 text-xs" role="alert">
          <Trans>Could not load items.</Trans>{" "}
          <button type="button" className="underline" onClick={() => void nodes.refetch()}>
            <Trans>Retry</Trans>
          </button>
        </li>
      </SidebarMenuSub>
    );
  }

  if (nodes.data.length === 0) {
    return (
      <SidebarMenuSub>
        <li className="text-muted-foreground px-2 py-1 text-xs">
          <Trans>No items yet</Trans>
        </li>
      </SidebarMenuSub>
    );
  }

  return (
    <SidebarMenuSub>
      {nodes.data.map((node) => (
        <AppNode key={node.id} spaceId={spaceId} node={node} />
      ))}
    </SidebarMenuSub>
  );
}

// `nodeActions` is the one definition of what a caller may do to a node and
// gates each entry on the node's capabilities. The rail offers only these; the
// handlers for everything else are never reached, so they do nothing.
const RAIL_ACTIONS: Record<string, true> = {
  edit: true,
  star: true,
  share: true,
  rename: true,
  trash: true,
};
const noop = () => {};

function AppNode({ spaceId, node }: { spaceId: string; node: Node }) {
  const { t, i18n } = useLingui();
  const openPanel = useUiStore((s) => s.openPanel);
  const starred = useStarred(spaceId, 200);
  const toggleStar = useToggleStar();
  const rename = useRenameNode(spaceId);
  const del = useDeleteNode(spaceId);
  const [renaming, setRenaming] = useState(false);
  const [renameName, setRenameName] = useState("");
  const [confirmOpen, setConfirmOpen] = useState(false);

  const isStarred = (starred.data ?? []).some((n) => n.id === node.id);
  const { nodeId: openId } = useParams({ strict: false }) as { nodeId?: string };
  const Icon = extKindOf(node.ext_kind)?.icon ?? Puzzle;
  const nodeName = node.name;

  const handlers: NodeActionHandlers = {
    onPreview: noop,
    onShowDetails: noop,
    onMove: noop,
    onCopy: noop,
    onClipboard: noop,
    onToggleStar: (n, next) =>
      toggleStar.mutate(
        { nodeId: n.id, starred: next },
        { onError: (e) => toast.error(e.message) },
      ),
    onRename: (n) => {
      setRenameName(n.name);
      setRenaming(true);
    },
    onDeleteRequest: () => setConfirmOpen(true),
  };
  const actions = nodeActions(node, handlers, openPanel, isStarred).filter(
    (a) => RAIL_ACTIONS[a.key],
  );

  const submitRename = (e: FormEvent) => {
    e.preventDefault();
    const trimmed = renameName.trim();
    if (trimmed && trimmed !== node.name) {
      rename.mutate(
        { nodeId: node.id, name: trimmed },
        {
          onError: (err) =>
            toast.error(t`Could not rename “${nodeName}”`, { description: err.message }),
        },
      );
    }
    setRenaming(false);
  };

  return (
    <SidebarMenuSubItem>
      <SidebarMenuSubButton
        className="pr-7"
        isActive={openId === node.id}
        render={
          <Link
            to="/drive/$spaceId/file/$nodeId"
            params={{ spaceId: node.space_id, nodeId: node.id }}
          />
        }
      >
        <Icon />
        <span className="truncate">{node.name}</span>
      </SidebarMenuSubButton>

      {actions.length > 0 && (
        <DropdownMenu>
          <DropdownMenuTrigger
            render={
              <button
                type="button"
                aria-label={t`Actions for ${nodeName}`}
                className="text-sidebar-foreground ring-sidebar-ring hover:bg-sidebar-accent hover:text-sidebar-accent-foreground absolute top-1 right-1 flex size-5 items-center justify-center rounded-md outline-hidden focus-visible:ring-2 aria-expanded:opacity-100 md:opacity-0 md:group-focus-within/menu-sub-item:opacity-100 md:group-hover/menu-sub-item:opacity-100 [&>svg]:size-4"
              />
            }
          >
            <MoreHorizontal />
          </DropdownMenuTrigger>
          <DropdownMenuContent align="start" className="w-48">
            {groupActions(actions).map((group, i) => (
              <Fragment key={group[0]!.key}>
                {i > 0 && <DropdownMenuSeparator />}
                <DropdownMenuGroup>
                  {group.map((action) => (
                    <DropdownMenuItem
                      key={action.key}
                      variant={action.destructive ? "destructive" : undefined}
                      onClick={action.run}
                    >
                      <action.icon /> {i18n._(action.label)}
                    </DropdownMenuItem>
                  ))}
                </DropdownMenuGroup>
              </Fragment>
            ))}
          </DropdownMenuContent>
        </DropdownMenu>
      )}

      <Dialog open={renaming} onOpenChange={setRenaming}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>
              <Trans>Rename</Trans>
            </DialogTitle>
            <DialogDescription>
              <Trans>Enter a new name for this item.</Trans>
            </DialogDescription>
          </DialogHeader>
          <form onSubmit={submitRename}>
            <Input
              autoFocus
              value={renameName}
              onChange={(e) => setRenameName(e.target.value)}
              required
            />
            <DialogFooter className="mt-4">
              <Button
                type="submit"
                disabled={!renameName.trim() || renameName.trim() === node.name}
              >
                <Trans>Save</Trans>
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      <ConfirmDialog
        open={confirmOpen}
        onOpenChange={setConfirmOpen}
        title={t`Move to trash`}
        description={t`Are you sure you want to move "${nodeName}" to trash?`}
        confirmLabel={t`Move to trash`}
        // eslint-disable-next-line lingui/no-unlocalized-strings -- Button variant id
        confirmVariant="destructive"
        onConfirm={() =>
          del.mutate(node.id, {
            onError: (e) =>
              toast.error(t`Could not move “${nodeName}” to trash`, { description: e.message }),
          })
        }
      />
    </SidebarMenuSubItem>
  );
}
