import { Outlet, createFileRoute, notFound, useNavigate } from "@tanstack/react-router";
import { Trans } from "@lingui/react/macro";
import { Button } from "@workspace/ui/components/button";
import { fetchSpaces, useSpace } from "@/lib/queries";
import { useUiStore } from "@/lib/ui-store";
import { NoAdminBanner } from "@/components/no-admin-banner";

export const Route = createFileRoute("/drive/$spaceId")({
  beforeLoad: async ({ context, params }) => {
    const spaces = await fetchSpaces(context.queryClient);
    const space = spaces.find((s) => s.id === params.spaceId);
    // A deleted space, or one the caller was removed from, must not stay the
    // space `/drive` redirects to.
    if (!space) {
      useUiStore.getState().clearLastSpaceId(params.spaceId);
      throw notFound();
    }
    useUiStore.getState().setLastSpaceId(space.id);
  },
  component: SpaceLayout,
  notFoundComponent: SpaceGone,
});

function SpaceLayout() {
  const { spaceId } = Route.useParams();
  const space = useSpace(spaceId);

  // The shell above owns the header; this layout resolves the space in
  // `beforeLoad` and holds its surfaces — plus the one notice that belongs to
  // the space itself rather than to any surface inside it, which is why it
  // hangs here and not on the settings page a non-admin cannot open.
  return (
    <div className="flex min-h-0 flex-1 flex-col">
      {space?.admin_count === 0 && <NoAdminBanner />}
      <Outlet />
    </div>
  );
}

function SpaceGone() {
  const navigate = useNavigate();
  return (
    <div className="flex flex-1 flex-col items-center justify-center gap-3 p-8">
      <h1 className="text-sm font-medium">
        <Trans>Space not found</Trans>
      </h1>
      <p className="text-muted-foreground text-xs">
        <Trans>It may have been deleted, or you no longer have access.</Trans>
      </p>
      <Button variant="outline" onClick={() => void navigate({ to: "/drive" })}>
        <Trans>Back to Drive</Trans>
      </Button>
    </div>
  );
}
