import { Trans } from "@lingui/react/macro";
import { ShieldAlert } from "lucide-react";
import { Alert, AlertDescription, AlertTitle } from "@workspace/ui/components/alert";

/**
 * A space with nobody in its Admin group.
 *
 * This is reachable state, not a bug: organization membership belongs to the
 * identity provider, so the last admin can be removed in its console and
 * pp-hub only learns about it afterwards. It refuses the change on its own
 * write path and mirrors it everywhere else, which leaves a space nobody can
 * administer — no settings, no people, no handovers.
 *
 * Shown to every member rather than only to admins, because in this state
 * there is no admin to show it to. It is rendered by the space layout, above
 * whichever surface is open: settings is exactly the page the people who need
 * to read this cannot reach.
 */
export function NoAdminBanner() {
  return (
    <Alert variant="destructive" className="mx-4 mt-4 w-auto shrink-0 md:mt-6 lg:mx-6">
      <ShieldAlert />
      <AlertTitle>
        <Trans>This space has no administrator</Trans>
      </AlertTitle>
      <AlertDescription>
        <Trans>
          Nobody can change its settings, manage its people or complete a handover until an instance
          administrator appoints one.
        </Trans>
      </AlertDescription>
    </Alert>
  );
}
