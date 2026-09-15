import { Link, useParams } from "@tanstack/react-router";
import { spacesOptions } from "@/lib/queries";
import { railRow } from "@/components/rail-item";

export function SpaceNav() {
  const spaces = spacesOptions();
  const { spaceId } = useParams({ strict: false }) as { spaceId?: string };

  if (spaces.isPending) {
    return <p className="text-muted-foreground px-2 py-1 text-xs">Loading spaces…</p>;
  }
  if (spaces.isError || !spaces.data?.length) {
    return <p className="text-muted-foreground px-2 py-1 text-xs">No spaces yet.</p>;
  }

  return (
    <nav className="flex flex-col gap-0.5">
      {spaces.data.map((s) => (
        <Link
          key={s.id}
          to="/drive/$spaceId"
          params={{ spaceId: s.id }}
          search={{ dir: undefined }}
          className={railRow(spaceId === s.id, "justify-between")}
        >
          <span className="truncate">{s.name}</span>
          <span className="text-muted-foreground shrink-0 text-[10px] tracking-wider uppercase">
            {s.role}
          </span>
        </Link>
      ))}
    </nav>
  );
}
