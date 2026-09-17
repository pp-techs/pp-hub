import { Outlet, createFileRoute, notFound, useNavigate } from "@tanstack/react-router";
import { Button } from "@workspace/ui/components/button";
import { fetchSpaces } from "@/lib/queries";
import { useUiStore } from "@/lib/ui-store";
import { DriveHeader } from "@/components/drive-header";

/**
 * Everything about one space hangs off this layout: the listing, the feeds,
 * the trash and the settings pages.
 *
 * They are child routes rather than `?panel=` values so that `spaceId` is in
 * the params on every drive surface. The left rail reads it from there, which
 * is why switching to Recent or Starred can no longer empty the rail — there is
 * no state to lose, only a URL segment that is always present.
 */
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

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <DriveHeader spaceId={spaceId} />
      <Outlet />
    </div>
  );
}

function SpaceGone() {
  const navigate = useNavigate();
  return (
    <div className="flex flex-1 flex-col items-center justify-center gap-3 p-8">
      <h1 className="text-sm font-medium">Space not found</h1>
      <p className="text-muted-foreground text-xs">
        It may have been deleted, or you no longer have access.
      </p>
      <Button variant="outline" onClick={() => void navigate({ to: "/drive" })}>
        Back to Drive
      </Button>
    </div>
  );
}
