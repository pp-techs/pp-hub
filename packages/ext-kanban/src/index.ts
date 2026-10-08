import { lazy } from "react";
import { msg } from "@lingui/core/macro";
import { KanbanSquare } from "lucide-react";
import { defineExtension } from "@pp-hub/ext-sdk";
import manifest from "../pp-app.json";

// The id and the kind strings are the manifest's, the one place they are written.
export default defineExtension({
  id: manifest.id,
  name: msg`Kanban`,
  notifications: {
    assigned: ({ actor, excerpt }) => msg`${actor} assigned you to “${excerpt}”`,
  },
  nodeKinds: [
    {
      kind: manifest.nodeKinds[0].kind,
      label: msg`Kanban board`,
      icon: KanbanSquare,
      newLabel: msg`New kanban board`,
      defaultName: msg`Untitled board`,
      newHint: msg`Columns To do, In progress, and Done are added for you.`,
      editor: lazy(() => import("./board-editor")),
    },
  ],
});
