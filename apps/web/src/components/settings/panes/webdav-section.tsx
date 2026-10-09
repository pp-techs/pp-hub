import { Trans, useLingui } from "@lingui/react/macro";
import { createModal } from "@buiducnhat/better-modal";
import { toast } from "sonner";
import { useQueryClient } from "@tanstack/react-query";
import { Button } from "@workspace/ui/components/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@workspace/ui/components/dialog";
import { keys, useCreateAgentToken, useMe } from "@/lib/queries";
import { useKeyedState } from "@/lib/use-keyed-state";
import { SectionLabel } from "@/components/section-label";
import { CopyField, CreateTokenForm, TokenList } from "./personal-tokens";

interface CreateDavTokenProps extends Record<string, unknown> {
  webdavUrl: string;
  username: string;
  /** Changes on every open, so a reopened dialog starts from an empty form. */
  openedAt: number;
}

/**
 * Names and mints a WebDAV token, then shows everything a client asks for —
 * server, username, password — with short per-client steps. The password is
 * the token itself and the server never returns it again.
 */
const CreateDavTokenDialog = createModal<CreateDavTokenProps>(
  "create-dav-token",
  ({ webdavUrl, username, openedAt, modal }) => {
    const { t } = useLingui();
    const create = useCreateAgentToken();
    const [secret, setSecret] = useKeyedState<number, string | null>(openedAt, null);

    const close = () => modal.resolve(undefined);

    return (
      <Dialog open={modal.visible} onOpenChange={(open) => !open && close()}>
        <DialogContent className="sm:max-w-lg">
          {secret ? (
            <>
              <DialogHeader>
                <DialogTitle>
                  <Trans>Copy your password now</Trans>
                </DialogTitle>
                <DialogDescription>
                  <Trans>
                    It will not be shown again. Anyone holding it can open your files as you.
                  </Trans>
                </DialogDescription>
              </DialogHeader>
              <CopyField
                id="dav-token-url"
                label={<Trans>Server</Trans>}
                value={webdavUrl}
                copied={t`Server URL copied`}
                copyFailed={t`Could not copy the server URL`}
              />
              <CopyField
                id="dav-token-username"
                label={<Trans>Username</Trans>}
                value={username}
                copied={t`Username copied`}
                copyFailed={t`Could not copy the username`}
              />
              <CopyField
                id="dav-token-secret"
                label={<Trans>Password</Trans>}
                value={secret}
                copied={t`Password copied`}
                copyFailed={t`Could not copy the password`}
              />
              <ul className="text-muted-foreground flex list-disc flex-col gap-1 pl-4 text-xs">
                <li>
                  <Trans>
                    <span className="text-foreground font-medium">rclone (recommended):</span> add a
                    WebDAV remote with these details, then run <code>rclone nfsmount</code> on macOS
                    (no macFUSE needed) or <code>rclone mount</code> with WinFsp on Windows. Add{" "}
                    <code>--vfs-cache-mode full</code> to either.
                  </Trans>
                </li>
                <li>
                  <Trans>
                    <span className="text-foreground font-medium">Finder:</span> Go › Connect to
                    Server, enter the server URL, then sign in.
                  </Trans>
                </li>
                <li>
                  <Trans>
                    <span className="text-foreground font-medium">Windows Explorer:</span> Map
                    network drive, enter the server URL. Works only over HTTPS, and files over about
                    50 MB fail unless the WebClient size limit is raised.
                  </Trans>
                </li>
              </ul>
              <DialogFooter>
                <Button onClick={close}>
                  <Trans>Done</Trans>
                </Button>
              </DialogFooter>
            </>
          ) : (
            <CreateTokenForm
              idPrefix="dav-token"
              openedAt={openedAt}
              title={<Trans>Create a WebDAV password</Trans>}
              description={
                <Trans>Name it after the computer or app that will mount the drive.</Trans>
              }
              placeholder={t`Finder on my laptop`}
              pending={create.isPending}
              onSubmit={(name, days) =>
                create.mutate(
                  { name, expires_in_days: days, purpose: "dav" },
                  {
                    onSuccess: (created) => setSecret(created.token),
                    onError: (e) =>
                      toast.error(t`Could not create the password`, { description: e.message }),
                  },
                )
              }
              onCancel={close}
            />
          )}
        </DialogContent>
      </Dialog>
    );
  },
);

/**
 * Mounting the drive in Finder, Explorer or rclone. WebDAV clients cannot
 * sign in through a browser, so each one gets its own token to use as the
 * password. Absent `webdav_url` means the administrator has not turned
 * WebDAV on, and the section is not shown at all.
 */
export function WebDavSection() {
  const { t } = useLingui();
  const qc = useQueryClient();
  const { data: me } = useMe();
  const webdavUrl = me?.webdav_url;
  if (!me || !webdavUrl) return null;

  async function createToken(url: string, username: string) {
    await CreateDavTokenDialog.show({ webdavUrl: url, username, openedAt: Date.now() });
    void qc.invalidateQueries({ queryKey: keys.agentTokens });
  }

  return (
    <section className="flex flex-col gap-3">
      <SectionLabel>
        <Trans>Mount as a drive (WebDAV)</Trans>
      </SectionLabel>
      <div className="flex items-start justify-between gap-4">
        <p className="text-muted-foreground text-[10px]">
          <Trans>
            Open your files in Finder, File Explorer or rclone. Sign in with your email and a
            password created here; each computer or app gets its own, and it expires.
          </Trans>
        </p>
        <Button
          variant="outline"
          size="sm"
          className="shrink-0"
          onClick={() => void createToken(webdavUrl, me.email)}
        >
          <Trans>Create password</Trans>
        </Button>
      </div>
      <CopyField
        id="webdav-url"
        label={<Trans>Server</Trans>}
        value={webdavUrl}
        copied={t`Server URL copied`}
        copyFailed={t`Could not copy the server URL`}
      />
      <TokenList purpose="dav" />
    </section>
  );
}
