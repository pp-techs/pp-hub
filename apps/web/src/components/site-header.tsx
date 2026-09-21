import { Fragment } from "react";
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
import { SidebarTrigger } from "@workspace/ui/components/sidebar";
import { useNode } from "@/lib/queries";
import { useUiStore } from "@/lib/ui-store";
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
  // Fixed slots keep the hook order stable. Each fetches the node at one level;
  // `parent_id === undefined` marks the space root, where the trail stops.
  const level0 = useNode(dir);
  const level1 = useNode(level0.data?.parent_id);
  const level2 = useNode(level1.data?.parent_id);
  const level3 = useNode(level2.data?.parent_id);
  const level4 = useNode(level3.data?.parent_id);
  const level5 = useNode(level4.data?.parent_id);
  const level6 = useNode(level5.data?.parent_id);
  const level7 = useNode(level6.data?.parent_id);

  const fetched = [level0, level1, level2, level3, level4, level5, level6, level7];

  // Stop at the first level where there is no node, or the node has no parent
  // (the space root). Anything past it would be a corrupted or circular chain.
  const deepest = fetched.findIndex((q) => !q.data || !q.data.parent_id);
  const chain = fetched.slice(0, deepest === -1 ? MAX_DEPTH : deepest + 1);

  // Prefer the cached name if we have one — it reflects any rename the user
  // made in this session. The fetched name is the fallback until the cache
  // catches up, or forever for folders the user opened exactly once.
  // `chain` runs leaf-to-root (level0 is the current dir); reverse it so the
  // breadcrumb reads root-to-leaf, ancestors before the current folder.
  const trail = chain
    .map((q) => {
      const id = q.data?.id;
      const name = (id && cachedDirs[id]?.name) || q.data?.name;
      return { id, name };
    })
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
                <BreadcrumbLink
                  className="truncate"
                  render={
                    <Link to="/drive/$spaceId" params={{ spaceId }} search={{ dir: crumb.dirId }} />
                  }
                >
                  {crumb.label}
                </BreadcrumbLink>
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
