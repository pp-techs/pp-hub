import { toast } from "sonner";
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
import type { UserSearchResult } from "@/lib/api";
import { useKeyedState } from "@/lib/use-keyed-state";
import { useAddSpaceMember, useCreateInvitation } from "@/lib/queries";
import { MemberPicker } from "@/components/member-picker";
import { SectionLabel } from "@/components/section-label";

interface InviteProps extends Record<string, unknown> {
  spaceId: string;
  /** Accounts already in the space, hidden from the picker. */
  memberIds: string[];
  /** Seeds the admin switch from the space's own default. */
  defaultAsAdmin: boolean;
}

/**
 * Adds someone to the space — by picking an existing account, or by emailing
 * an address that has none.
 *
 * The only way into a space: `settings/people` opens it, nothing else does.
 */
export const InviteModal = createModal<InviteProps>(
  "invite-people",
  ({ spaceId, memberIds, defaultAsAdmin, modal }) => {
    const addMember = useAddSpaceMember();
    const invite = useCreateInvitation(spaceId);

    // better-modal keeps a closed modal mounted, so plain `useState` would
    // hold the last invite's typed address for the life of the app. The key is
    // the opening itself: reopening for the same space still starts blank.
    const opening = `${spaceId}:${modal.visible}`;
    const [asAdmin, setAsAdmin] = useKeyedState(opening, defaultAsAdmin);
    const [picked, setPicked] = useKeyedState<string, UserSearchResult | null>(opening, null);
    const [email, setEmail] = useKeyedState(opening, "");

    return (
      <Dialog open={modal.visible} onOpenChange={(open) => !open && modal.resolve(undefined)}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>Invite people</DialogTitle>
            <DialogDescription>
              Joining the space opens no files on its own — what someone can see is what their
              groups have been given. Add them to a group on the People table afterwards.
            </DialogDescription>
          </DialogHeader>

          <div className="flex flex-col gap-4">
            <div className="flex items-center justify-between gap-2">
              <Label htmlFor="invite-as-admin" className="cursor-pointer text-sm font-normal">
                Join as admin
                <span className="text-muted-foreground block text-[10px]">
                  Admins change space settings and manage people.
                </span>
              </Label>
              <Switch id="invite-as-admin" checked={asAdmin} onCheckedChange={setAsAdmin} />
            </div>

            <div className="flex flex-col gap-1">
              <SectionLabel>Someone with an account</SectionLabel>
              <div className="flex items-center gap-2">
                <div className="min-w-0 flex-1">
                  <MemberPicker
                    onSelect={setPicked}
                    excludeIds={memberIds}
                    placeholder={picked ? picked.display_name || picked.name : "Search people…"}
                  />
                </div>
                <Button
                  disabled={!picked || addMember.isPending}
                  onClick={() =>
                    picked &&
                    addMember.mutate(
                      { spaceId, user_id: picked.id, as_admin: asAdmin },
                      {
                        onSuccess: () => {
                          toast.success(`Added ${picked.display_name || picked.name}`);
                          modal.resolve(undefined);
                        },
                        onError: (e) => toast.error(e.message),
                      },
                    )
                  }
                >
                  Add
                </Button>
              </div>
            </div>

            <form
              className="flex flex-col gap-1"
              onSubmit={(e) => {
                e.preventDefault();
                invite.mutate(
                  { email: email.trim(), as_admin: asAdmin },
                  {
                    onSuccess: (result) => {
                      if (result.added_member) {
                        toast.success(`${result.email} already has an account`, {
                          description: "They were added to the space directly.",
                        });
                      } else if (result.mail_sent) {
                        void navigator.clipboard?.writeText(result.url ?? "");
                        toast.success("Invitation sent", {
                          description: `Emailed to ${result.email}. The link is also on your clipboard.`,
                        });
                      } else if (result.url) {
                        void navigator.clipboard?.writeText(result.url);
                        toast.success("Invitation created", {
                          description: "The link is on your clipboard — send it to them.",
                        });
                      }
                      modal.resolve(undefined);
                    },
                    onError: (e) => toast.error("Could not invite", { description: e.message }),
                  },
                );
              }}
            >
              <SectionLabel>By email</SectionLabel>
              <div className="flex items-center gap-2">
                <Input
                  type="email"
                  value={email}
                  onChange={(e) => setEmail(e.target.value)}
                  placeholder="name@example.com"
                  aria-label="Email to invite"
                  className="min-w-0 flex-1"
                  required
                />
                <Button type="submit" disabled={invite.isPending || !email.trim()}>
                  <UserPlus data-icon="inline-start" /> Invite
                </Button>
              </div>
              <p className="text-muted-foreground text-[10px]">
                pp-hub does not send mail yet. The invitation link is copied to your clipboard so
                you can send it yourself; it expires in 14 days.
              </p>
            </form>
          </div>
        </DialogContent>
      </Dialog>
    );
  },
);
