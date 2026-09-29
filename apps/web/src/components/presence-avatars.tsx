import type { PresenceProvider } from "@slash-editor/react";
import { usePresence } from "@slash-editor/react";
import { useLingui } from "@lingui/react/macro";
import { Avatar, AvatarFallback, AvatarGroup } from "@workspace/ui/components/avatar";
import { Tooltip, TooltipContent, TooltipTrigger } from "@workspace/ui/components/tooltip";

/**
 * Everyone else in the document, as initials in their caret colour — the
 * same colour their cursor carries in the text, so a face can be matched to
 * a caret at a glance. The local user is left out: `usePresence` skips our
 * own awareness client.
 */
export function PresenceAvatars({ provider }: { provider: PresenceProvider }) {
  const { t } = useLingui();
  const peers = usePresence(provider);

  if (peers.length === 0) return null;

  return (
    <AvatarGroup data-testid="presence-avatars">
      {peers.map((peer) => {
        const name = typeof peer.name === "string" && peer.name.length > 0 ? peer.name : t`Guest`;
        const color = typeof peer.color === "string" ? peer.color : undefined;

        return (
          <Tooltip key={peer.clientId}>
            <TooltipTrigger
              render={
                <Avatar size="sm" data-testid="presence-avatar" data-name={name} aria-label={name}>
                  <AvatarFallback
                    className="font-medium text-white"
                    style={color ? { backgroundColor: color } : undefined}
                  >
                    {name.slice(0, 1).toUpperCase()}
                  </AvatarFallback>
                </Avatar>
              }
            />
            <TooltipContent>{name}</TooltipContent>
          </Tooltip>
        );
      })}
    </AvatarGroup>
  );
}
