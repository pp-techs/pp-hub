import { Link, useNavigate, useParams, useSearch } from "@tanstack/react-router";
import { Clock, House, Star, Trash2, Users } from "lucide-react";
import type { LucideIcon } from "lucide-react";
import { railRow } from "@/components/rail-item";
import type { DrivePanel } from "@/lib/api";

/**
 * Routes that span every space, so they cannot be panels over a single space
 * view the way Trash and Shared are.
 */
const routes: {
  to: "/drive/home" | "/drive/recent" | "/drive/starred";
  label: string;
  icon: LucideIcon;
}[] = [
  { to: "/drive/home", label: "Home", icon: House },
  { to: "/drive/recent", label: "Recent", icon: Clock },
  { to: "/drive/starred", label: "Starred", icon: Star },
];

/** Panels over the space view — the shell and toolbar stay mounted. */
const panels: { panel: DrivePanel; label: string; icon: LucideIcon }[] = [
  { panel: "shared", label: "Shared with me", icon: Users },
  { panel: "trash", label: "Trash", icon: Trash2 },
];

export function QuickAccess() {
  const navigate = useNavigate();
  const { spaceId } = useParams({ strict: false }) as { spaceId?: string };
  const search = useSearch({ strict: false }) as { panel?: DrivePanel };

  return (
    <div className="flex flex-col gap-0.5">
      {routes.map(({ to, label, icon: Icon }) => (
        <Link
          key={to}
          to={to}
          className={railRow(false)}
          activeProps={{ className: railRow(true) }}
        >
          <Icon className="text-muted-foreground size-3.5 shrink-0" />
          <span className="truncate">{label}</span>
        </Link>
      ))}

      {/* Trash is per-space, so it needs a space to hang on. */}
      {spaceId &&
        panels.map(({ panel, label, icon: Icon }) => {
          const active = search.panel === panel;
          return (
            <button
              key={panel}
              type="button"
              className={railRow(active)}
              onClick={() =>
                void navigate({
                  to: "/drive/$spaceId",
                  params: { spaceId },
                  search: { panel: active ? undefined : panel },
                })
              }
            >
              <Icon className="text-muted-foreground size-3.5 shrink-0" />
              <span className="truncate">{label}</span>
            </button>
          );
        })}
    </div>
  );
}
