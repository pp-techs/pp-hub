import { createContext, useContext, useState } from "react";
import { createPortal } from "react-dom";

/**
 * The header lives in the shell (`SidebarInset`) while the controls that
 * belong in it — sort, view, "Browse files" — belong to whichever route is
 * rendered inside `<Outlet/>`. There is no props path between the two, so the
 * shell publishes its slots and a route fills them by portal: the trailing
 * actions, and — on a file, whose breadcrumb only its editor can draw (a
 * sub-page's trail) — the lead, in place of the breadcrumb the shell derives
 * from the pathname.
 *
 * The header is navigation only. What belongs to an open file — its version
 * line, presence, the outline and comments toggles, Share, Versions,
 * Download — goes to the document bar under it (`DocActions`), beside the
 * open-file tabs.
 *
 * Deliberately not `ui-store`: that store is for ephemeral UI *state*, and a
 * ReactNode is not state — keeping it here means no effect writes on render
 * and no stale node when a route unmounts.
 */
type Slot = HTMLDivElement | null;
const HeaderSlotContext = createContext<{
  actions: Slot;
  setActions: (node: Slot) => void;
  lead: Slot;
  setLead: (node: Slot) => void;
  doc: Slot;
  setDoc: (node: Slot) => void;
}>({
  actions: null,
  setActions: () => {},
  lead: null,
  setLead: () => {},
  doc: null,
  setDoc: () => {},
});

/**
 * Whether the surface below is the one on screen. Open files stay mounted
 * out of sight behind the one shown, and only the shown one may fill the
 * header or name the browser tab.
 */
const OnScreenContext = createContext(true);

export function HeaderSlotProvider({ children }: { children: React.ReactNode }) {
  const [actions, setActions] = useState<Slot>(null);
  const [lead, setLead] = useState<Slot>(null);
  const [doc, setDoc] = useState<Slot>(null);
  return (
    <HeaderSlotContext.Provider value={{ actions, setActions, lead, setLead, doc, setDoc }}>
      {children}
    </HeaderSlotContext.Provider>
  );
}

/** Rendered once by the header — the target every route's actions land in. */
export function HeaderSlot() {
  const { setActions } = useContext(HeaderSlotContext);
  return <div ref={setActions} className="flex shrink-0 items-center gap-1" />;
}

/** Rendered by the header in place of its breadcrumb while a file is open. */
export function HeaderLeadSlot() {
  const { setLead } = useContext(HeaderSlotContext);
  return <div ref={setLead} className="flex min-w-0 flex-1 items-center gap-2" />;
}

/** Rendered once by the document bar — where an open file's own controls land. */
export function DocActionsSlot() {
  const { setDoc } = useContext(HeaderSlotContext);
  return <div ref={setDoc} className="flex shrink-0 items-center gap-1" />;
}

/** Rendered by a route: puts its controls in the shell's one header. */
export function HeaderActions({ children }: { children: React.ReactNode }) {
  const { actions } = useContext(HeaderSlotContext);
  const onScreen = useContext(OnScreenContext);
  return actions && onScreen ? createPortal(children, actions) : null;
}

/** Rendered by a file's editor: where it is, at the head of the header. */
export function HeaderLead({ children }: { children: React.ReactNode }) {
  const { lead } = useContext(HeaderSlotContext);
  const onScreen = useContext(OnScreenContext);
  return lead && onScreen ? createPortal(children, lead) : null;
}

/** Rendered by a file's editor: its own controls, in the document bar. */
export function DocActions({ children }: { children: React.ReactNode }) {
  const { doc } = useContext(HeaderSlotContext);
  const onScreen = useContext(OnScreenContext);
  return doc && onScreen ? createPortal(children, doc) : null;
}

/** Marks a subtree as on screen or not; nested markers can only narrow it. */
export function OnScreen({ value, children }: { value: boolean; children: React.ReactNode }) {
  const outer = useContext(OnScreenContext);
  return <OnScreenContext.Provider value={outer && value}>{children}</OnScreenContext.Provider>;
}

export function useOnScreen(): boolean {
  return useContext(OnScreenContext);
}
