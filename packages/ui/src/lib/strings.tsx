"use client";

import * as React from "react";

/**
 * The handful of strings the primitives render themselves.
 *
 * These are almost all screen-reader labels on controls whose visible content
 * is an icon, so they are invisible until they are the only thing a reader
 * has — which is exactly why they cannot be left in English in a localized
 * app.
 *
 * They are injected rather than translated here on purpose: a design-system
 * package must not depend on an application's i18n runtime. It has no idea
 * which library the consumer uses, and importing one would make this package
 * unusable by any app that chose differently. The defaults below are English,
 * so a consumer that never provides a value keeps today's behaviour exactly.
 */
export interface UiStrings {
  close: string;
  more: string;
  toggleSidebar: string;
  /** Accessible name of the mobile sidebar sheet. */
  sidebar: string;
  sidebarDescription: string;
  /** Accessible name of the breadcrumb landmark. */
  breadcrumb: string;
}

const DEFAULTS: UiStrings = {
  close: "Close",
  more: "More",
  toggleSidebar: "Toggle Sidebar",
  sidebar: "Sidebar",
  sidebarDescription: "Displays the mobile sidebar.",
  breadcrumb: "breadcrumb",
};

const UiStringsContext = React.createContext<UiStrings>(DEFAULTS);

/**
 * Supply translated primitive strings. Partial: anything omitted keeps its
 * English default, so a consumer can translate one label without restating
 * the rest.
 */
export function UiStringsProvider({
  strings,
  children,
}: {
  strings: Partial<UiStrings>;
  children: React.ReactNode;
}) {
  const value = React.useMemo(() => ({ ...DEFAULTS, ...strings }), [strings]);
  return <UiStringsContext value={value}>{children}</UiStringsContext>;
}

export function useUiStrings(): UiStrings {
  return React.use(UiStringsContext);
}
