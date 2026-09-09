import { Link, useParams } from "@tanstack/react-router";
import { FolderSync } from "lucide-react";
import { spacesOptions } from "@/lib/queries";
import { cn } from "@workspace/ui/lib/utils";

export function SpaceNav() {
  const spaces = spacesOptions();
  const { spaceId } = useParams({ strict: false }) as { spaceId?: string };

  if (spaces.isPending) {
    return <p className="px-3 py-2 text-xs opacity-60">Loading spaces…</p>;
  }
  if (spaces.isError || !spaces.data?.length) {
    return <p className="px-3 py-2 text-xs opacity-60">No spaces yet — use + above.</p>;
  }

  return (
    <nav className="flex flex-1 flex-col gap-0.5 overflow-y-auto p-2">
      {spaces.data.map((s) => (
        <Link
          key={s.id}
          to="/drive/$spaceId"
          params={{ spaceId: s.id }}
          search={{ dir: undefined }}
          className={cn(
            "hover:bg-sidebar-accent flex items-center justify-between gap-2 rounded-md px-2 py-1.5 text-sm",
            spaceId === s.id && "bg-sidebar-accent font-medium",
          )}
        >
          <span className="truncate">
            {spaceId === s.id ? <FolderSync className="mb-0.5 mr-1 inline size-3.5" /> : null}
            {s.name}
          </span>
          <span className="text-muted-foreground text-[10px] uppercase">{s.role}</span>
        </Link>
      ))}
    </nav>
  );
}
