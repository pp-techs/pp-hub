import { Avatar, AvatarFallback, AvatarImage } from "@workspace/ui/components/avatar";
import { cn } from "@workspace/ui/lib/utils";
import type { SocialUser } from "@/lib/api";
import { presenceInitials } from "@/lib/collab/presence";

/** A comment author's or notification actor's face, initials when there is no picture. */
export function SocialAvatar({ user, className }: { user: SocialUser; className?: string }) {
  return (
    <Avatar size="sm" className={cn("mt-0.5", className)}>
      <AvatarImage src={user.avatar_url || undefined} alt="" />
      <AvatarFallback>{presenceInitials(user.name)}</AvatarFallback>
    </Avatar>
  );
}
