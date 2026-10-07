import { createFileRoute, redirect } from "@tanstack/react-router";

// `/admin` has no page of its own; Users is the front door.
export const Route = createFileRoute("/admin/")({
  beforeLoad: () => {
    throw redirect({ to: "/admin/users", replace: true });
  },
});
