import { fmtRelative, type ActivityEvent, type ActivityKind } from "@/lib/api";

/**
 * Past-tense phrasing for each verb. Kept here rather than on the server so
 * the wording is a UI concern and the stored event stays a bare fact.
 */
const PHRASES: Record<ActivityKind, string> = {
  uploaded: "uploaded",
  created: "created",
  renamed: "renamed",
  moved: "moved",
  trashed: "moved to trash",
  restored: "restored",
  purged: "permanently deleted",
  shared: "shared",
  unshared: "revoked access to",
  reverted: "reverted",
  invited: "invited",
  joined: "joined",
};

export function ActivityFeed({ events }: { events: ActivityEvent[] }) {
  if (events.length === 0) {
    return <p className="text-muted-foreground text-xs">Nothing has happened yet.</p>;
  }

  return (
    <ul className="border-border flex flex-col gap-3 border-l pl-3">
      {events.map((e) => (
        <li key={e.id} className="relative flex flex-col gap-0.5">
          <span
            aria-hidden
            className="bg-border absolute top-1.5 -left-[17px] size-1.5 rounded-full"
          />
          <p className="text-xs">
            <span className="font-medium">{e.actor}</span> {PHRASES[e.kind] ?? e.kind}
            {e.node_name && <span className="font-medium"> {e.node_name}</span>}
            <span className="text-muted-foreground"> in {e.space_name}</span>
          </p>
          <span className="text-muted-foreground text-[10px] tabular-nums">
            {fmtRelative(e.created_at)}
          </span>
        </li>
      ))}
    </ul>
  );
}
