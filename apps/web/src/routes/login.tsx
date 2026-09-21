import { createFileRoute, redirect } from "@tanstack/react-router";
import { Trans, useLingui } from "@lingui/react/macro";
import { msg } from "@lingui/core/macro";
import type { MessageDescriptor } from "@lingui/core";
import type * as React from "react";
import { FileText, HardDrive, Lock, LogIn, Users } from "lucide-react";
import type { LucideIcon } from "lucide-react";
import {
  Card,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from "@workspace/ui/components/card";
import { Alert, AlertDescription, AlertTitle } from "@workspace/ui/components/alert";
import { Button } from "@workspace/ui/components/button";
import { fetchMe } from "@/lib/queries";
import { safePath } from "@/lib/safe-path";
import { pageTitle } from "@/lib/i18n";

/**
 * Why a sign-in attempt came back here.
 *
 * `/auth/callback` is a top-level navigation, so it cannot answer with the
 * problem+json the rest of the server uses — that would render as raw JSON in
 * front of somebody who only tried to log in. It logs the cause and redirects
 * with a code instead, and this is where the code becomes a sentence.
 * Anything unrecognised is an OAuth error parameter passed through by the
 * provider.
 *
 * The map holds message *descriptors*, not rendered strings: this is module
 * scope, evaluated once at import time, long before a catalog is active.
 * Rendered text here would pin whichever language happened to load first and
 * would never follow a language switch. `i18n._(descriptor)` at render time
 * is what makes it reactive.
 */
const ERRORS: Record<string, MessageDescriptor> = {
  state: msg`That sign-in link expired or was already used. Start again from this page.`,
  exchange: msg`The identity provider refused to complete the sign-in. Try again.`,
  token: msg`The identity provider's response could not be verified. Try again.`,
  access_denied: msg`Sign-in was cancelled.`,
  suspended: msg`This account is suspended. Contact an administrator.`,
  "email-taken": msg`An account already exists with this email address under a different identity provider. Contact an administrator.`,
  internal: msg`Something went wrong on our side. Try again in a moment.`,
};

export const Route = createFileRoute("/login")({
  // `head` runs outside React, at navigation time, so it reaches the i18n
  // singleton directly instead of a hook. Because it re-runs per navigation
  // and not per render, a title set here follows the language on the next
  // route change rather than instantly — an accepted trade for not
  // re-rendering the document head on every keystroke.
  head: () => ({ meta: [{ title: pageTitle(msg`Sign in`) }] }),
  validateSearch: (search: Record<string, unknown>): { redirect?: string; error?: string } => ({
    // Only a same-origin path is honoured: an absolute URL — or the
    // protocol-relative shapes a bare "/" prefix test lets through — would
    // make the sign-in page an open redirect. `safePath` is the same rule the
    // server applies to `next`.
    redirect: safePath(search.redirect),
    error: typeof search.error === "string" ? search.error : undefined,
  }),
  beforeLoad: async ({ context, search }) => {
    const me = await fetchMe(context.queryClient);
    if (me) throw redirect({ href: search.redirect ?? "/drive" });
  },
  component: LoginPage,
});

/**
 * The product monogram, drawn the same way a space monogram is in the rail
 * (`space-switcher.tsx`), so the first tile somebody sees is the tile they
 * keep seeing.
 */
function BrandMark() {
  return (
    <div className="bg-foreground text-background flex aspect-square size-8 shrink-0 items-center justify-center rounded-lg text-[11px] font-semibold">
      {/* eslint-disable-next-line lingui/no-unlocalized-strings -- product monogram */}
      pp
    </div>
  );
}

function Feature({
  icon: Icon,
  title,
  children,
}: {
  icon: LucideIcon;
  title: React.ReactNode;
  children: React.ReactNode;
}) {
  return (
    <li className="flex gap-3">
      <span className="bg-card ring-foreground/10 flex size-7 shrink-0 items-center justify-center rounded-md ring-1">
        <Icon className="text-muted-foreground size-3.5" />
      </span>
      <span className="grid gap-0.5">
        <span className="text-xs font-medium">{title}</span>
        <span className="text-muted-foreground text-xs/relaxed">{children}</span>
      </span>
    </li>
  );
}

function LoginPage() {
  const { i18n } = useLingui();
  const { redirect: target, error } = Route.useSearch();
  const href = target ? `/auth/login?next=${encodeURIComponent(target)}` : "/auth/login";

  return (
    <main className="bg-background relative flex min-h-svh items-center justify-center overflow-hidden p-6">
      {/* Decoration only: a hairline grid faded out at the edges, and one
          low-opacity wash of the single accent colour behind the card. Both
          are token-driven, so they follow the theme rather than pinning a
          literal white or blue. */}
      <div aria-hidden className="pointer-events-none absolute inset-0 select-none">
        <div className="bg-primary/8 dark:bg-primary/12 absolute top-1/2 left-1/2 size-[44rem] -translate-x-1/2 -translate-y-1/2 rounded-full blur-3xl" />
        <div className="absolute inset-0 [background-image:linear-gradient(to_right,var(--border)_1px,transparent_1px),linear-gradient(to_bottom,var(--border)_1px,transparent_1px)] [background-size:28px_28px] [mask-image:radial-gradient(ellipse_60%_55%_at_50%_50%,#000,transparent_75%)]" />
      </div>

      <div className="animate-in fade-in-0 relative grid w-full max-w-4xl items-center gap-6 duration-200 lg:grid-cols-[1fr_20rem] lg:gap-16">
        <section className="hidden flex-col gap-6 lg:flex">
          <div className="flex items-center gap-2.5">
            <BrandMark />
            <span className="text-sm font-medium tracking-tight">
              {/* eslint-disable-next-line lingui/no-unlocalized-strings -- product name */}
              pp-hub
            </span>
          </div>
          <div className="grid gap-2">
            <h1 className="font-heading max-w-sm text-xl font-semibold tracking-tight">
              <Trans>Your workspace, on your own server.</Trans>
            </h1>
            <p className="text-muted-foreground max-w-sm text-xs/relaxed">
              <Trans>
                Minimal self-hosted workspace: drive, office, spaces. Nothing leaves the machine you
                run it on.
              </Trans>
            </p>
          </div>
          <ul className="grid max-w-sm gap-4">
            <Feature icon={HardDrive} title={<Trans>Drive</Trans>}>
              <Trans>Upload, organise and share files, with every version kept.</Trans>
            </Feature>
            <Feature icon={FileText} title={<Trans>Office</Trans>}>
              <Trans>Edit documents and spreadsheets together in the browser.</Trans>
            </Feature>
            <Feature icon={Users} title={<Trans>Spaces</Trans>}>
              <Trans>Separate teams, each with its own groups and invitations.</Trans>
            </Feature>
          </ul>
        </section>

        <Card className="w-full max-w-sm justify-self-center shadow-sm lg:justify-self-end">
          <CardHeader className="gap-2">
            <div className="lg:hidden">
              <BrandMark />
            </div>
            <CardTitle className="text-base tracking-tight">
              <Trans>Sign in to pp-hub</Trans>
            </CardTitle>
            <CardDescription>
              <Trans>Continue with the identity provider your administrator configured.</Trans>
            </CardDescription>
          </CardHeader>
          <CardContent className="flex flex-col gap-4">
            {error && (
              <Alert variant="destructive">
                <AlertTitle>
                  <Trans>Could not sign you in</Trans>
                </AlertTitle>
                <AlertDescription>
                  {ERRORS[error] ? i18n._(ERRORS[error]) : i18n._(msg`Sign-in failed. Try again.`)}
                </AlertDescription>
              </Alert>
            )}
            {/* Server-side OIDC handshake; no tokens in the SPA. `next` survives
                the provider round trip in its own short-lived cookie, so the
                callback lands back on the page the caller was bounced from
                rather than always on "/". */}
            <Button size="lg" className="w-full" render={<a href={href} />}>
              <LogIn data-icon="inline-start" />
              <Trans>Continue with SSO</Trans>
            </Button>
          </CardContent>
          <CardFooter className="text-muted-foreground gap-2 text-[10px]">
            <Lock className="size-3 shrink-0" />
            <span>
              <Trans>Accounts are provisioned by an administrator.</Trans>
            </span>
          </CardFooter>
        </Card>

        {/* The left panel carries the pitch on wide screens; below `lg` it is
            gone, and the card alone says nothing about what is behind it. */}
        <p className="text-muted-foreground max-w-sm justify-self-center text-center text-xs/relaxed lg:hidden">
          <Trans>Minimal self-hosted workspace: drive, office, spaces.</Trans>
        </p>
      </div>
    </main>
  );
}
