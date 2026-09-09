import { useUiStore } from "@/lib/ui-store";
import { SharePanel } from "@/components/share-panel";
import { VersionsPanel } from "@/components/versions-panel";
import { PreviewPanel } from "@/components/preview-panel";
import { EditPanel } from "@/components/edit-panel";

/** Renders the currently-open node dialog (zustand-driven). */
export function NodePanels() {
  const panel = useUiStore((s) => s.panel);
  if (!panel) return null;
  switch (panel.kind) {
    case "share":
      return <SharePanel node={panel.node} />;
    case "versions":
      return <VersionsPanel node={panel.node} />;
    case "preview":
      return <PreviewPanel node={panel.node} />;
    case "edit":
      return <EditPanel node={panel.node} />;
  }
}
