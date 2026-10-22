import { createFileRoute } from "@tanstack/react-router";
import { Info, MoreHorizontal, Server, Trash2, Upload } from "lucide-react";
import { cn } from "@workspace/ui/lib/utils";
import { toast } from "sonner";
import { Trans, useLingui } from "@lingui/react/macro";
import { msg } from "@lingui/core/macro";
import { Button } from "@workspace/ui/components/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@workspace/ui/components/dropdown-menu";
import { Switch } from "@workspace/ui/components/switch";
import { Skeleton } from "@workspace/ui/components/skeleton";
import { Card } from "@workspace/ui/components/card";
import { AdminPage } from "@/components/admin-page";
import { AppBackendModal } from "@/components/apps/app-backend-modal";
import { RemoveAppModal } from "@/components/apps/remove-app-modal";
import { UploadAppModal } from "@/components/apps/upload-app-modal";
import { ExtIconTile, extIconOf } from "@/components/ext-icon-tile";
import { LoadingStatus } from "@/components/loading";
import { useExtRegistry } from "@/ext/registry";
import type { Extension } from "@/lib/api";
import { extensionsOptions, useExtensions, useSetInstanceExtension } from "@/lib/queries";
import { extensionName } from "@/lib/ext-name";
import { pageTitle } from "@/lib/i18n";

export const Route = createFileRoute("/admin/apps")({
  head: () => ({ meta: [{ title: pageTitle(msg`Apps`) }] }),
  // The `/admin` layout has already authenticated the caller as an admin.
  loader: ({ context }) => {
    void context.queryClient.ensureQueryData(extensionsOptions());
  },
  component: AdminAppsPage,
});

function AdminAppsPage() {
  const extensions = useExtensions();
  // Apps' names and icons come from the registry the list fills.
  useExtRegistry();

  return (
    <AdminPage
      title={<Trans>Apps</Trans>}
      description={
        <Trans>
          Apps you upload for this install. Every app arrives switched off: turn one on here to make
          it available, then space admins can switch it off again for their own space.
        </Trans>
      }
      actions={
        <Button onClick={() => void UploadAppModal.show({})}>
          <Upload data-icon="inline-start" />
          <Trans>Upload app</Trans>
        </Button>
      }
    >
      {extensions.isPending ? (
        <LoadingStatus>
          <Card className="gap-0 divide-y py-0">
            {[0, 1].map((i) => (
              <div key={i} className="flex items-center gap-4 p-4">
                <Skeleton className="size-9 rounded-lg" />
                <div className="flex flex-1 flex-col gap-1.5">
                  <Skeleton className="h-4 w-32" />
                  <Skeleton className="h-3.5 w-2/3" />
                </div>
                <Skeleton className="h-5 w-9" />
              </div>
            ))}
          </Card>
        </LoadingStatus>
      ) : extensions.isError ? (
        <p className="text-destructive text-sm">{extensions.error.message}</p>
      ) : extensions.data.length === 0 ? (
        <p className="text-muted-foreground text-sm">
          <Trans>No apps yet. Upload one to get started.</Trans>
        </p>
      ) : (
        <div className="flex flex-col gap-4">
          <Card className="gap-0 divide-y py-0">
            {extensions.data.map((ext) => (
              <AppRow key={ext.id} ext={ext} />
            ))}
          </Card>
          <div className="bg-muted/50 text-muted-foreground flex items-start gap-2.5 rounded-lg border p-3 text-xs/relaxed">
            <Info className="mt-0.5 size-3.5 shrink-0" />
            <p>
              <Trans>
                Turning an app off hides it from the New menu and stops its items from opening.
                Nothing is deleted; turning it back on restores everything. Apps run in a sandbox:
                they reach files only through the items they are opened on, with the access of
                whoever opens them.
              </Trans>
            </p>
          </div>
        </div>
      )}
    </AdminPage>
  );
}

function AppRow({ ext }: { ext: Extension }) {
  const { t, i18n } = useLingui();
  const setEnabled = useSetInstanceExtension();
  const name = extensionName(ext, i18n);
  const id = `ext-${ext.id}`;
  const { author, version } = ext;

  return (
    <div className="flex items-center gap-4 p-4">
      <ExtIconTile icon={extIconOf(ext.node_kinds)} muted={!ext.instance} />
      <div className="flex min-w-0 flex-1 flex-col gap-1">
        <div className="flex flex-wrap items-baseline gap-2">
          <label htmlFor={id} className="text-sm font-medium">
            {name}
          </label>
          {ext.version && (
            <span className="text-muted-foreground text-[0.625rem] tabular-nums">
              <Trans>v{version}</Trans>
            </span>
          )}
        </div>
        {ext.description && (
          <p className="text-muted-foreground text-xs/relaxed">{ext.description}</p>
        )}
        {ext.author && (
          <p className="text-muted-foreground text-[0.625rem]">
            <Trans>By {author}</Trans>
          </p>
        )}
        <p className="text-muted-foreground flex items-center gap-1.5 text-xs/relaxed">
          <span
            aria-hidden
            className={cn(
              "size-1.5 shrink-0 rounded-full",
              ext.instance ? "bg-primary" : "bg-muted-foreground/40",
            )}
          />
          {ext.instance ? (
            <Trans>On for this install. Spaces can still switch it off.</Trans>
          ) : (
            <Trans>Off. Nobody can create or open its items.</Trans>
          )}
        </p>
      </div>
      <Switch
        id={id}
        aria-label={t`Turn ${name} on for this install`}
        checked={ext.instance}
        disabled={setEnabled.isPending}
        onCheckedChange={(enabled) =>
          setEnabled.mutate(
            { extId: ext.id, enabled },
            {
              onError: (e) => toast.error(t`Could not update ${name}`, { description: e.message }),
            },
          )
        }
      />
      <DropdownMenu>
        <DropdownMenuTrigger
          render={<Button variant="ghost" size="icon-sm" aria-label={t`Manage ${name}`} />}
        >
          <MoreHorizontal />
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end">
          <DropdownMenuItem
            onClick={() => void UploadAppModal.show({ update: { id: ext.id, name } })}
          >
            <Upload />
            <Trans>Upload new version…</Trans>
          </DropdownMenuItem>
          <DropdownMenuItem onClick={() => void AppBackendModal.show({ extId: ext.id, name })}>
            <Server />
            <Trans>Backend…</Trans>
          </DropdownMenuItem>
          <DropdownMenuSeparator />
          <DropdownMenuItem
            variant="destructive"
            onClick={() => void RemoveAppModal.show({ extId: ext.id, name })}
          >
            <Trash2 />
            <Trans>Remove…</Trans>
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
    </div>
  );
}
