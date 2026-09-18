import {
  ArchiveRestore,
  FilePlus2,
  History,
  LogIn,
  Move,
  PencilLine,
  Share2,
  Trash2,
  Upload,
  UserMinus,
  UserPlus,
  XCircle,
  type LucideIcon,
} from "lucide-react";
import { Card, CardContent } from "@workspace/ui/components/card";
import {
  Timeline,
  TimelineContent,
  TimelineDate,
  TimelineHeader,
  TimelineIndicator,
  TimelineItem,
  TimelineSeparator,
  TimelineTitle,
} from "@workspace/ui/components/reui/timeline";
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

/** One glyph per verb, so the feed reads at a glance without the sentence. */
const ICONS: Record<ActivityKind, LucideIcon> = {
  uploaded: Upload,
  created: FilePlus2,
  renamed: PencilLine,
  moved: Move,
  trashed: Trash2,
  restored: ArchiveRestore,
  purged: XCircle,
  shared: Share2,
  unshared: UserMinus,
  reverted: History,
  invited: UserPlus,
  joined: LogIn,
};

export function ActivityFeed({ events }: { events: ActivityEvent[] }) {
  if (events.length === 0) {
    return <p className="text-muted-foreground text-xs">Nothing has happened yet.</p>;
  }

  return (
    <Card>
      <CardContent>
        {/* Every event already happened, so the whole feed renders as
            "completed" — defaultValue pins the active step past the last
            item rather than tracking progress through a process. */}
        <Timeline defaultValue={events.length}>
          {events.map((e, i) => {
            const Icon = ICONS[e.kind] ?? History;
            return (
              <TimelineItem
                key={e.id}
                step={i + 1}
                className="group-data-[orientation=vertical]/timeline:ms-8"
              >
                <TimelineHeader>
                  <TimelineSeparator className="group-data-[orientation=vertical]/timeline:-left-6 group-data-[orientation=vertical]/timeline:h-[calc(100%-1.25rem-0.25rem)] group-data-[orientation=vertical]/timeline:translate-y-5" />
                  <TimelineTitle render={<p />} className="text-xs font-normal">
                    <span className="font-medium">{e.actor}</span> {PHRASES[e.kind] ?? e.kind}
                    {e.node_name && <span className="font-medium"> {e.node_name}</span>}
                    <span className="text-muted-foreground"> in {e.space_name}</span>
                  </TimelineTitle>
                  <TimelineIndicator className="bg-primary/10 text-primary group-data-completed/timeline-item:bg-primary group-data-completed/timeline-item:text-primary-foreground flex size-5 items-center justify-center border-none group-data-[orientation=vertical]/timeline:-left-6">
                    <Icon className="size-3" />
                  </TimelineIndicator>
                </TimelineHeader>
                <TimelineContent>
                  <TimelineDate className="mt-0.5 mb-0 text-[10px] tabular-nums">
                    {fmtRelative(e.created_at)}
                  </TimelineDate>
                </TimelineContent>
              </TimelineItem>
            );
          })}
        </Timeline>
      </CardContent>
    </Card>
  );
}
