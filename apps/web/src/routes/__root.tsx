import { Outlet, createRootRouteWithContext, Link } from "@tanstack/react-router";
import { QueryClientProvider } from "@tanstack/react-query";
import { Trans } from "@lingui/react/macro";
import type { QueryClient } from "@tanstack/react-query";
import { Button } from "@workspace/ui/components/button";
import { ModalContainer } from "@buiducnhat/better-modal";
import { LocaleProvider } from "@/components/locale-provider";

interface RouterContext {
  queryClient: QueryClient;
}

export const Route = createRootRouteWithContext<RouterContext>()({
  component: RootComponent,
  notFoundComponent: NotFound,
});

function RootComponent() {
  const { queryClient } = Route.useRouteContext();

  return (
    <QueryClientProvider client={queryClient}>
      <LocaleProvider>
        <Outlet />
        <ModalContainer />
      </LocaleProvider>
    </QueryClientProvider>
  );
}

function NotFound() {
  return (
    <main className="flex min-h-svh flex-col items-center justify-center gap-4 p-6">
      <p className="text-muted-foreground text-sm">
        <Trans>Page not found.</Trans>
      </p>
      <Link to="/drive">
        <Button>
          <Trans>Go to Drive</Trans>
        </Button>
      </Link>
    </main>
  );
}
