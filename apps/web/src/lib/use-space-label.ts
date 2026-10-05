import { useCallback } from "react";
import { useLingui } from "@lingui/react/macro";
import type { Space } from "@/lib/api";

/**
 * How a space is named on screen. A personal space's stored name is its
 * owner's; the owner sees it as "My Drive", in their language.
 */
export function useSpaceLabel(): (space: Pick<Space, "kind" | "name">) => string {
  const { t } = useLingui();
  return useCallback((space) => (space.kind === "personal" ? t`My Drive` : space.name), [t]);
}
