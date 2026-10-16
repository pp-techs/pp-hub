import { StrictMode, Suspense, lazy } from "react";
import { createRoot } from "react-dom/client";
import { RouterProvider, createRouter } from "@tanstack/react-router";
import { installExtHost } from "@pp-hub/ext-sdk";
import "@workspace/ui/globals.css";
import { setUnauthorizedHandler } from "@/lib/api";
import { appExtHost } from "@/ext/host";
import { queryClient } from "@/lib/query-client";
import { bindRouter } from "@/lib/open-file";
import { activateLocale, detectLocale } from "@/lib/i18n";
import { ThemeProvider } from "@/components/theme-provider";
import { routeTree } from "./routeTree.gen";

// Extensions reach the app only through the SDK; this is the one place it is wired.
installExtHost(appExtHost);

const Toaster = lazy(() => import("sonner").then((m) => ({ default: m.Toaster })));

const router = createRouter({
  routeTree,
  defaultPreload: "intent",
  context: { queryClient },
});
// Files open as navigations from menus and toasts that have no router hook.
bindRouter(router);

/**
 * One place where a dead session becomes a sign-in screen.
 *
 * Sessions live in the database, so the server can end one while this tab is
 * still rendering a signed-in shell; the transport reports that as a 401 and
 * this hands it to the router. `/login` is excluded on purpose — its own
 * `fetchMe` probe legitimately 401s for a signed-out visitor, and answering
 * that with a navigation to `/login` is an endless loop. `navigating` keeps a
 * burst of parallel 401s (a surface fires several queries at once) from
 * stacking one navigation per failed request.
 */
let navigating = false;
setUnauthorizedHandler(() => {
  if (navigating || location.pathname === "/login") return;
  navigating = true;
  queryClient.clear();
  void router.navigate({ to: "/login", search: { redirect: location.pathname } }).finally(() => {
    navigating = false;
  });
});

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}

// The catalog is awaited before the first render on purpose. The alternative
// — paint in English, swap when the import resolves — is a visible flash on
// every load for every non-English user, and the catalog is a same-origin,
// hashed, cacheable chunk. `detectLocale()` reads this device's echo of the
// account preference, so the guess is already right for a returning user;
// `LocaleProvider` reconciles it once `/users/me` answers.
await activateLocale(detectLocale());

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <ThemeProvider>
      <RouterProvider router={router} context={{ queryClient }} />
      <Suspense fallback={null}>
        <Toaster position="bottom-right" />
      </Suspense>
    </ThemeProvider>
  </StrictMode>,
);
