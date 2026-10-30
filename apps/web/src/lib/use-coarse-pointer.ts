import * as React from "react";

/**
 * Whether the primary pointing device is coarse (a finger) rather than a
 * mouse or trackpad.
 *
 * This is a different question from `useIsMobile`, which is about *width* and
 * drives layout (breadcrumb folding, panel behaviour). This one is about the
 * *input*, and decides how a node opens: a mouse has a double-click, a phone
 * has no such gesture, so a plain tap opens there instead. A landscape tablet
 * is wide enough to be "not mobile" yet has no double-click either, which is
 * why this keys off the pointer rather than the viewport.
 */
export function useCoarsePointer(): boolean {
  const [coarse, setCoarse] = React.useState<boolean>(() =>
    typeof window === "undefined" ? false : window.matchMedia("(pointer: coarse)").matches,
  );

  React.useEffect(() => {
    const mql = window.matchMedia("(pointer: coarse)");
    const onChange = () => setCoarse(mql.matches);
    mql.addEventListener("change", onChange);
    setCoarse(mql.matches);
    return () => mql.removeEventListener("change", onChange);
  }, []);

  return coarse;
}
