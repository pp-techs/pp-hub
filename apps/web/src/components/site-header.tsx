import { Fragment, useRef, useState } from "react";
import { Link, useLocation, useParams, useSearch } from "@tanstack/react-router";
import { useLingui } from "@lingui/react/macro";
import { msg } from "@lingui/core/macro";
import type { MessageDescriptor } from "@lingui/core";
import {
  Breadcrumb,
  BreadcrumbItem,
  BreadcrumbLink,
  BreadcrumbList,
  BreadcrumbPage,
  BreadcrumbSeparator,
} from "@workspace/ui/components/breadcrumb";
import { cn } from "@workspace/ui/lib/utils";
import { SidebarTrigger } from "@workspace/ui/components/sidebar";
import type { Node } from "@/lib/api";
import { useMoveNodes, useNode } from "@/lib/queries";
import { useUiStore } from "@/lib/ui-store";
import { isValidMoveTarget } from "@/lib/drag-drop";
import { HeaderSlot } from "@/components/header-slot";
import { NavSearch } from "@/components/nav-search";

/**
 * Path segment -> the label the breadcrumb shows. Descriptors rather than
 * strings: this is module scope and runs before any catalog is active, so a
 * rendered label here would freeze the first language loaded.
 *
 * A new surface adds its segment here, never a prop on the route.
 */
const SECTIONS: Record<string, MessageDescriptor> = {
  home: msg`Home`,
  recent: msg`Recent`,
  starred: msg`Starred`,
  shared: msg`Shared with me`,
  trash: msg`Trash`,
};

/** How deep a folder trail we are willing to resolve one level at a time. */
const MAX_DEPTH = 8;

export function SiteHeader() {
  const location = useLocation();
  const { spaceId } = useParams({ strict: false }) as { spaceId?: string };
  const cachedDirs = useUiStore((s) => s.dirs);
  const { dir } = useSearch({ strict: false }) as { dir?: string };

  const sections = location.pathname
    .split("/")
    .slice(2)
    .map((segment) => SECTIONS[segment])
    .filter((label): label is MessageDescriptor => Boolean(label));

  const onListing = sections.length === 0;

  return (
    <header className="flex h-(--header-height) shrink-0 items-center gap-1 border-b px-4 transition-[width,height] ease-linear lg:gap-2 lg:px-6">
      <SidebarTrigger className="-ml-1" />
      {/* Plain div so height is predictable: base-ui's Separator has
          self-stretch defaults that fight explicit sizing. */}
      <div className="bg-border mx-1 h-4 w-px lg:mx-2" />

      <Breadcrumb className="min-w-0">
        <BreadcrumbList className="gap-1 sm:gap-1.5">
          {onListing ? (
            <ListingBreadcrumb spaceId={spaceId} dir={dir} cachedDirs={cachedDirs} />
          ) : (
            <SectionBreadcrumb sections={sections} />
          )}
        </BreadcrumbList>
      </Breadcrumb>

      <div className="ml-auto flex shrink-0 items-center gap-1 lg:gap-2">
        <NavSearch />
        <HeaderSlot />
      </div>
    </header>
  );
}

function SectionBreadcrumb({ sections }: { sections: MessageDescriptor[] }) {
  const { i18n } = useLingui();
  return (
    <>
      {sections.map((descriptor, i) => {
        const isLast = i === sections.length - 1;
        const label = i18n._(descriptor);
        return (
          <Fragment key={descriptor.id ?? label}>
            {i > 0 && <BreadcrumbSeparator />}
            <BreadcrumbItem className="min-w-0">
              <BreadcrumbPage
                className={isLast ? "truncate font-medium" : "text-muted-foreground truncate"}
              >
                {label}
              </BreadcrumbPage>
            </BreadcrumbItem>
          </Fragment>
        );
      })}
    </>
  );
}

/**
 * Breadcrumb on the listing route: `My Drive` + the folder trail up to `?dir=`.
 * Every segment not yet in the `dirs` cache is fetched on demand via `useNode`,
 * so a direct link (`?dir=deep-folder`) still resolves the full trail even if
 * none of its ancestors have been opened in this session.
 */
function ListingBreadcrumb({
  spaceId,
  dir,
  cachedDirs,
}: {
  spaceId?: string;
  dir?: string;
  cachedDirs: Record<string, { name: string; parent?: string }>;
}) {
  const { t } = useLingui();
  // Fixed slots keep the hook order stable. Each level's id is its child's
  // parent — from the fetched node when there is one, otherwise from the
  // `dirs` store the listing fills in. The store is what lets the trail paint
  // at once on a folder that was just opened, instead of collapsing to
  // "My Drive" for a round trip, and lets every level fetch in parallel
  // rather than one after another. `undefined` marks the space root.
  const parentOf = (id: string | undefined, node: Node | undefined) =>
    node ? node.parent_id : id ? cachedDirs[id]?.parent : undefined;
  const id0 = dir;
  const level0 = useNode(id0);
  const id1 = parentOf(id0, level0.data);
  const level1 = useNode(id1);
  const id2 = parentOf(id1, level1.data);
  const level2 = useNode(id2);
  const id3 = parentOf(id2, level2.data);
  const level3 = useNode(id3);
  const id4 = parentOf(id3, level3.data);
  const level4 = useNode(id4);
  const id5 = parentOf(id4, level4.data);
  const level5 = useNode(id5);
  const id6 = parentOf(id5, level5.data);
  const level6 = useNode(id6);
  const id7 = parentOf(id6, level6.data);
  const level7 = useNode(id7);

  const levels = [
    [id0, level0.data],
    [id1, level1.data],
    [id2, level2.data],
    [id3, level3.data],
    [id4, level4.data],
    [id5, level5.data],
    [id6, level6.data],
    [id7, level7.data],
  ] as const;

  // Stop at the space root; anything past MAX_DEPTH would be a corrupted or
  // circular chain. Prefer the cached name — it reflects any rename made in
  // this session — and fall back to the fetched one. `levels` runs
  // leaf-to-root; reverse it so the breadcrumb reads root-to-leaf.
  const rootAt = levels.findIndex(([id]) => !id);
  const trail = levels
    .slice(0, rootAt === -1 ? MAX_DEPTH : rootAt)
    .map(([id, node]) => ({ id, name: (id && cachedDirs[id]?.name) || node?.name }))
    .filter((seg): seg is { id: string; name: string } => Boolean(seg.id && seg.name))
    .reverse();

  const crumbs: Crumb[] = [
    { key: "my-drive", label: t`My Drive` },
    ...trail.map((seg) => ({
      key: `dir-${seg.id}`,
      label: seg.name,
      dirId: seg.id,
    })),
  ];

  return (
    <>
      {crumbs.map((crumb, i) => {
        const isLast = i === crumbs.length - 1;
        return (
          <Fragment key={crumb.key}>
            {i > 0 && <BreadcrumbSeparator />}
            <BreadcrumbItem className="min-w-0">
              {isLast || !spaceId ? (
                <BreadcrumbPage className="truncate font-medium">{crumb.label}</BreadcrumbPage>
              ) : (
                <BreadcrumbDropLink spaceId={spaceId} dirId={crumb.dirId} label={crumb.label} />
              )}
            </BreadcrumbItem>
          </Fragment>
        );
      })}
    </>
  );
}

interface Crumb {
  key: string;
  label: string;
  /**
   * Absent on the root crumb, which is still a link: `dir: undefined` is what
   * clears `?dir=` and lands back at the top of the space. Only the last
   * crumb renders as plain text — it is where you already are.
   */
  dirId?: string;
}

function BreadcrumbDropLink({
  spaceId,
  dirId,
  label,
}: {
  spaceId: string;
  dirId?: string;
  label: string;
}) {
  const { moveNodes } = useMoveNodes(spaceId);
  const [isDropTarget, setIsDropTarget] = useState(false);
  const dragDepth = useRef(0);

  const handleDragEnter = (e: React.DragEvent) => {
    const dragging = useUiStore.getState().draggingNodes;
    const storeDirs = useUiStore.getState().dirs;
    if (!isValidMoveTarget(dirId, dragging, spaceId, storeDirs)) return;
    e.preventDefault();
    dragDepth.current += 1;
    setIsDropTarget(true);
  };

  const handleDragOver = (e: React.DragEvent) => {
    const dragging = useUiStore.getState().draggingNodes;
    const storeDirs = useUiStore.getState().dirs;
    if (isValidMoveTarget(dirId, dragging, spaceId, storeDirs)) {
      e.preventDefault();
      // eslint-disable-next-line lingui/no-unlocalized-strings -- dataTransfer dropEffect
      e.dataTransfer.dropEffect = "move";
    } else {
      // eslint-disable-next-line lingui/no-unlocalized-strings -- dataTransfer dropEffect
      e.dataTransfer.dropEffect = "none";
    }
  };

  const handleDragLeave = () => {
    dragDepth.current = Math.max(0, dragDepth.current - 1);
    if (dragDepth.current === 0) {
      setIsDropTarget(false);
    }
  };

  const handleDrop = (e: React.DragEvent) => {
    dragDepth.current = 0;
    setIsDropTarget(false);
    const dragging = useUiStore.getState().draggingNodes;
    const storeDirs = useUiStore.getState().dirs;
    if (!isValidMoveTarget(dirId, dragging, spaceId, storeDirs)) return;
    e.preventDefault();
    e.stopPropagation();
    if (dragging) void moveNodes(dragging.nodes, dirId);
    useUiStore.getState().setDraggingNodes(null);
  };

  return (
    <BreadcrumbLink
      className={cn(
        "truncate transition-colors",
        isDropTarget && "text-primary underline ring-1 ring-primary rounded px-1 bg-primary/10",
      )}
      onDragEnter={handleDragEnter}
      onDragOver={handleDragOver}
      onDragLeave={handleDragLeave}
      onDrop={handleDrop}
      render={<Link to="/drive/$spaceId" params={{ spaceId }} search={{ dir: dirId }} />}
    >
      {label}
    </BreadcrumbLink>
  );
}
