import { cn } from "@workspace/ui/lib/utils";

/**
 * The product brand mark, inheriting from the favicon / touch icon asset so
 * the icon is consistent across the tab, sign-in page, and shared links.
 */
export function BrandMark({ className }: { className?: string }) {
  return (
    <img
      src="/apple-touch-icon.png"
      /* eslint-disable-next-line lingui/no-unlocalized-strings -- product brand name */
      alt="PP Hub"
      className={cn("size-8 shrink-0 object-contain", className)}
    />
  );
}
