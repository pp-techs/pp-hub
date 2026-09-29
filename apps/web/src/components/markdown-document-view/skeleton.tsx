import { Skeleton } from "@workspace/ui/components/skeleton";
import { cn } from "@workspace/ui/lib/utils";
import { LoadingStatus } from "@/components/loading";

/** Column the document is laid out in, shared by the view and its skeleton. */
// eslint-disable-next-line lingui/no-unlocalized-strings -- CSS classes
export const DOCUMENT_COLUMN = "mx-auto w-full max-w-3xl";

/** Stand-in for a document while it loads: a title and paragraph lines in the same column. */
export function MarkdownDocumentSkeleton({ className }: { className?: string }) {
  return (
    <LoadingStatus className={cn("flex flex-col gap-3", DOCUMENT_COLUMN, className)}>
      <Skeleton className="h-7 w-1/2" />
      <Skeleton className="h-4 w-full" />
      <Skeleton className="h-4 w-11/12" />
      <Skeleton className="h-4 w-4/5" />
      <Skeleton className="h-4 w-2/3" />
    </LoadingStatus>
  );
}
