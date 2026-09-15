import { Link, useNavigate } from "@tanstack/react-router";
import { ChevronRight, Info, Moon, Sun, Users } from "lucide-react";
import { Button } from "@workspace/ui/components/button";
import { spacesOptions } from "@/lib/queries";
import { useUiStore } from "@/lib/ui-store";
import { useTheme } from "@/components/theme-provider";
import type { DrivePanel } from "@/lib/api";

interface DriveHeaderProps {
  spaceId: string;
  dir?: string;
  panel?: DrivePanel | "members";
  detailsOpen: boolean;
  onToggleDetails: () => void;
  onTogglePanel: (panel: "members") => void;
}

/**
 * Row 1 of the main column: where you are on the left, global toggles on the
 * right. Kept to 40px so it and the toolbar below read as one 80px band.
 */
export function DriveHeader({
  spaceId,
  dir,
  panel,
  detailsOpen,
  onToggleDetails,
  onTogglePanel,
}: DriveHeaderProps) {
  const spaces = spacesOptions();
  const dirs = useUiStore((s) => s.dirs);
  const navigate = useNavigate();
  const { theme, setTheme } = useTheme();

  const space = spaces.data?.find((s) => s.id === spaceId);

  // The API has no single-node GET, so ancestors come from the trail recorded
  // while browsing. A cold deep-link shows the space plus the current folder.
  const trail: { id: string; name: string }[] = [];
  let cursor = dir;
  const guard = new Set<string>();
  while (cursor && dirs[cursor] && !guard.has(cursor)) {
    guard.add(cursor);
    trail.unshift({ id: cursor, name: dirs[cursor].name });
    cursor = dirs[cursor].parent;
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
          aria-label="Space members"
          aria-pressed={panel === "members"}
          className={panel === "members" ? "bg-accent text-foreground" : ""}
          onClick={() => onTogglePanel("members")}
        >
          <Users />
        </Button>
        <Button
          variant="ghost"
          size="icon-sm"
          aria-label="Details"
          aria-pressed={detailsOpen}
          className={detailsOpen ? "bg-accent text-foreground" : ""}
          onClick={onToggleDetails}
        >
          <Info />
        </Button>
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
