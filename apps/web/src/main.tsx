import { StrictMode, Suspense, lazy } from "react";
import { createRoot } from "react-dom/client";
import { RouterProvider, createRouter } from "@tanstack/react-router";
import "@workspace/ui/globals.css";
import { queryClient } from "@/lib/query-client";
import { ThemeProvider } from "@/components/theme-provider";
import { routeTree } from "./routeTree.gen";

const Toaster = lazy(() => import("sonner").then((m) => ({ default: m.Toaster })));

const router = createRouter({
  routeTree,
  defaultPreload: "intent",
  context: { queryClient },
});

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}

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
