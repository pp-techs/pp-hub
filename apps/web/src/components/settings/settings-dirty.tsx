import { createContext, useContext, useEffect } from "react";

/**
 * How a pane tells the settings shell it is holding unsaved edits.
 *
 * `null` outside the dialog, so a pane rendered anywhere else is simply not
 * guarded rather than throwing.
 */
const SettingsDirtyContext = createContext<((dirty: boolean) => void) | null>(null);

export const SettingsDirtyProvider = SettingsDirtyContext.Provider;

/**
 * Reports unsaved edits to the settings shell.
 *
 * A dialog is far easier to dismiss by accident than a page: Escape and a
 * stray click on the backdrop are each one gesture, where leaving a page took
 * a deliberate navigation. So the shell refuses both while a pane is dirty,
 * and asks first when the close button or another pane would throw the edits
 * away.
 */
export function useSettingsDirty(dirty: boolean) {
  const report = useContext(SettingsDirtyContext);

  useEffect(() => {
    if (!report) return;
    report(dirty);
    // Unmounting counts as clean: the pane is gone, so whatever it held can no
    // longer be saved and must not keep blocking the dialog.
    return () => report(false);
  }, [dirty, report]);
}
