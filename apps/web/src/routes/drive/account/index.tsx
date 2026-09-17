import { createFileRoute, redirect } from "@tanstack/react-router";

/** `/drive/account` is not a page of its own — Profile is the landing pane. */
export const Route = createFileRoute("/drive/account/")({
  beforeLoad: () => {
    throw redirect({ to: "/drive/account/profile", replace: true });
  },
});
