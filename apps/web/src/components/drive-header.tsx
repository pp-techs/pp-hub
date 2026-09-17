import { Link, useLocation, useNavigate, useSearch } from "@tanstack/react-router";
import { ChevronRight, Moon, Sun } from "lucide-react";
import { Button } from "@workspace/ui/components/button";
import { spacesOptions } from "@/lib/queries";
import { useUiStore } from "@/lib/ui-store";
import { useTheme } from "@/components/theme-provider";

/**
 * What each path segment under `/drive/$spaceId` is called in a breadcrumb.
 * The URL is the only source of truth for where the user is, so deriving the
 * crumbs from it cannot disagree with the rendered route.
 */
const SECTIONS: Record<string, string> = {
  home: "Home",
  recent: "Recent",
  starred: "Starred",
  shared: "Shared with me",
  trash: "Trash",
  settings: "Settings",
  general: "General",
  people: "People",
  danger: "Danger zone",
};

/**
 * Row 1 of the main column: where you are on the left, global toggles on the
 * right. Kept to 40px so it and the toolbar below read as one 80px band.
 */
export function DriveHeader({ spaceId }: { spaceId: string }) {
  const spaces = spacesOptions();
  const dirs = useUiStore((s) => s.dirs);
  const navigate = useNavigate();
  const location = useLocation();
  const { theme, setTheme } = useTheme();
  // Only the listing route declares `dir`; everywhere else it is absent.
  const { dir } = useSearch({ strict: false }) as { dir?: string };

  const space = spaces.data?.find((s) => s.id === spaceId);

  // Sections named by the path, then — on the listing — the folder trail. The
  // API has no single-node GET, so ancestors come from the trail recorded while
  // browsing; a cold deep-link shows the space plus the current folder.
  const sections = location.pathname
    .split("/")
    .slice(3)
    .map((segment) => SECTIONS[segment])
    .filter((label): label is string => Boolean(label));

  const trail: { id: string; name: string }[] = [];
  let cursor = dir;
  const guard = new Set<string>();
  while (cursor && dirs[cursor] && !guard.has(cursor)) {
    guard.add(cursor);
    const entry = dirs[cursor];
    trail.unshift({ id: cursor, name: entry.name });
    cursor = entry.parent;
  }

  return (
    <header className="border-border flex h-10 shrink-0 items-center justify-between gap-2 border-b px-4">
      <nav aria-label="Breadcrumb" className="flex min-w-0 items-center gap-1.5 text-xs">
        <Link
          to="/drive/$spaceId"
          params={{ spaceId }}
          search={{ dir: undefined }}
          className="text-muted-foreground hover:text-foreground truncate transition-colors"
        >
          {space?.name ?? "Drive"}
        </Link>

        {sections.map((label, i) => (
          <span key={label} className="flex min-w-0 items-center gap-1.5">
            <ChevronRight className="text-border size-3.5 shrink-0" />
            <span
              className={
                i === sections.length - 1
                  ? "truncate font-medium"
                  : "text-muted-foreground truncate"
              }
              aria-current={i === sections.length - 1 ? "page" : undefined}
            >
              {label}
            </span>
          </span>
        ))}

        {trail.map((c) => (
          <span key={c.id} className="flex min-w-0 items-center gap-1.5">
            <ChevronRight className="text-border size-3.5 shrink-0" />
            {c.id === dir ? (
              <span className="truncate font-medium" aria-current="page">
                {c.name}
              </span>
            ) : (
              <button
                type="button"
                className="text-muted-foreground hover:text-foreground truncate transition-colors"
                onClick={() =>
                  void navigate({
                    to: "/drive/$spaceId",
                    params: { spaceId },
                    search: { dir: c.id },
                  })
                }
              >
                {c.name}
              </button>
            )}
          </span>
        ))}
      </nav>

      <div className="flex shrink-0 items-center gap-1">
        <Button
          variant="ghost"
          size="icon-sm"
          aria-label="Toggle theme"
          onClick={() => setTheme(theme === "dark" ? "light" : "dark")}
        >
          {theme === "dark" ? <Sun /> : <Moon />}
        </Button>
      </div>
    </header>
  );
}
