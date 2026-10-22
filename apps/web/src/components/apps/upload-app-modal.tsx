import { useRef, useState } from "react";
import { toast } from "sonner";
import { Trans, useLingui } from "@lingui/react/macro";
import { FileArchive, FolderOpen, Upload } from "lucide-react";
import { createModal } from "@buiducnhat/better-modal";
import { Alert, AlertDescription } from "@workspace/ui/components/alert";
import { Button } from "@workspace/ui/components/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@workspace/ui/components/dialog";
import { ApiError } from "@/lib/api";
import { AppPackageError, MAX_APP_UPLOAD, zipFolder } from "@/lib/app-package";
import { useFormatters } from "@/lib/format";
import { useInstallApp } from "@/lib/queries";

interface UploadAppProps extends Record<string, unknown> {
  /** Updating an installed app: the upload must be this app. */
  update?: { id: string; name: string };
}

/** What was picked: the bytes to send and how to name them back. */
type Picked = { blob: Blob; label: string };

/**
 * Installs an app from a .zip or a folder, or uploads a new version of one
 * (M51). A folder is zipped here first; either way the server checks the
 * package and the app arrives switched off.
 */
export const UploadAppModal = createModal<UploadAppProps>("upload-app", ({ update, modal }) => {
  const { t } = useLingui();
  const f = useFormatters();
  const install = useInstallApp();
  const zipRef = useRef<HTMLInputElement>(null);
  const folderRef = useRef<HTMLInputElement>(null);
  const [picked, setPicked] = useState<Picked | null>(null);
  const [error, setError] = useState<string | null>(null);
  // An install refused because the id is taken can be retried as an update.
  const [takenId, setTakenId] = useState(false);
  const [zipping, setZipping] = useState(false);

  const close = () => {
    setPicked(null);
    setError(null);
    setTakenId(false);
    modal.resolve(undefined);
  };

  const pickZip = (files: FileList | null) => {
    const file = files?.[0];
    if (!file) return;
    setError(null);
    setTakenId(false);
    if (file.size > MAX_APP_UPLOAD) {
      setPicked(null);
      setError(t`A package is at most 20 MiB.`);
      return;
    }
    setPicked({ blob: file, label: file.name });
  };

  const pickFolder = async (files: FileList | null) => {
    if (!files || files.length === 0) return;
    setError(null);
    setTakenId(false);
    setZipping(true);
    const folder = files[0]!.webkitRelativePath.split("/")[0] || t`folder`;
    try {
      setPicked({ blob: await zipFolder([...files]), label: folder });
    } catch (e) {
      setPicked(null);
      if (e instanceof AppPackageError) {
        setError(
          e.reason === "empty"
            ? t`The folder holds no files to package.`
            : e.reason === "too-many-files"
              ? t`A package holds at most 2000 files.`
              : t`The folder is too large for a package (50 MiB, 20 MiB once compressed).`,
        );
      } else {
        setError(e instanceof Error ? e.message : String(e));
      }
    } finally {
      setZipping(false);
    }
  };

  const submit = (upgrade: boolean) => {
    if (!picked) return;
    install.mutate(
      { file: picked.blob, upgrade: upgrade || !!update, id: update?.id },
      {
        onSuccess: (app) => {
          const version = app.version;
          toast.success(update || upgrade ? t`Updated to v${version}` : t`App installed`, {
            description: update || upgrade ? undefined : t`It is off until you switch it on.`,
          });
          close();
        },
        onError: (e) => {
          const taken = !update && !upgrade && e instanceof ApiError && e.status === 409;
          setTakenId(taken);
          setError(
            taken
              ? t`This app is already installed. Update it with this package instead?`
              : e.message,
          );
        },
      },
    );
  };

  const updateName = update?.name;
  return (
    <Dialog open={modal.visible} onOpenChange={(open) => !open && close()}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>
            {update ? <Trans>Update {updateName}</Trans> : <Trans>Upload an app</Trans>}
          </DialogTitle>
          <DialogDescription>
            {update ? (
              <Trans>
                Upload the new version as a .zip or a folder. Items made with the app keep their
                data, and its switches stay as they are.
              </Trans>
            ) : (
              <Trans>
                A .zip or a folder holding a pp-app.json and the app’s built web files. The app runs
                in a sandbox and arrives switched off.
              </Trans>
            )}
          </DialogDescription>
        </DialogHeader>

        <div className="grid grid-cols-2 gap-2">
          <Button variant="outline" onClick={() => zipRef.current?.click()} disabled={zipping}>
            <FileArchive data-icon="inline-start" />
            <Trans>Choose .zip</Trans>
          </Button>
          <Button variant="outline" onClick={() => folderRef.current?.click()} disabled={zipping}>
            <FolderOpen data-icon="inline-start" />
            <Trans>Choose folder</Trans>
          </Button>
        </div>
        <input
          ref={zipRef}
          type="file"
          accept=".zip,application/zip"
          className="hidden"
          onChange={(e) => {
            pickZip(e.target.files);
            e.target.value = "";
          }}
        />
        <input
          ref={folderRef}
          type="file"
          multiple
          {...{ webkitdirectory: "" }}
          className="hidden"
          onChange={(e) => {
            void pickFolder(e.target.files);
            e.target.value = "";
          }}
        />

        {zipping && (
          <p className="text-muted-foreground text-xs">
            <Trans>Packaging the folder…</Trans>
          </p>
        )}
        {picked && !zipping && (
          <p className="bg-muted/50 flex items-center gap-2 rounded-md border px-3 py-2 text-xs">
            <FileArchive className="text-muted-foreground size-3.5 shrink-0" />
            <span className="min-w-0 flex-1 truncate font-medium">{picked.label}</span>
            <span className="text-muted-foreground tabular-nums">{f.size(picked.blob.size)}</span>
          </p>
        )}
        {error && (
          <Alert variant="destructive">
            <AlertDescription>{error}</AlertDescription>
          </Alert>
        )}

        <DialogFooter>
          {takenId ? (
            <Button onClick={() => submit(true)} disabled={install.isPending}>
              <Upload data-icon="inline-start" />
              <Trans>Update the installed app</Trans>
            </Button>
          ) : (
            <Button
              onClick={() => submit(false)}
              disabled={!picked || zipping || install.isPending}
            >
              <Upload data-icon="inline-start" />
              {update ? <Trans>Upload update</Trans> : <Trans>Install</Trans>}
            </Button>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
});
