import { createFileRoute, Link } from "@tanstack/react-router";
import { useState } from "react";
import { toast } from "sonner";
import { Button } from "@workspace/ui/components/button";
import { Input } from "@workspace/ui/components/input";
import { Label } from "@workspace/ui/components/label";
import { Textarea } from "@workspace/ui/components/textarea";
import { Badge } from "@workspace/ui/components/badge";
import { fmtDate, fmtRelative, type UserProfile } from "@/lib/api";
import { useMe, useSpaces, useUpdateProfile } from "@/lib/queries";
import { Surface } from "@/components/surface";
import { UserAvatar } from "@/components/user-avatar";
import { SectionLabel } from "@/components/section-label";

export const Route = createFileRoute("/drive/account/profile")({
  head: () => ({ meta: [{ title: "Profile · Account · pp-hub" }] }),
  component: ProfilePage,
});

function ProfilePage() {
  const { data: profile, isLoading } = useMe();

  if (isLoading) return <p className="text-muted-foreground p-4 text-xs">Loading…</p>;
  if (!profile) return <p className="text-muted-foreground p-4 text-xs">Profile not found.</p>;

  return <ProfileForm key={profile.id} profile={profile} />;
}

function ProfileForm({ profile }: { profile: UserProfile }) {
  const update = useUpdateProfile();
  const spaces = useSpaces();
  const [displayName, setDisplayName] = useState(profile.display_name || profile.name);
  const [bio, setBio] = useState(profile.bio);

  const dirty = displayName !== (profile.display_name || profile.name) || bio !== profile.bio;

  return (
    <Surface className="max-w-xl">
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
            {profile.is_admin && <Badge variant="secondary">Server admin</Badge>}
          </span>
          <span className="text-muted-foreground truncate text-xs">{profile.email}</span>
          <span className="text-muted-foreground text-[10px] tabular-nums">
            Joined {fmtDate(profile.created_at)}
            {profile.last_seen && ` · last seen ${fmtRelative(profile.last_seen)}`}
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
              onSuccess: () => toast.success("Profile saved"),
              onError: (e) => toast.error("Could not save", { description: e.message }),
            },
          );
        }}
      >
        <SectionLabel>Profile</SectionLabel>

        <div className="flex flex-col gap-1">
          <Label htmlFor="display-name">Display name</Label>
          <p className="text-muted-foreground text-[10px]">
            How your name appears on files, shares and member lists.
          </p>
          <Input
            id="display-name"
            value={displayName}
            onChange={(e) => setDisplayName(e.target.value)}
            placeholder={profile.name}
          />
        </div>

        <div className="flex flex-col gap-1">
          <Label htmlFor="bio">Bio</Label>
          <Textarea
            id="bio"
            value={bio}
            onChange={(e) => setBio(e.target.value)}
            placeholder="Tell your colleagues what you work on"
            rows={3}
          />
        </div>

        <div className="flex flex-col gap-1">
          <Label htmlFor="email">Email</Label>
          <Input id="email" value={profile.email} disabled readOnly />
          <p className="text-muted-foreground text-[10px]">
            Managed by your identity provider — change it there.
          </p>
        </div>

        <div className="border-border flex items-center gap-2 border-t pt-4">
          <Button type="submit" disabled={!dirty || update.isPending}>
            Save changes
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
            Discard
          </Button>
          <span className="text-muted-foreground text-[10px]">
            {dirty ? "Unsaved changes" : "No unsaved changes"}
          </span>
        </div>
      </form>

      <section className="flex flex-col gap-2">
        <SectionLabel>Your spaces</SectionLabel>
        <div className="border-border divide-border flex flex-col divide-y rounded-lg border">
          {(spaces.data ?? []).map((space) => (
            <div key={space.id} className="flex items-center gap-2 p-2">
              <span
                aria-hidden
                className="bg-foreground text-background grid size-5 shrink-0 place-content-center rounded-sm text-[10px] font-semibold"
              >
                {monogram(space.name)}
              </span>
              <Link
                to="/drive/$spaceId"
                params={{ spaceId: space.id }}
                search={{ dir: undefined }}
                className="min-w-0 flex-1 truncate text-xs font-medium hover:underline"
              >
                {space.name}
              </Link>
              <span className="text-muted-foreground shrink-0 text-[10px] uppercase">
                {space.is_admin ? "Admin" : "Member"}
              </span>
            </div>
          ))}
          {spaces.data?.length === 0 && (
            <p className="text-muted-foreground p-2 text-xs">You are not in any space yet.</p>
          )}
        </div>
        <p className="text-muted-foreground text-[10px]">
          Only a space admin can add or remove members, so leaving is a request to one of them.
        </p>
      </section>
    </Surface>
  );
}

/** Two-letter monogram for a space, e.g. "Acme Core" -> "AC". */
function monogram(name: string) {
  const words = name.trim().split(/\s+/).filter(Boolean);
  if (words.length === 0) return "?";
  if (words.length === 1) return words[0].slice(0, 2).toUpperCase();
  return (words[0][0] + words[1][0]).toUpperCase();
}
