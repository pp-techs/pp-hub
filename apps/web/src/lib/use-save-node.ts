import { useEffect, useRef } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { useLingui } from "@lingui/react/macro";
import { saveNode } from "@/lib/api";
import { keys } from "@/lib/queries";

/** A save OnlyOffice queued becomes a version a moment later; the node is asked again after it. */
const VERSION_SETTLE_MS = 3_000;

/**
 * Asks the editor holding a file to save it now, and says what it found.
 *
 * The editors save on their own — OnlyOffice every few minutes, a block
 * document when its room goes quiet — so this is for the person who wants to
 * know it is done, and for the moment before leaving.
 */
export function useSaveNode(nodeId: string) {
  const { t } = useLingui();
  const qc = useQueryClient();
  const timer = useRef<ReturnType<typeof setTimeout>>(undefined);
  useEffect(() => () => clearTimeout(timer.current), []);

  return useMutation({
    mutationFn: () => saveNode(nodeId),
    onSuccess: (state) => {
      if (state === "unchanged") toast(t`Already saved`);
      else toast.success(state === "queued" ? t`Saving…` : t`Saved`);
      const refresh = () => {
        void qc.invalidateQueries({ queryKey: keys.node(nodeId) });
        void qc.invalidateQueries({ queryKey: keys.versions(nodeId) });
      };
      refresh();
      if (state === "queued") {
        clearTimeout(timer.current);
        timer.current = setTimeout(refresh, VERSION_SETTLE_MS);
      }
    },
    onError: (error) => toast.error(t`Could not save`, { description: error.message }),
  });
}

/**
 * Cmd+S (Ctrl+S) saves the open file instead of the browser's "save page as".
 *
 * Listened for on the window, so it works from the header and from inside the
 * block editor. It does not reach OnlyOffice's own iframe: a key pressed
 * while that frame has focus goes to the Document Server, which handles it
 * itself, and a cross-origin page cannot see it.
 */
export function useSaveShortcut(nodeId: string, enabled: boolean) {
  const { mutate, isPending } = useSaveNode(nodeId);
  const busy = useRef(false);
  busy.current = isPending;

  useEffect(() => {
    if (!enabled) return;
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key.toLowerCase() !== "s" || event.altKey || event.shiftKey) return;
      if (!(event.metaKey || event.ctrlKey)) return;
      event.preventDefault();
      if (!busy.current && !event.repeat) mutate();
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [enabled, mutate]);
}
