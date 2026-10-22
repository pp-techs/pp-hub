// The app's one connection to pp-hub: `connect()` once, switch Lingui to the
// host's language, then render the app with the connected `App` in context.
// (Kept identical in app-kanban and app-checklist; the apps share no workspace
// package.)

import { createContext, use, useEffect, useMemo, useState, type ReactNode } from "react";
import { i18n, type Messages } from "@lingui/core";
import { I18nProvider } from "@lingui/react";
import { Trans, useLingui } from "@lingui/react/macro";
import { CircleAlert, LoaderCircle, PlugZap, PowerOff } from "lucide-react";
import { connect, type App } from "@pp-hub/app-sdk";
import { Button } from "@workspace/ui/components/button";
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@workspace/ui/components/empty";
import { UiStringsProvider } from "@workspace/ui/lib/strings";

/** The languages the app ships catalogs for. */
export type Locale = "en" | "vi";

/** A BCP 47 tag from the host narrowed to a shipped locale: base `vi` is Vietnamese, anything else English. */
export function localeOf(tag: string): Locale {
  // eslint-disable-next-line lingui/no-unlocalized-strings -- locale tags
  return tag.toLowerCase().split("-")[0] === "vi" ? "vi" : "en";
}

const AppContext = createContext<App | null>(null);

/** The connected app; only inside `AppProvider`. */
export function useApp(): App {
  const app = use(AppContext);
  // eslint-disable-next-line lingui/no-unlocalized-strings -- developer error
  if (!app) throw new Error("useApp is used outside AppProvider");
  return app;
}

/** The strings `@workspace/ui` primitives render themselves, translated. */
function UiStrings({ children }: { children: ReactNode }) {
  const { t } = useLingui();
  const strings = useMemo(() => ({ close: t`Close`, more: t`More` }), [t]);
  return <UiStringsProvider strings={strings}>{children}</UiStringsProvider>;
}

/** A centred spinner for while the app connects or loads its records. */
export function Spinner() {
  return (
    <div role="status" className="text-muted-foreground flex h-full items-center justify-center">
      <LoaderCircle className="size-5 animate-spin" aria-hidden />
      <span className="sr-only">
        <Trans>Loading…</Trans>
      </span>
    </div>
  );
}

/**
 * The records could not be loaded. `refused` is a 403: the app is switched off
 * for the space, or the viewer lost access. `what` is the node's own name for
 * itself, translated and lower-case ("board", "checklist").
 */
export function Unavailable({
  what,
  refused,
  onRetry,
}: {
  what: string;
  refused: boolean;
  onRetry: () => void;
}) {
  return (
    <Empty className="h-full">
      <EmptyHeader>
        <EmptyMedia variant="icon">{refused ? <PowerOff /> : <CircleAlert />}</EmptyMedia>
        <EmptyTitle>
          {refused ? (
            <Trans>This app is turned off</Trans>
          ) : (
            <Trans>This {what} could not be opened</Trans>
          )}
        </EmptyTitle>
        <EmptyDescription>
          {refused ? (
            <Trans>
              This app is switched off for this space. Your {what} is kept; an admin can turn the
              app back on.
            </Trans>
          ) : (
            <Trans>It may have been deleted, or you may no longer have access to it.</Trans>
          )}
        </EmptyDescription>
      </EmptyHeader>
      <Button variant="outline" onClick={onRetry}>
        <Trans>Try again</Trans>
      </Button>
    </Empty>
  );
}

function NotConnected() {
  return (
    <div className="text-muted-foreground flex h-full flex-col items-center justify-center gap-2 p-6 text-center text-xs">
      <PlugZap className="size-5" aria-hidden />
      <p className="text-foreground text-sm font-medium">
        <Trans>This app could not reach pp-hub</Trans>
      </p>
      <p>
        <Trans>It only works when opened from pp-hub. Reload the page to try again.</Trans>
      </p>
    </div>
  );
}

/**
 * Connects to the host and renders `children` once connected, in the host's
 * language. `catalogs` are the compiled `messages.po` of each locale; English
 * must already be active (main.tsx) so the loading and failure states read.
 */
export function AppProvider({
  catalogs,
  children,
}: {
  catalogs: Record<Locale, Messages>;
  children: ReactNode;
}) {
  const [state, setState] = useState<{ app?: App; failed?: boolean }>({});

  useEffect(() => {
    let live = true;
    connect({ applyTheme: true }).then(
      (app) => {
        const locale = localeOf(app.context.locale);
        i18n.loadAndActivate({ locale, messages: catalogs[locale] });
        document.documentElement.lang = locale;
        if (live) setState({ app });
      },
      () => live && setState({ failed: true }),
    );
    return () => {
      live = false;
    };
  }, [catalogs]);

  return (
    <I18nProvider i18n={i18n}>
      <UiStrings>
        {state.app ? (
          <AppContext value={state.app}>{children}</AppContext>
        ) : state.failed ? (
          <NotConnected />
        ) : (
          <Spinner />
        )}
      </UiStrings>
    </I18nProvider>
  );
}
