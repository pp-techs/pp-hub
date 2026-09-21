import type { FormEvent } from "react";
import { toast } from "sonner";
import { Trans, useLingui } from "@lingui/react/macro";
import { UserPlus } from "lucide-react";
import { createModal } from "@buiducnhat/better-modal";
import { Button } from "@workspace/ui/components/button";
import { Input } from "@workspace/ui/components/input";
import { Label } from "@workspace/ui/components/label";
import { Switch } from "@workspace/ui/components/switch";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@workspace/ui/components/dialog";
import { ApiError } from "@/lib/api";
import { useKeyedState } from "@/lib/use-keyed-state";
import { useAddSpaceMember } from "@/lib/queries";

interface InviteProps extends Record<string, unknown> {
  spaceId: string;
  /** Seeds the admin switch from the space's own default. */
  defaultAsAdmin: boolean;
}

/**
 * Adds an address to the space.
 *
 * An email address is the whole form, because membership is the identity
 * provider's to decide: pp-hub asks it to put the address in the space's
 * organization and mirrors whatever comes back. An address it already knows
 * is a member the moment this returns; one it does not gets an organization
 * invitation by mail and shows up here once it is accepted. There is no
 * account picker any more — there is nothing local left to pick from.
 *
 * The only way into a space: `settings/people` opens it, nothing else does.
 */
export const InviteModal = createModal<InviteProps>(
  "invite-people",
  ({ spaceId, defaultAsAdmin, modal }) => {
    const { t } = useLingui();
    const addMember = useAddSpaceMember();

    // better-modal keeps a closed modal mounted, so plain `useState` would
    // hold the last invite's typed address for the life of the app. The key is
    // the opening itself: reopening for the same space still starts blank.
    const opening = `${spaceId}:${modal.visible}`;
    const [asAdmin, setAsAdmin] = useKeyedState(opening, defaultAsAdmin);
    const [email, setEmail] = useKeyedState(opening, "");

    /**
     * Each refusal means something different to the person reading it, and
     * only one of them is worth retrying. 422 in particular is not this
     * screen's to fix: the space mirrors no organization, so there is nowhere
     * to put the address until an instance administrator links one.
     */
    const reason = (error: Error): string => {
      const status = error instanceof ApiError ? error.status : 0;
      if (status === 409) return t`They are already a member of this space.`;
      if (status === 422) {
        return t`This space is not linked to an organization yet, so nobody can be added to it. An instance administrator has to link it first — there is nothing to fix from this screen.`;
      }
      if (status === 503) {
        return t`The identity provider could not be reached. Nothing was changed; try again in a moment.`;
      }
      return error.message;
    };

    const submit = (e: FormEvent) => {
      e.preventDefault();
      const address = email.trim();
      addMember.mutate(
        { spaceId, email: address, as_admin: asAdmin },
        {
          onSuccess: (result) => {
            if (result.status === "invited") {
              toast.success(t`Invitation sent to ${address}`, {
                description: t`They have no account yet, so the identity provider emailed them one. They join this space as soon as they accept.`,
              });
            } else {
              toast.success(t`${address} is now a member`, {
                description: t`They already had an account, so they were added straight away.`,
              });
            }
            modal.resolve(undefined);
          },
          onError: (error) =>
            toast.error(t`Could not add ${address}`, { description: reason(error) }),
        },
      );
    };

    return (
      <Dialog open={modal.visible} onOpenChange={(open) => !open && modal.resolve(undefined)}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>
              <Trans>Invite people</Trans>
            </DialogTitle>
            <DialogDescription>
              <Trans>
                Joining the space opens no files on its own — what someone can see is what their
                groups have been given. Add them to a group on the Groups pane of Settings
                afterwards.
              </Trans>
            </DialogDescription>
          </DialogHeader>

          <form className="flex flex-col gap-4" onSubmit={submit}>
            <div className="flex items-center justify-between gap-2">
              <Label htmlFor="invite-as-admin" className="cursor-pointer text-sm font-normal">
                <Trans>Join as admin</Trans>
                <span className="text-muted-foreground block text-[10px]">
                  <Trans>Admins change space settings and manage people.</Trans>
                </span>
              </Label>
              <Switch id="invite-as-admin" checked={asAdmin} onCheckedChange={setAsAdmin} />
            </div>

            <div className="flex flex-col gap-1">
              <div className="flex items-center gap-2">
                <Input
                  type="email"
                  value={email}
                  onChange={(e) => setEmail(e.target.value)}
                  placeholder="name@example.com"
                  aria-label={t`Email address to add`}
                  className="min-w-0 flex-1"
                  required
                  autoFocus
                />
                <Button type="submit" disabled={addMember.isPending || !email.trim()}>
                  <UserPlus data-icon="inline-start" /> <Trans>Add</Trans>
                </Button>
              </div>
              <p className="text-muted-foreground text-[10px]">
                <Trans>
                  An address that already has an account joins immediately. Any other address is
                  emailed an invitation by the identity provider and appears here once accepted.
                </Trans>
              </p>
            </div>
          </form>
        </DialogContent>
      </Dialog>
    );
  },
);
