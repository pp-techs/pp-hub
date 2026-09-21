import { useState } from "react";
import { Link } from "@tanstack/react-router";
import { Trans, useLingui } from "@lingui/react/macro";
import { toast } from "sonner";
import { Badge } from "@workspace/ui/components/badge";
import { Button } from "@workspace/ui/components/button";
import { Input } from "@workspace/ui/components/input";
import { Label } from "@workspace/ui/components/label";
import { Textarea } from "@workspace/ui/components/textarea";
import type { UserProfile } from "@/lib/api";
import { useMe, useSpaces, useUpdateProfile } from "@/lib/queries";
import { useFormatters } from "@/lib/format";
import { UserAvatar } from "@/components/user-avatar";
import { SectionLabel } from "@/components/section-label";
import { useSettingsDirty } from "@/components/settings/settings-dirty";

export function AccountProfilePane() {
  const { data: profile, isLoading } = useMe();

  if (isLoading)
    return (
      <p className="text-muted-foreground text-xs">
        <Trans>Loading…</Trans>
      </p>
    );
  if (!profile)
    return (
      <p className="text-muted-foreground text-xs">
        <Trans>Profile not found.</Trans>
      </p>
    );

  return <ProfileForm key={profile.id} profile={profile} />;
}

function ProfileForm({ profile }: { profile: UserProfile }) {
  const { t } = useLingui();
  const f = useFormatters();
  const update = useUpdateProfile();
  const spaces = useSpaces();
  const [displayName, setDisplayName] = useState(profile.display_name || profile.name);
  const [bio, setBio] = useState(profile.bio);

  const dirty = displayName !== (profile.display_name || profile.name) || bio !== profile.bio;
  useSettingsDirty(dirty);

  // Joined/last-seen is one sentence, not a stem plus an appended tail: the
  // two halves have to be a single message so a translation can reorder them.
  const joined = f.date(profile.created_at);
  const lastSeen = profile.last_seen ? f.relative(profile.last_seen) : null;

  return (
    <div className="flex max-w-xl flex-col gap-6">
      <div className="flex items-center gap-4">
        <UserAvatar
          src={profile.avatar_url}
          name={profile.display_name || profile.name}
          email={profile.email}
          size="md"
          editable
        />
        <div className="flex min-w-0 flex-col gap-0.5">
          <span className="flex items-center gap-2 text-sm font-medium">
            <span className="truncate">{profile.display_name || profile.name}</span>
            {profile.is_admin && (
              <Badge variant="secondary">
                <Trans>Server admin</Trans>
              </Badge>
            )}
          </span>
          <span className="text-muted-foreground truncate text-xs">{profile.email}</span>
          <span className="text-muted-foreground text-[10px] tabular-nums">
            {lastSeen ? (
              <Trans>
                Joined {joined} · last seen {lastSeen}
              </Trans>
            ) : (
              <Trans>Joined {joined}</Trans>
            )}
          </span>
        </div>
      </div>

      <form
        className="flex flex-col gap-3"
        onSubmit={(e) => {
          e.preventDefault();
          update.mutate(
            { display_name: displayName, bio },
            {
              onSuccess: () => toast.success(t`Profile saved`),
              onError: (e) => toast.error(t`Could not save`, { description: e.message }),
            },
          );
        }}
      >
        <SectionLabel>
          <Trans>Profile</Trans>
        </SectionLabel>

        <div className="flex flex-col gap-1">
          <Label htmlFor="display-name">
            <Trans>Display name</Trans>
          </Label>
          <p className="text-muted-foreground text-[10px]">
            <Trans>How your name appears on files, shares and member lists.</Trans>
          </p>
          <Input
            id="display-name"
            value={displayName}
            onChange={(e) => setDisplayName(e.target.value)}
            placeholder={profile.name}
          />
        </div>

        <div className="flex flex-col gap-1">
          <Label htmlFor="bio">
            <Trans>Bio</Trans>
          </Label>
          <Textarea
            id="bio"
            value={bio}
            onChange={(e) => setBio(e.target.value)}
            placeholder={t`Tell your colleagues what you work on`}
            rows={3}
          />
        </div>

        <div className="flex flex-col gap-1">
          <Label htmlFor="email">
            <Trans>Email</Trans>
          </Label>
          <Input id="email" value={profile.email} disabled readOnly />
          <p className="text-muted-foreground text-[10px]">
            <Trans>Managed by your identity provider — change it there.</Trans>
          </p>
        </div>

        <div className="border-border flex items-center gap-2 border-t pt-4">
          <Button type="submit" disabled={!dirty || update.isPending}>
            <Trans>Save changes</Trans>
          </Button>
          <Button
            type="button"
            variant="ghost"
            disabled={!dirty}
            onClick={() => {
              setDisplayName(profile.display_name || profile.name);
              setBio(profile.bio);
            }}
          >
            <Trans>Discard</Trans>
          </Button>
          <span className="text-muted-foreground text-[10px]">
            {dirty ? <Trans>Unsaved changes</Trans> : <Trans>No unsaved changes</Trans>}
          </span>
        </div>
      </form>

      <section className="flex flex-col gap-2">
        <SectionLabel>
          <Trans>Your spaces</Trans>
        </SectionLabel>
        <div className="border-border divide-border flex flex-col divide-y rounded-lg border">
          {(spaces.data ?? []).map((space) => (
            <div key={space.id} className="flex items-center gap-2 p-2">
              <span
                aria-hidden
                className="bg-foreground text-background grid size-5 shrink-0 place-content-center rounded-sm text-[10px] font-semibold"
              >
                {monogram(space.name)}
              </span>
              {/* No explicit close: a navigation that does not name a hash
                  drops the one in the address, and the dialog follows it. */}
              <Link
                to="/drive/$spaceId"
                params={{ spaceId: space.id }}
                search={{ dir: undefined }}
                className="min-w-0 flex-1 truncate text-xs font-medium hover:underline"
              >
                {space.name}
              </Link>
              <span className="text-muted-foreground shrink-0 text-[10px] uppercase">
                {space.is_admin ? <Trans>Admin</Trans> : <Trans>Member</Trans>}
              </span>
            </div>
          ))}
          {spaces.data?.length === 0 && (
            <p className="text-muted-foreground p-2 text-xs">
              <Trans>You are not in any space yet.</Trans>
            </p>
          )}
        </div>
        <p className="text-muted-foreground text-[10px]">
          <Trans>
            Only a space admin can add or remove members, so leaving is a request to one of them.
          </Trans>
        </p>
      </section>
    </div>
  );
}

/** Two-letter monogram for a space, e.g. "Acme Core" -> "AC". */
function monogram(name: string) {
  const words = name.trim().split(/\s+/).filter(Boolean);
  if (words.length === 0) return "?";
  if (words.length === 1) return words[0].slice(0, 2).toUpperCase();
  return (words[0][0] + words[1][0]).toUpperCase();
}
