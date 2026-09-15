import { useUiStore } from "@/lib/ui-store";
import { SharePanel } from "@/components/share-panel";
import { VersionsPanel } from "@/components/versions-panel";

/** Renders the currently-open node dialog (zustand-driven). */
export function NodePanels() {
  const panel = useUiStore((s) => s.panel);
  if (!panel) return null;
  switch (panel.kind) {
    case "share":
      return <SharePanel node={panel.node} />;
    case "versions":
      return <VersionsPanel node={panel.node} />;
  }
}
