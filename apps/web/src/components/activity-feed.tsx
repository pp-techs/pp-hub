import type { ReactNode } from "react";
import { Trans } from "@lingui/react/macro";
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
import type { ActivityEvent, ActivityKind } from "@/lib/api";
import { useFormatters } from "@/lib/format";

/**
 * One whole sentence per verb, not a verb fragment spliced into a template.
 * Kept here rather than on the server so the wording is a UI concern and the
 * stored event stays a bare fact.
 *
 * A fragment map plus `{actor} {verb} {node} in {space}` cannot be
 * translated: word order is part of the sentence, and a language that puts
 * the verb last has nowhere to put it. Each entry is a render function rather
 * than a message descriptor because the sentence carries markup, and because
 * `<Trans>` resolves its text at render time it follows a language switch
 * even though the map itself is built once at import.
 */
type Sentence = (parts: { actor: string; nodeName: string; spaceName: string }) => ReactNode;

const SENTENCES: Record<ActivityKind, Sentence> = {
  uploaded: ({ actor, nodeName, spaceName }) => (
    <Trans>
      <strong className="font-medium">{actor}</strong> uploaded{" "}
      <strong className="font-medium">{nodeName}</strong> in{" "}
      <span className="text-muted-foreground">{spaceName}</span>
    </Trans>
  ),
  created: ({ actor, nodeName, spaceName }) => (
    <Trans>
      <strong className="font-medium">{actor}</strong> created{" "}
      <strong className="font-medium">{nodeName}</strong> in{" "}
      <span className="text-muted-foreground">{spaceName}</span>
    </Trans>
  ),
  renamed: ({ actor, nodeName, spaceName }) => (
    <Trans>
      <strong className="font-medium">{actor}</strong> renamed{" "}
      <strong className="font-medium">{nodeName}</strong> in{" "}
      <span className="text-muted-foreground">{spaceName}</span>
    </Trans>
  ),
  moved: ({ actor, nodeName, spaceName }) => (
    <Trans>
      <strong className="font-medium">{actor}</strong> moved{" "}
      <strong className="font-medium">{nodeName}</strong> in{" "}
      <span className="text-muted-foreground">{spaceName}</span>
    </Trans>
  ),
  trashed: ({ actor, nodeName, spaceName }) => (
    <Trans>
      <strong className="font-medium">{actor}</strong> moved{" "}
      <strong className="font-medium">{nodeName}</strong> to trash in{" "}
      <span className="text-muted-foreground">{spaceName}</span>
    </Trans>
  ),
  restored: ({ actor, nodeName, spaceName }) => (
    <Trans>
      <strong className="font-medium">{actor}</strong> restored{" "}
      <strong className="font-medium">{nodeName}</strong> in{" "}
      <span className="text-muted-foreground">{spaceName}</span>
    </Trans>
  ),
  purged: ({ actor, nodeName, spaceName }) => (
    <Trans>
      <strong className="font-medium">{actor}</strong> permanently deleted{" "}
      <strong className="font-medium">{nodeName}</strong> in{" "}
      <span className="text-muted-foreground">{spaceName}</span>
    </Trans>
  ),
  shared: ({ actor, nodeName, spaceName }) => (
    <Trans>
      <strong className="font-medium">{actor}</strong> shared{" "}
      <strong className="font-medium">{nodeName}</strong> in{" "}
      <span className="text-muted-foreground">{spaceName}</span>
    </Trans>
  ),
  unshared: ({ actor, nodeName, spaceName }) => (
    <Trans>
      <strong className="font-medium">{actor}</strong> revoked access to{" "}
      <strong className="font-medium">{nodeName}</strong> in{" "}
      <span className="text-muted-foreground">{spaceName}</span>
    </Trans>
  ),
  reverted: ({ actor, nodeName, spaceName }) => (
    <Trans>
      <strong className="font-medium">{actor}</strong> reverted{" "}
      <strong className="font-medium">{nodeName}</strong> in{" "}
      <span className="text-muted-foreground">{spaceName}</span>
    </Trans>
  ),
  // `invited` and `joined` carry an email address in `node_name`, not a node
  // name, so these two read as membership sentences rather than file ones.
  // `joined` is recorded both when an admin adds someone and when an invited
  // person accepts — "added" is the one verb that reads correctly for both,
  // since the actor is the one who performed the act in either case.
  invited: ({ actor, nodeName, spaceName }) => (
    <Trans>
      <strong className="font-medium">{actor}</strong> invited{" "}
      <strong className="font-medium">{nodeName}</strong> to{" "}
      <span className="text-muted-foreground">{spaceName}</span>
    </Trans>
  ),
  joined: ({ actor, nodeName, spaceName }) => (
    <Trans>
      <strong className="font-medium">{actor}</strong> added{" "}
      <strong className="font-medium">{nodeName}</strong> to{" "}
      <span className="text-muted-foreground">{spaceName}</span>
    </Trans>
  ),
};

/** A kind this build does not know yet still reads as a sentence. */
const UNKNOWN: Sentence = ({ actor, nodeName, spaceName }) => (
  <Trans>
    <strong className="font-medium">{actor}</strong> updated{" "}
    <strong className="font-medium">{nodeName}</strong> in{" "}
    <span className="text-muted-foreground">{spaceName}</span>
  </Trans>
);

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
  const f = useFormatters();

  if (events.length === 0) {
    return (
      <p className="text-muted-foreground text-xs">
        <Trans>Nothing has happened yet.</Trans>
      </p>
    );
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
            const sentence = SENTENCES[e.kind] ?? UNKNOWN;
            return (
              <TimelineItem
                key={e.id}
                step={i + 1}
                className="group-data-[orientation=vertical]/timeline:ms-8"
              >
                <TimelineHeader>
                  <TimelineSeparator className="group-data-[orientation=vertical]/timeline:-left-6 group-data-[orientation=vertical]/timeline:h-[calc(100%-1.25rem-0.25rem)] group-data-[orientation=vertical]/timeline:translate-y-5" />
                  <TimelineTitle render={<p />} className="text-xs font-normal">
                    {sentence({
                      actor: e.actor,
                      nodeName: e.node_name ?? "",
                      spaceName: e.space_name,
                    })}
                  </TimelineTitle>
                  <TimelineIndicator className="bg-primary/10 text-primary group-data-completed/timeline-item:bg-primary group-data-completed/timeline-item:text-primary-foreground flex size-5 items-center justify-center border-none group-data-[orientation=vertical]/timeline:-left-6">
                    <Icon className="size-3" />
                  </TimelineIndicator>
                </TimelineHeader>
                <TimelineContent>
                  <TimelineDate className="mt-0.5 mb-0 text-[10px] tabular-nums">
                    {f.relative(e.created_at)}
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
