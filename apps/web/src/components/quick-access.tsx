import { useNavigate, useParams, useSearch } from "@tanstack/react-router";
import { Trash2, Users } from "lucide-react";
import type { LucideIcon } from "lucide-react";
import { railRow } from "@/components/rail-item";
import type { DrivePanel } from "@/lib/api";

const items: { panel: DrivePanel; label: string; icon: LucideIcon }[] = [
  { panel: "shared", label: "Shared with me", icon: Users },
  { panel: "trash", label: "Trash", icon: Trash2 },
];

/**
 * Quick-access rail entries. These are panels over the space view rather than
 * their own routes, matching how Members already works — the shell, breadcrumb
 * and toolbar stay mounted while the listing swaps out.
 *
 * The Stitch design also lists Recent and Starred; the API exposes neither, so
 * they are left out rather than shipped as rows that cannot resolve.
 */
export function QuickAccess() {
  const navigate = useNavigate();
  const { spaceId } = useParams({ strict: false }) as { spaceId?: string };
  const search = useSearch({ strict: false }) as { panel?: DrivePanel };

  // Without a space there is nothing to hang the panel on.
  if (!spaceId) return null;

  return (
    <div className="flex flex-col gap-0.5">
      {items.map(({ panel, label, icon: Icon }) => {
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
