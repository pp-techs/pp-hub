import { createFileRoute, redirect } from "@tanstack/react-router";
import { fetchMe } from "@/lib/auth";

export const Route = createFileRoute("/")({
  beforeLoad: async ({ context }) => {
    const me = await fetchMe(context.queryClient);
    throw redirect({ to: me ? "/drive" : "/login" });
  },
});
