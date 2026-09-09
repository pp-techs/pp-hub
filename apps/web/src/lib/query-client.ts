import { QueryClient } from "@tanstack/react-query";

// Module-scope so route loaders can prefetch before React mounts.
export const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      refetchOnWindowFocus: false,
      retry: 1,
    },
  },
});
