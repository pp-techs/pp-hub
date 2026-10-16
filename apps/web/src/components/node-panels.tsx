import { useEffect, useRef } from "react";
import { useLocation } from "@tanstack/react-router";
import { useUiStore } from "@/lib/ui-store";
import { VersionsPanel } from "@/components/versions-panel";

/**
 * Renders the currently-open node dialog (zustand-driven). Mounted once by the
 * drive shell, so every surface's Versions action — the listing's, Recent's,
 * an open file's — has it. A panel is about the page it was opened on:
 * following a link out of it (a page link in a version's preview) closes it.
 */
export function NodePanels() {
  const panel = useUiStore((s) => s.panel);
  const closePanel = useUiStore((s) => s.closePanel);
  const pathname = useLocation({ select: (l) => l.pathname });
  const seen = useRef(pathname);
  useEffect(() => {
    if (seen.current === pathname) return;
    seen.current = pathname;
    closePanel();
  }, [pathname, closePanel]);

  if (!panel) return null;
  switch (panel.kind) {
    case "versions":
      return <VersionsPanel node={panel.node} />;
  }
}
