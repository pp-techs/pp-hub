import { cn } from "@workspace/ui/lib/utils";

/**
 * The product brand mark: the full-detail megaminx symbol (`docs/brand/`). The
 * tab and touch icons use the simplified small-size cuts of the same mark.
 */
export function BrandMark({ className }: { className?: string }) {
  return (
    <img
      src="/brand-mark.svg"
      /* eslint-disable-next-line lingui/no-unlocalized-strings -- product brand name */
      alt="PP Hub"
      className={cn("size-8 shrink-0 object-contain", className)}
    />
  );
}
