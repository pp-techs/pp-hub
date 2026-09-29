import { useState } from "react";
import { createModal } from "@buiducnhat/better-modal";
import { Trans, useLingui } from "@lingui/react/macro";
import { Loader2 } from "lucide-react";
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
import { unfurlLink, type UnfurledLink } from "@/lib/api";
import { useKeyedState } from "@/lib/use-keyed-state";

type Props = { documentId: string };

/**
 * `https://` is assumed when the scheme is left off, as a browser's address
 * bar does; anything but http(s) is refused here as the server would.
 */
function normalizeLinkInput(input: string): string | null {
  const trimmed = input.trim();
  if (!trimmed) return null;
  // eslint-disable-next-line lingui/no-unlocalized-strings -- URL scheme
  const candidate = /^[a-z][a-z0-9+.-]*:/i.test(trimmed) ? trimmed : `https://${trimmed}`;
  try {
    const url = new URL(candidate);
    return url.protocol === "http:" || url.protocol === "https:" ? url.href : null;
  } catch {
    return null;
  }
}

/**
 * Asks for the link a bookmark card shows, and has the server read it: the
 * card's title, description and thumbnail come back from `unfurl`, the
 * thumbnail already stored as an attachment of the document. When the site
 * cannot be read, the link can still go in as a card with just its address.
 * Resolves `null` when dismissed.
 */
export const EmbedLinkModal = createModal<Props, UnfurledLink | null>(
  "embed-link",
  ({ documentId, modal }) => {
    const { t } = useLingui();
    const [input, setInput] = useKeyedState(modal.visible, "");
    const [error, setError] = useKeyedState<boolean, string | null>(modal.visible, null);
    const [pending, setPending] = useState(false);
    const url = normalizeLinkInput(input);

    const submit = async () => {
      if (!url || pending) return;
      setPending(true);
      setError(null);
      try {
        modal.resolve(await unfurlLink(documentId, url));
      } catch (e) {
        setError(e instanceof Error && e.message ? e.message : t`The link could not be read.`);
      } finally {
        setPending(false);
      }
    };

    return (
      <Dialog open={modal.visible} onOpenChange={(open) => !open && modal.resolve(null)}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>
              <Trans>Embed a link</Trans>
            </DialogTitle>
            <DialogDescription>
              <Trans>Shows the page as a card with its title, description and preview image.</Trans>
            </DialogDescription>
          </DialogHeader>
          <form
            className="flex flex-col gap-3"
            onSubmit={(e) => {
              e.preventDefault();
              void submit();
            }}
          >
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="embed-link-url">
                <Trans>Link</Trans>
              </Label>
              <Input
                id="embed-link-url"
                type="text"
                inputMode="url"
                // eslint-disable-next-line lingui/no-unlocalized-strings -- example URL
                placeholder="https://example.com/article"
                value={input}
                onChange={(e) => {
                  setInput(e.target.value);
                  setError(null);
                }}
                aria-invalid={!!error || (input.trim() !== "" && !url)}
                autoFocus
                required
              />
              {error ? (
                <p role="alert" className="text-destructive text-xs">
                  {error}
                </p>
              ) : input.trim() !== "" && !url ? (
                <p className="text-muted-foreground text-xs">
                  <Trans>Enter a web address starting with http:// or https://.</Trans>
                </p>
              ) : null}
            </div>
            <DialogFooter>
              <Button type="button" variant="outline" onClick={() => modal.resolve(null)}>
                <Trans>Cancel</Trans>
              </Button>
              {error && url ? (
                <Button
                  type="button"
                  variant="secondary"
                  onClick={() => modal.resolve({ url, title: "", description: "" })}
                >
                  <Trans>Add without preview</Trans>
                </Button>
              ) : null}
              <Button type="submit" disabled={!url || pending}>
                {pending ? <Loader2 data-icon="inline-start" className="animate-spin" /> : null}
                <Trans>Embed</Trans>
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
    );
  },
);
