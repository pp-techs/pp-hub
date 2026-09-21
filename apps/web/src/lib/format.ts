import * as React from "react";
import { fmtDate, fmtRelative, fmtSize, sortNodes } from "@/lib/api";
import { useLocale } from "@/components/locale-provider";
import type { Node, SortField, SortOrder } from "@/lib/api";

/**
 * The formatters in `lib/api/utils.ts` take the locale as an argument rather
 * than reading it from a module-level singleton, so that the transport layer
 * never imports the i18n runtime — the dependency only ever points one way.
 *
 * This hook is the other half: it binds the active locale and, because it
 * subscribes through `useLocale()`, it also re-renders the calling component
 * when the language changes. A component that formatted a date by calling
 * `fmtDate(iso)` directly would keep showing the old locale's date until
 * something else happened to re-render it.
 */
export function useFormatters() {
  const locale = useLocale();

  return React.useMemo(
    () => ({
      date: (iso: string | undefined) => fmtDate(iso, locale),
      relative: (iso: string | undefined) => fmtRelative(iso, locale),
      size: (n: number) => fmtSize(n, locale),
      sort: (nodes: Node[], field: SortField, order: SortOrder) =>
        sortNodes(nodes, field, order, locale),
    }),
    [locale],
  );
}
