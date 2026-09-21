import { createFileRoute } from "@tanstack/react-router";
import { useLingui } from "@lingui/react/macro";
import { msg } from "@lingui/core/macro";
import type { MessageDescriptor } from "@lingui/core";
import { Clock } from "lucide-react";
import { Surface } from "@/components/surface";
import { SpaceNodeTable } from "@/components/space-node-table";
import { useRecent } from "@/lib/queries";
import { pageTitle } from "@/lib/i18n";
import type { ActivityKind, SearchResult } from "@/lib/api";

export const Route = createFileRoute("/drive/$spaceId/recent")({
  head: () => ({ meta: [{ title: pageTitle(msg`Recent`) }] }),
  component: RecentPage,
});

/**
 * How the table phrases the caller's own last action on a file.
 *
 * Descriptors, not strings: this is module scope and runs before a catalog is
 * active, so a rendered phrase here would pin whichever language loaded first
 * and never follow a language switch. `i18n._()` at render time is what makes
 * it reactive.
 */
const ACTIONS: Record<ActivityKind, MessageDescriptor> = {
  uploaded: msg`You uploaded`,
  created: msg`You created`,
  renamed: msg`You renamed`,
  moved: msg`You moved`,
  trashed: msg`You trashed`,
  restored: msg`You restored`,
  purged: msg`You deleted`,
  shared: msg`You shared`,
  unshared: msg`You revoked`,
  reverted: msg`You reverted`,
  invited: msg`You invited`,
  joined: msg`You joined`,
};

/** Everything the caller has touched in this space. */
function RecentPage() {
  const { t, i18n } = useLingui();
  const { spaceId } = Route.useParams();
  const recent = useRecent(spaceId, 100);

  return (
    <Surface>
      <SpaceNodeTable
        nodes={recent.data ?? []}
        isPending={recent.isPending}
        emptyIcon={<Clock className="text-muted-foreground size-10" />}
        emptyTitle={t`Nothing recent yet`}
        emptyDescription={t`Files you upload or edit in this space show up here.`}
        extraColumn={{
          header: t`Action`,
          cell: (n) => {
            const action = ACTIONS[(n as SearchResult & { last_action: ActivityKind }).last_action];
            return action ? i18n._(action) : null;
          },
        }}
      />
    </Surface>
  );
}
