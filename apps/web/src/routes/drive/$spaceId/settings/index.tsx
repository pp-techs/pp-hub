import { createFileRoute, redirect } from "@tanstack/react-router";

/** `/settings` is not a page of its own — General is the landing pane. */
export const Route = createFileRoute("/drive/$spaceId/settings/")({
  beforeLoad: ({ params }) => {
    throw redirect({
      to: "/drive/$spaceId/settings/general",
      params: { spaceId: params.spaceId },
      replace: true,
    });
  },
});
