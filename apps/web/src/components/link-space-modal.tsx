import { toast } from "sonner";
import { Plural, Trans, useLingui } from "@lingui/react/macro";
import { Link2 } from "lucide-react";
import { createModal } from "@buiducnhat/better-modal";
import { Button } from "@workspace/ui/components/button";
import { Checkbox } from "@workspace/ui/components/checkbox";
import { Label } from "@workspace/ui/components/label";
import { Skeleton } from "@workspace/ui/components/skeleton";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@workspace/ui/components/dialog";
import { ApiError } from "@/lib/api";
import { useKeyedState } from "@/lib/use-keyed-state";
import { useLinkPreview, useLinkSpace } from "@/lib/queries";

interface LinkSpaceProps extends Record<string, unknown> {
  spaceId: string;
  spaceName: string;
}

/**
 * Links a space that predates the Logto sync to a new organization.
 *
 * The preview is the point of the dialog: members Logto has an account for
 * are pushed into the organization with their current role, and the rest
 * lose access and wait for a handover. The second group is named, and linking
 * with anybody in it needs an explicit acknowledgement — the server refuses
 * without one.
 */
export const LinkSpaceModal = createModal<LinkSpaceProps>(
  "link-space",
  ({ spaceId, spaceName, modal }) => {
    const { t } = useLingui();
    const preview = useLinkPreview(spaceId, modal.visible);
    const link = useLinkSpace(spaceId);
    const [confirmed, setConfirmed] = useKeyedState(`${spaceId}:${modal.visible}`, false);

    const pushable = preview.data?.pushable ?? [];
    const stranded = preview.data?.stranded ?? [];
    const needsConfirm = stranded.length > 0;

    const submit = () =>
      link.mutate(needsConfirm, {
        onSuccess: () => {
          toast.success(t`Linking ${spaceName}`, {
            description: t`The organization exists; its members are being added now. The space shows as linked in a moment.`,
          });
          modal.resolve(undefined);
        },
        onError: (error) => {
          const status = error instanceof ApiError ? error.status : 0;
          toast.error(t`Could not link ${spaceName}`, {
            description:
              status === 503
                ? t`The identity provider could not be reached. Nothing was changed; try again in a moment.`
                : error.message,
          });
        },
      });

    return (
      <Dialog open={modal.visible} onOpenChange={(open) => !open && modal.resolve(undefined)}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>
              <Trans>Link to Logto</Trans>
            </DialogTitle>
            <DialogDescription>
              <Trans>
                Creates a Logto organization named “{spaceName}” and makes it the source of truth
                for who belongs to this space.
              </Trans>
            </DialogDescription>
          </DialogHeader>

          {preview.isLoading ? (
            <div className="flex flex-col gap-2">
              <Skeleton className="h-3.5 w-3/4" />
              <Skeleton className="h-3.5 w-1/2" />
            </div>
          ) : preview.isError ? (
            <p className="text-destructive text-xs">{preview.error.message}</p>
          ) : (
            <div className="flex flex-col gap-3 text-xs">
              <p>
                <Plural
                  value={pushable.length}
                  one="# member joins the organization with their current role."
                  other="# members join the organization with their current role."
                />
              </p>
              {needsConfirm && (
                <div className="border-destructive/40 bg-destructive/5 flex flex-col gap-2 rounded-md border p-3">
                  <p className="font-medium">
                    <Plural
                      value={stranded.length}
                      one="# member has no Logto account and will lose access:"
                      other="# members have no Logto account and will lose access:"
                    />
                  </p>
                  <ul className="text-muted-foreground flex flex-col gap-0.5">
                    {stranded.map((m) => (
                      <li key={m.user_id} className="truncate">
                        {m.name ? `${m.name} · ${m.email}` : m.email}
                      </li>
                    ))}
                  </ul>
                  <p className="text-muted-foreground">
                    <Trans>
                      Their files stay in place and wait for a handover on the People pane.
                    </Trans>
                  </p>
                  <div className="flex items-center gap-2">
                    <Checkbox
                      id="link-confirm-stranded"
                      checked={confirmed}
                      onCheckedChange={(checked) => setConfirmed(checked === true)}
                    />
                    <Label htmlFor="link-confirm-stranded" className="text-xs font-normal">
                      <Trans>I understand they will lose access</Trans>
                    </Label>
                  </div>
                </div>
              )}
            </div>
          )}

          <DialogFooter>
            <Button variant="outline" onClick={() => modal.resolve(undefined)}>
              <Trans>Cancel</Trans>
            </Button>
            <Button
              onClick={submit}
              disabled={!preview.data || link.isPending || (needsConfirm && !confirmed)}
            >
              <Link2 data-icon="inline-start" /> <Trans>Link</Trans>
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    );
  },
);
