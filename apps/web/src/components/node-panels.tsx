import { useUiStore } from "@/lib/ui-store";
import { VersionsPanel } from "@/components/versions-panel";

/** Renders the currently-open node dialog (zustand-driven). */
export function NodePanels() {
  const panel = useUiStore((s) => s.panel);
  if (!panel) return null;
  switch (panel.kind) {
    case "versions":
      return <VersionsPanel node={panel.node} />;
  }
}
