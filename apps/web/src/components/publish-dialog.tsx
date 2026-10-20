import { useState } from "react";
import { createModal } from "@buiducnhat/better-modal";
import { Plural, Trans, useLingui } from "@lingui/react/macro";
import { msg } from "@lingui/core/macro";
import type { MessageDescriptor } from "@lingui/core";
import { toast } from "sonner";
import { Copy, ExternalLink, Globe, TriangleAlert } from "lucide-react";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@workspace/ui/components/alert-dialog";
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
import { Input } from "@workspace/ui/components/input";
import { Label } from "@workspace/ui/components/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@workspace/ui/components/select";
import { Skeleton } from "@workspace/ui/components/skeleton";
import { Switch } from "@workspace/ui/components/switch";
import { ApiError, type Node, type Publication, type PublicationBody } from "@/lib/api";
import {
  useNodeChildren,
  usePublication,
  usePublish,
  useUnpublish,
  useUpdatePublication,
} from "@/lib/queries";
import { useFormatters } from "@/lib/format";
import { LoadingStatus } from "@/components/loading";

type Props = { node: Node };

/** A folder's entry when the publisher names none. */
// eslint-disable-next-line lingui/no-unlocalized-strings -- file name
const DEFAULT_ENTRY = "index.html";

/** The files a browser renders as a page, the only ones worth offering as an entry. */
const HTML_NAME = /\.html?$/i;

/**
 * How long a publication lasts. "keep" leaves a deadline already set alone
 * (PATCH and republish treat an absent `expires_at` as "unchanged"); it is
 * offered only when there is one to keep.
 */
type Expiry = "keep" | "never" | "1" | "7" | "30";

const EXPIRY_PRESETS: { value: Expiry; label: MessageDescriptor }[] = [
  { value: "never", label: msg`Never` },
  { value: "1", label: msg`1 day` },
  { value: "7", label: msg`7 days` },
  { value: "30", label: msg`30 days` },
];

const message = (e: unknown, fallback: string) =>
  e instanceof ApiError ? e.message || fallback : fallback;

/**
 * Publishes an `.html` file or a folder as a public mini site (M50), and
 * manages it once published: the URL, republishing a new snapshot, password
 * and expiry, unpublishing.
 *
 * The publication is a snapshot, so the dialog says so whenever the source
 * has moved on since (`stale`): nothing a visitor sees changes until
 * Republish.
 */
export const PublishDialog = createModal<Props, null>("publish", ({ node, modal }) => {
  const live = modal.visible;
  const { t } = useLingui();
  // Named local so the placeholder reaches a translator as `{nodeName}`.
  const nodeName = node.name;
  const publication = usePublication(live ? node.id : null);
  const pub = publication.data;

  return (
    <Dialog open={live} onOpenChange={(open) => !open && modal.resolve(null)}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Globe className="size-4 shrink-0" />
            <Trans>Publish “{nodeName}”</Trans>
          </DialogTitle>
          <DialogDescription>
            {node.kind === "dir" ? (
              <Trans>
                Turns this folder into a public web site that anyone with the link can open, scripts
                included. Visitors see a snapshot: later edits stay private until you republish.
              </Trans>
            ) : (
              <Trans>
                Turns this page into a public web site that anyone with the link can open, scripts
                included. Visitors see a snapshot: later edits stay private until you republish.
              </Trans>
            )}
          </DialogDescription>
        </DialogHeader>

        {publication.isPending ? (
          <LoadingStatus className="flex flex-col gap-3">
            <Skeleton className="h-8 w-full" />
            <Skeleton className="h-8 w-2/3" />
            <Skeleton className="h-8 w-1/2" />
          </LoadingStatus>
        ) : publication.isError ? (
          <p className="text-destructive text-sm">
            {message(publication.error, t`Could not load this publication.`)}
          </p>
        ) : (
          // Remounted per opening and per server answer: better-modal keeps a
          // closed modal mounted, and a password typed for the last save must
          // not linger once the server holds it.
          <PublishForm
            key={`${live}:${node.id}:${pub ? `${pub.rev}:${pub.has_password}:${pub.expires_at}` : ""}`}
            node={node}
            pub={pub ?? null}
            onClose={() => modal.resolve(null)}
          />
        )}
      </DialogContent>
    </Dialog>
  );
});

function PublishForm({
  node,
  pub,
  onClose,
}: {
  node: Node;
  pub: Publication | null;
  onClose: () => void;
}) {
  const { t, i18n } = useLingui();
  const f = useFormatters();
  const isFolder = node.kind === "dir";
  const publish = usePublish(node.id);
  const update = useUpdatePublication(node.id);
  const unpublish = useUnpublish(node.id);
  const busy = publish.isPending || update.isPending || unpublish.isPending;

  const children = useNodeChildren(isFolder ? node.id : null);
  const htmlFiles = (children.data ?? [])
    .filter((c) => c.kind === "file" && HTML_NAME.test(c.name))
    .map((c) => c.name);
  const hasIndex = htmlFiles.includes(DEFAULT_ENTRY);
  // The doc's rule: a select when the folder has no index.html at its root,
  // so the publisher picks among the pages that are there. Otherwise a field,
  // which also takes a nested path such as `dist/index.html`.
  const offerSelect = children.isSuccess && !hasIndex && htmlFiles.length > 0;
  // `null` until the publisher touches it: the default follows the listing,
  // which may arrive after the first paint.
  const [entryPath, setEntryPath] = useState<string | null>(null);
  const defaultEntry =
    pub?.entry_path ?? (offerSelect ? (htmlFiles[0] ?? DEFAULT_ENTRY) : DEFAULT_ENTRY);
  const entry = entryPath ?? defaultEntry;
  const entryItems = [...new Set([...htmlFiles, ...(pub ? [pub.entry_path] : [])])].map((n) => ({
    value: n,
    label: n,
  }));

  const [usePassword, setUsePassword] = useState(pub?.has_password ?? false);
  const [password, setPassword] = useState("");
  // eslint-disable-next-line lingui/no-unlocalized-strings -- expiry choice ids
  const [expiry, setExpiry] = useState<Expiry>(pub?.expires_at ? "keep" : "never");
  // The server's 422 (no entry, a bad entry), shown where it can be fixed.
  const [formError, setFormError] = useState<string | null>(null);
  const [confirmUnpublish, setConfirmUnpublish] = useState(false);

  // Named local so the extracted message reads `{expiresOn}`.
  const expiresOn = pub?.expires_at ? f.date(pub.expires_at) : "";
  const expiryItems = [
    ...(pub?.expires_at
      ? [{ value: "keep" as Expiry, label: t`Keep (expires ${expiresOn})` }]
      : []),
    ...EXPIRY_PRESETS.map((p) => ({ value: p.value, label: i18n._(p.label) })),
  ];

  /**
   * Password and expiry as the API reads them: a value sets, `null` clears,
   * absent keeps. A string is a problem to show instead of a request.
   */
  function settingsBody(): PublicationBody | string {
    const body: PublicationBody = {};
    if (usePassword) {
      if (password) body.password = password;
      else if (!pub?.has_password) return t`Enter a password, or turn the password off.`;
    } else if (pub?.has_password) {
      body.password = null;
    }
    if (expiry === "never") {
      if (pub?.expires_at) body.expires_at = null;
    } else if (expiry !== "keep") {
      body.expires_at = new Date(Date.now() + Number(expiry) * 86_400_000).toISOString();
    }
    return body;
  }

  function onError(e: unknown, fallback: string) {
    if (e instanceof ApiError && e.status === 422) {
      setFormError(e.message || fallback);
      return;
    }
    toast.error(message(e, fallback));
  }

  function runPublish() {
    const body = settingsBody();
    if (typeof body === "string") {
      setFormError(body);
      return;
    }
    setFormError(null);
    const trimmed = entry.trim();
    if (isFolder && trimmed) body.entry_path = trimmed;
    publish.mutate(body, {
      onSuccess: () => toast.success(pub ? t`Republished` : t`Published`),
      onError: (e) => onError(e, pub ? t`Could not republish` : t`Could not publish`),
    });
  }

  const pendingSettings = pub ? settingsBody() : null;
  const settingsChanged =
    typeof pendingSettings === "object" &&
    pendingSettings !== null &&
    Object.keys(pendingSettings).length > 0;

  function saveSettings() {
    const body = settingsBody();
    if (typeof body === "string") {
      setFormError(body);
      return;
    }
    setFormError(null);
    update.mutate(body, {
      onSuccess: () => toast.success(t`Settings saved`),
      onError: (e) => onError(e, t`Could not save settings`),
    });
  }

  function runUnpublish() {
    unpublish.mutate(undefined, {
      onSuccess: () => {
        toast.success(t`Unpublished`);
        onClose();
      },
      onError: (e) => toast.error(message(e, t`Could not unpublish`)),
    });
  }

  const fileCount = pub?.file_count ?? 0;
  const rev = pub?.rev ?? 0;
  const publishedAt = pub ? f.date(pub.published_at) : "";
  const totalSize = pub ? f.size(pub.total_bytes) : "";

  return (
    <div className="flex flex-col gap-4">
      {pub && (
        <section className="flex flex-col gap-2 rounded-lg border bg-muted/20 p-3">
          <div className="flex items-center gap-2">
            <Input
              readOnly
              value={pub.url}
              aria-label={t`Published address`}
              onFocus={(e) => e.currentTarget.select()}
            />
            <Button
              variant="outline"
              size="sm"
              onClick={() =>
                void navigator.clipboard.writeText(pub.url).then(
                  () => toast.success(t`Link copied`),
                  () => toast.error(t`Could not copy the link`),
                )
              }
            >
              <Copy data-icon="inline-start" />
              <Trans>Copy</Trans>
            </Button>
            <Button
              variant="outline"
              size="sm"
              render={<a href={pub.url} target="_blank" rel="noopener noreferrer" />}
            >
              <ExternalLink data-icon="inline-start" />
              <Trans>Open</Trans>
            </Button>
          </div>
          <p className="text-muted-foreground text-xs">
            <Trans>
              Revision {rev} · published {publishedAt} ·{" "}
              <Plural value={fileCount} one="# file" other="# files" /> · {totalSize}
            </Trans>
          </p>
          {pub.stale && (
            <Alert>
              <TriangleAlert />
              <AlertDescription>
                <Trans>Changed since last published. Republish to show the latest version.</Trans>
              </AlertDescription>
            </Alert>
          )}
        </section>
      )}

      {isFolder && (
        <div className="grid gap-1.5">
          <Label htmlFor="publish-entry">
            <Trans>Entry page</Trans>
          </Label>
          {offerSelect ? (
            <Select
              value={entry}
              items={entryItems}
              onValueChange={(v) => {
                setEntryPath((v as string | null) ?? null);
                setFormError(null);
              }}
            >
              <SelectTrigger id="publish-entry" className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {entryItems.map((item) => (
                  <SelectItem key={item.value} value={item.value}>
                    {item.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          ) : (
            <Input
              id="publish-entry"
              value={entry}
              placeholder={DEFAULT_ENTRY}
              aria-invalid={formError !== null}
              onChange={(e) => {
                setEntryPath(e.target.value);
                setFormError(null);
              }}
            />
          )}
          <p className="text-muted-foreground text-xs">
            {pub ? (
              <Trans>
                The page visitors land on, relative to this folder. Applies on republish.
              </Trans>
            ) : (
              <Trans>The page visitors land on, relative to this folder.</Trans>
            )}
          </p>
        </div>
      )}

      <div className="grid gap-1.5">
        <div className="flex items-center justify-between gap-3">
          <Label htmlFor="publish-password-toggle">
            <Trans>Require a password</Trans>
          </Label>
          <Switch
            id="publish-password-toggle"
            checked={usePassword}
            onCheckedChange={(v) => {
              setUsePassword(v);
              setFormError(null);
            }}
          />
        </div>
        {usePassword && (
          <Input
            id="publish-password"
            type="password"
            autoComplete="new-password"
            value={password}
            aria-label={t`Password`}
            placeholder={pub?.has_password ? t`Unchanged` : t`Password`}
            onChange={(e) => {
              setPassword(e.target.value);
              setFormError(null);
            }}
          />
        )}
      </div>

      <div className="grid gap-1.5">
        <Label htmlFor="publish-expiry">
          <Trans>Expiration</Trans>
        </Label>
        <Select
          value={expiry}
          items={expiryItems}
          onValueChange={(v) => setExpiry((v as Expiry | null) ?? "never")}
        >
          <SelectTrigger id="publish-expiry" className="w-full">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {expiryItems.map((item) => (
              <SelectItem key={item.value} value={item.value}>
                {item.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>

      {formError && (
        <p role="alert" className="text-destructive text-sm">
          {formError}
        </p>
      )}

      <DialogFooter className="gap-2 sm:justify-between">
        {pub ? (
          <>
            <Button variant="destructive" disabled={busy} onClick={() => setConfirmUnpublish(true)}>
              <Trans>Unpublish</Trans>
            </Button>
            <div className="flex flex-col-reverse gap-2 sm:flex-row">
              <Button variant="outline" disabled={busy || !settingsChanged} onClick={saveSettings}>
                <Trans>Save settings</Trans>
              </Button>
              <Button disabled={busy} onClick={runPublish}>
                <Trans>Republish</Trans>
              </Button>
            </div>
          </>
        ) : (
          <>
            <Button variant="outline" onClick={onClose}>
              <Trans>Cancel</Trans>
            </Button>
            <Button disabled={busy} onClick={runPublish}>
              <Globe data-icon="inline-start" />
              <Trans>Publish</Trans>
            </Button>
          </>
        )}
      </DialogFooter>

      {/* Inline rather than a nested better-modal, as in the share dialog: the
          confirmation belongs to this dialog and must not outlive it. */}
      <AlertDialog open={confirmUnpublish} onOpenChange={setConfirmUnpublish}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              <Trans>Unpublish this site?</Trans>
            </AlertDialogTitle>
            <AlertDialogDescription>
              <Trans>
                The address stops working at once for everyone who has it. Publishing again later
                gives a new address.
              </Trans>
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>
              <Trans>Cancel</Trans>
            </AlertDialogCancel>
            <AlertDialogAction
              variant="destructive"
              onClick={(e) => {
                e.preventDefault();
                setConfirmUnpublish(false);
                runUnpublish();
              }}
            >
              <Trans>Unpublish</Trans>
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
