import { useState } from "react";
import { toast } from "sonner";
import { Trans, useLingui } from "@lingui/react/macro";
import { Copy, KeyRound } from "lucide-react";
import { createModal } from "@buiducnhat/better-modal";
import { Alert, AlertDescription, AlertTitle } from "@workspace/ui/components/alert";
import { Button } from "@workspace/ui/components/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@workspace/ui/components/dialog";
import { Input } from "@workspace/ui/components/input";
import { Label } from "@workspace/ui/components/label";
import { Skeleton } from "@workspace/ui/components/skeleton";
import { useKeyedState } from "@/lib/use-keyed-state";
import { useAdminApp, useRotateAppSecret, useUpdateApp } from "@/lib/queries";

interface AppBackendProps extends Record<string, unknown> {
  extId: string;
  name: string;
}

/**
 * Where an uploaded app's backend is, and the secret it checks pp-hub's
 * tokens with (M51). The secret is shown once, when it is made; the server
 * keeps it sealed.
 */
export const AppBackendModal = createModal<AppBackendProps>(
  "app-backend",
  ({ extId, name, modal }) => {
    const { t } = useLingui();
    const app = useAdminApp(extId, modal.visible);
    const update = useUpdateApp(extId);
    const rotate = useRotateAppSecret(extId);
    const [url, setUrl] = useKeyedState(
      `${extId}:${modal.visible}:${app.data?.backend_url}`,
      app.data?.backend_url ?? "",
    );
    const [secret, setSecret] = useState<string | null>(null);

    const close = () => {
      setSecret(null);
      modal.resolve(undefined);
    };

    const save = () =>
      update.mutate(url.trim(), {
        onSuccess: () => toast.success(t`Saved where ${name} reaches its backend`),
        onError: (e) => toast.error(t`Could not save`, { description: e.message }),
      });

    const makeSecret = () =>
      rotate.mutate(undefined, {
        onSuccess: (res) => setSecret(res.secret),
        onError: (e) => toast.error(t`Could not generate a secret`, { description: e.message }),
      });

    const copy = (value: string) =>
      navigator.clipboard.writeText(value).then(
        () => toast.success(t`Copied`),
        () => toast.error(t`Could not copy`),
      );

    const manifestURL = app.data?.manifest_backend_url ?? "";
    return (
      <Dialog open={modal.visible} onOpenChange={(open) => !open && close()}>
        <DialogContent className="sm:max-w-lg">
          <DialogHeader>
            <DialogTitle>
              <Trans>{name} backend</Trans>
            </DialogTitle>
            <DialogDescription>
              <Trans>
                The app’s own server. PP Hub forwards the app’s requests to it with a short-lived
                token saying who is asking, signed with the secret below.
              </Trans>
            </DialogDescription>
          </DialogHeader>

          {app.isPending ? (
            <div className="flex flex-col gap-3">
              <Skeleton className="h-8 w-full" />
              <Skeleton className="h-16 w-full" />
            </div>
          ) : (
            <div className="flex flex-col gap-5">
              <div className="flex flex-col gap-2">
                <Label htmlFor="app-backend-url">
                  <Trans>Address</Trans>
                </Label>
                <div className="flex gap-2">
                  <Input
                    id="app-backend-url"
                    value={url}
                    onChange={(e) => setUrl(e.target.value)}
                    placeholder={manifestURL || t`https://…`}
                    inputMode="url"
                  />
                  <Button
                    variant="outline"
                    onClick={save}
                    disabled={update.isPending || url.trim() === (app.data?.backend_url ?? "")}
                  >
                    <Trans>Save</Trans>
                  </Button>
                </div>
                <p className="text-muted-foreground text-xs">
                  {manifestURL ? (
                    <Trans>Empty uses the address the app ships with: {manifestURL}</Trans>
                  ) : (
                    <Trans>The app ships with no address; set one here.</Trans>
                  )}
                </p>
              </div>

              <div className="flex flex-col gap-2">
                <Label>
                  <Trans>Secret</Trans>
                </Label>
                {secret ? (
                  <Alert>
                    <KeyRound />
                    <AlertTitle>
                      <Trans>Copy it now — it is not shown again</Trans>
                    </AlertTitle>
                    <AlertDescription className="flex flex-col gap-2">
                      <code className="bg-muted break-all rounded px-2 py-1 font-mono text-xs">
                        {secret}
                      </code>
                      <Button
                        variant="outline"
                        size="sm"
                        className="self-start"
                        onClick={() => void copy(secret)}
                      >
                        <Copy data-icon="inline-start" />
                        <Trans>Copy secret</Trans>
                      </Button>
                    </AlertDescription>
                  </Alert>
                ) : (
                  <p className="text-muted-foreground text-xs">
                    {app.data?.has_secret ? (
                      <Trans>
                        A secret is set. Generating a new one stops the old one at once; give the
                        backend the new one.
                      </Trans>
                    ) : (
                      <Trans>
                        No secret yet: PP Hub will not call the backend until one is generated and
                        given to it.
                      </Trans>
                    )}
                  </p>
                )}
                {!secret && (
                  <Button
                    variant="outline"
                    size="sm"
                    className="self-start"
                    onClick={makeSecret}
                    disabled={rotate.isPending}
                  >
                    <KeyRound data-icon="inline-start" />
                    {app.data?.has_secret ? (
                      <Trans>Generate a new secret</Trans>
                    ) : (
                      <Trans>Generate secret</Trans>
                    )}
                  </Button>
                )}
              </div>
            </div>
          )}

          <DialogFooter>
            <Button variant="outline" onClick={close}>
              <Trans>Done</Trans>
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    );
  },
);
