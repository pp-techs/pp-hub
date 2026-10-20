import { createFileRoute, useParams } from "@tanstack/react-router";
import { useState } from "react";
import { Trans, useLingui } from "@lingui/react/macro";
import { msg } from "@lingui/core/macro";
import { ExternalLink, Globe, Lock } from "lucide-react";
import { Button } from "@workspace/ui/components/button";
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@workspace/ui/components/empty";
import { Input } from "@workspace/ui/components/input";
import { Label } from "@workspace/ui/components/label";
import { Skeleton } from "@workspace/ui/components/skeleton";
import { ApiError } from "@/lib/api";
import {
  publicPublicationOptions,
  usePublicPublication,
  useUnlockPublication,
} from "@/lib/queries";
import { pageTitle } from "@/lib/i18n";
import { BrandMark } from "@/components/brand-mark";
import { LoadingStatus } from "@/components/loading";

/**
 * A published site (M50), as anyone with the link sees it. Anonymous, like
 * `/s/$token`: no auth guard, nothing here reads the session.
 *
 * The frame loads the bytes from `content_url`: the artifact origin when the
 * server has one, otherwise `/a/` on this origin. Either way the responses
 * carry their own sandboxing CSP, and the frame is sandboxed here too —
 * never with `allow-same-origin`, which under `/a/` would hand a published
 * page this origin's storage and cookies.
 */
export const Route = createFileRoute("/p/$token")({
  head: () => ({ meta: [{ title: pageTitle(msg`Published site`) }] }),
  loader: ({ context, params }) => {
    void context.queryClient.ensureQueryData(publicPublicationOptions(params.token));
  },
  component: PublishedViewer,
});

/** What a published page may do inside the frame; mirrors the artifact host's CSP `sandbox`. */
const SANDBOX =
  // eslint-disable-next-line lingui/no-unlocalized-strings -- sandbox tokens
  "allow-scripts allow-popups allow-popups-to-escape-sandbox allow-modals allow-downloads";

function PublishedViewer() {
  const { token } = useParams({ from: "/p/$token" });
  const { t } = useLingui();
  const info = usePublicPublication(token);
  const unlock = useUnlockPublication(token);
  // The grant-bearing URL a correct password bought; never stored beyond this page.
  const [unlockedUrl, setUnlockedUrl] = useState<string | null>(null);
  const [password, setPassword] = useState("");
  const [wrongPassword, setWrongPassword] = useState(false);

  if (info.isPending) {
    return (
      <Shell>
        <LoadingStatus className="flex flex-1 flex-col gap-3 p-4">
          <Skeleton className="h-4 w-64" />
          <Skeleton className="w-full flex-1" />
        </LoadingStatus>
      </Shell>
    );
  }

  if (info.isError) {
    return (
      <Shell>
        <Empty className="py-16">
          <EmptyHeader>
            <EmptyMedia>
              <Globe className="size-10" />
            </EmptyMedia>
            <EmptyTitle>
              <Trans>This site is not available</Trans>
            </EmptyTitle>
            <EmptyDescription>
              <Trans>
                It may have expired, been unpublished, or never existed. Ask whoever sent it for a
                new link.
              </Trans>
            </EmptyDescription>
          </EmptyHeader>
        </Empty>
      </Shell>
    );
  }

  const pub = info.data;
  const contentUrl = unlockedUrl ?? pub.content_url ?? null;
  const publisher = pub.published_by;

  return (
    <Shell
      bar={
        <>
          <span className="bg-border mx-1 h-4 w-px shrink-0" />
          <div className="flex min-w-0 flex-col">
            <span className="truncate text-sm font-medium">{pub.title}</span>
            <span className="text-muted-foreground truncate text-[11px]">
              {publisher ? (
                <Trans>Published by {publisher} · User-generated content</Trans>
              ) : (
                <Trans>User-generated content</Trans>
              )}
            </span>
          </div>
          {contentUrl && (
            <Button
              variant="outline"
              size="sm"
              className="ml-auto shrink-0"
              render={<a href={contentUrl} target="_blank" rel="noopener noreferrer" />}
            >
              <ExternalLink data-icon="inline-start" />
              <Trans>Open in new tab</Trans>
            </Button>
          )}
        </>
      }
    >
      {contentUrl ? (
        <iframe
          src={contentUrl}
          title={pub.title}
          sandbox={SANDBOX}
          referrerPolicy="no-referrer"
          className="min-h-0 w-full flex-1 border-0 bg-white"
        />
      ) : (
        <div className="flex flex-1 items-center justify-center p-6">
          <form
            className="flex w-full max-w-sm flex-col gap-3"
            onSubmit={(e) => {
              e.preventDefault();
              if (!password) return;
              setWrongPassword(false);
              unlock.mutate(password, {
                onSuccess: (r) => setUnlockedUrl(r.content_url),
                onError: (err) => {
                  if (err instanceof ApiError && err.status === 422) setWrongPassword(true);
                },
              });
            }}
          >
            <div className="flex items-center gap-2">
              <Lock className="size-4 shrink-0" />
              <h1 className="text-sm font-semibold">
                <Trans>This site is password protected</Trans>
              </h1>
            </div>
            <div className="grid gap-1.5">
              <Label htmlFor="publication-password">
                <Trans>Password</Trans>
              </Label>
              <Input
                id="publication-password"
                type="password"
                autoComplete="current-password"
                autoFocus
                value={password}
                aria-invalid={wrongPassword}
                onChange={(e) => {
                  setPassword(e.target.value);
                  setWrongPassword(false);
                }}
              />
            </div>
            {wrongPassword && (
              <p role="alert" className="text-destructive text-sm">
                <Trans>That password is not right.</Trans>
              </p>
            )}
            {unlock.isError && !wrongPassword && (
              <p role="alert" className="text-destructive text-sm">
                {unlock.error instanceof ApiError && unlock.error.message
                  ? unlock.error.message
                  : t`Could not open this site. Try again.`}
              </p>
            )}
            <Button type="submit" disabled={!password || unlock.isPending}>
              <Trans>Open</Trans>
            </Button>
          </form>
        </div>
      )}
    </Shell>
  );
}

function Shell({ bar, children }: { bar?: React.ReactNode; children: React.ReactNode }) {
  return (
    <div className="bg-background text-foreground flex h-svh flex-col">
      <header className="border-border flex h-12 shrink-0 items-center gap-2 border-b px-4">
        <BrandMark className="size-6" />
        {bar}
      </header>
      {children}
    </div>
  );
}
