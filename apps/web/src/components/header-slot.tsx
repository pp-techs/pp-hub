import { createContext, useContext, useState } from "react";
import { createPortal } from "react-dom";

/**
 * The header lives in the shell (`SidebarInset`) while the controls that
 * belong in it — sort, view, "Browse files" — belong to whichever route is
 * rendered inside `<Outlet/>`. There is no props path between the two, so the
 * shell publishes its trailing slot and a route fills it by portal.
 *
 * Deliberately not `ui-store`: that store is for ephemeral UI *state*, and a
 * ReactNode is not state — keeping it here means no effect writes on render
 * and no stale node when a route unmounts.
 */
const HeaderSlotContext = createContext<{
  node: HTMLDivElement | null;
  setNode: (node: HTMLDivElement | null) => void;
}>({ node: null, setNode: () => {} });

export function HeaderSlotProvider({ children }: { children: React.ReactNode }) {
  const [node, setNode] = useState<HTMLDivElement | null>(null);
  return (
    <HeaderSlotContext.Provider value={{ node, setNode }}>{children}</HeaderSlotContext.Provider>
  );
}

/** Rendered once by the header — the target every route's actions land in. */
export function HeaderSlot() {
  const { setNode } = useContext(HeaderSlotContext);
  return <div ref={setNode} className="flex shrink-0 items-center gap-1" />;
}

/** Rendered by a route: puts its controls in the shell's one header. */
export function HeaderActions({ children }: { children: React.ReactNode }) {
  const { node } = useContext(HeaderSlotContext);
  return node ? createPortal(children, node) : null;
}
