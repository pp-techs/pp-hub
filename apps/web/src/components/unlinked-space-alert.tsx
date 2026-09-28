import { Trans } from "@lingui/react/macro";
import { Link2, Unlink } from "lucide-react";
import { Alert, AlertDescription, AlertTitle } from "@workspace/ui/components/alert";
import { Button } from "@workspace/ui/components/button";
import type { Space } from "@/lib/api";
import { useMe } from "@/lib/queries";
import { LinkSpaceModal } from "@/components/link-space-modal";

/**
 * A space that mirrors no Logto organization: one created before the sync,
 * or one whose organization was deleted in Logto. Nobody can be added to it
 * until it is linked, and linking creates a company upstream — an instance
 * administrator's act, so only they get the button.
 */
export function UnlinkedSpaceAlert({ space }: { space: Space }) {
  const { data: me } = useMe();
  return (
    <Alert>
      <Unlink />
      <AlertTitle>
        <Trans>Not linked to a Logto organization</Trans>
      </AlertTitle>
      <AlertDescription className="flex flex-col items-start gap-2">
        {me?.is_admin ? (
          <>
            <p>
              <Trans>
                Nobody can be added to this space until it is linked. Linking creates an
                organization for it and moves its members there.
              </Trans>
            </p>
            <Button
              size="sm"
              onClick={() => void LinkSpaceModal.show({ spaceId: space.id, spaceName: space.name })}
            >
              <Link2 data-icon="inline-start" /> <Trans>Link to Logto</Trans>
            </Button>
          </>
        ) : (
          <p>
            <Trans>
              Nobody can be added to this space until an instance administrator links it to an
              organization.
            </Trans>
          </p>
        )}
      </AlertDescription>
    </Alert>
  );
}
