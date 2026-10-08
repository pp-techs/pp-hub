import { Trans, useLingui } from "@lingui/react/macro";
import { toast } from "sonner";
import { Lock } from "lucide-react";
import { cn } from "@workspace/ui/lib/utils";
import { Label } from "@workspace/ui/components/label";
import { Skeleton } from "@workspace/ui/components/skeleton";
import { Switch } from "@workspace/ui/components/switch";
import { useExtensions, useSetSpaceExtension } from "@/lib/queries";
import { extensionName } from "@/lib/ext-name";
import { useActiveSpaceId } from "@/lib/use-active-space";
import { ExtIconTile, extIconOf } from "@/components/ext-icon-tile";
import { LoadingStatus } from "@/components/loading";

/**
 * Which of the instance's apps this space uses. A space can only switch off
 * what the instance turned on, and back on again; an app the instance has off
 * is listed greyed, with the reason, because there is nothing to choose.
 * Switches apply at once, like the other panes that are not one form.
 */
export function SpaceAppsPane() {
  const { t, i18n } = useLingui();
  const spaceId = useActiveSpaceId();
  const extensions = useExtensions(spaceId, !!spaceId);
  const setEnabled = useSetSpaceExtension(spaceId ?? "");

  if (!spaceId || extensions.isPending) {
    return (
      <LoadingStatus className="flex max-w-xl flex-col gap-4">
        {[0, 1].map((i) => (
          <div key={i} className="flex items-start justify-between gap-4">
            <div className="flex flex-1 flex-col gap-1.5">
              <Skeleton className="h-3 w-32" />
              <Skeleton className="h-2.5 w-3/4" />
            </div>
            <Skeleton className="h-4 w-7 shrink-0 rounded-full" />
          </div>
        ))}
      </LoadingStatus>
    );
  }
  if (extensions.isError) {
    return <p className="text-destructive text-sm">{extensions.error.message}</p>;
  }
  if (extensions.data.length === 0) {
    return (
      <p className="text-muted-foreground text-sm">
        <Trans>This install includes no apps.</Trans>
      </p>
    );
  }

  return (
    <div className="flex max-w-xl flex-col gap-4">
      <p className="text-muted-foreground text-xs">
        <Trans>
          Apps add new kinds of item to the drive. Switching one off here hides it from the New menu
          and stops its items from opening in this space; nothing is deleted, and switching it back
          on restores everything.
        </Trans>
      </p>
      <div className="divide-y rounded-lg border">
        {extensions.data.map((ext) => {
          const name = extensionName(ext, i18n);
          const id = `space-ext-${ext.id}`;
          const locked = !ext.instance;
          return (
            <div
              key={ext.id}
              className={cn("flex items-center gap-3 p-3", locked && "bg-muted/40")}
            >
              <ExtIconTile icon={extIconOf(ext.node_kinds)} muted={locked} />
              <div className="flex min-w-0 flex-1 flex-col gap-0.5">
                <Label
                  htmlFor={id}
                  className={cn(
                    "cursor-pointer gap-1.5 text-sm",
                    locked && "text-muted-foreground",
                  )}
                >
                  {name}
                  {locked ? <Lock className="size-3 shrink-0" aria-hidden /> : null}
                </Label>
                <span className="text-muted-foreground block text-xs">
                  {locked ? (
                    <Trans>Turned off for this install by an administrator.</Trans>
                  ) : ext.enabled ? (
                    <Trans>On in this space. Turn it off to hide it here.</Trans>
                  ) : (
                    <Trans>Off in this space.</Trans>
                  )}
                </span>
              </div>
              <Switch
                id={id}
                checked={ext.enabled}
                disabled={locked || setEnabled.isPending}
                onCheckedChange={(enabled) =>
                  setEnabled.mutate(
                    { extId: ext.id, enabled },
                    {
                      onError: (e) =>
                        toast.error(t`Could not update ${name}`, { description: e.message }),
                    },
                  )
                }
              />
            </div>
          );
        })}
      </div>
    </div>
  );
}
