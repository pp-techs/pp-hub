import { Skeleton } from "@workspace/ui/components/skeleton";
import { cn } from "@workspace/ui/lib/utils";

/**
 * A label over an input — or over a textarea, with `multiline` — laid out as
 * the settings forms lay one out, so a pane's fields land where these sat.
 * Wrap a pane's worth of them in `LoadingStatus`.
 */
export function FieldSkeleton({
  multiline = false,
  className,
}: {
  multiline?: boolean;
  className?: string;
}) {
  return (
    <div className={cn("flex flex-col gap-1", className)}>
      <Skeleton className="h-3 w-24" />
      <Skeleton className={multiline ? "h-16 w-full" : "h-7 w-full"} />
    </div>
  );
}
