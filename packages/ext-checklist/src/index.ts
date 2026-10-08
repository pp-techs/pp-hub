import { lazy } from "react";
import { msg } from "@lingui/core/macro";
import { ListChecks } from "lucide-react";
import { defineExtension } from "@pp-hub/ext-sdk";
import manifest from "../pp-app.json";

// The id and the kind strings are the manifest's, the one place they are written.
export default defineExtension({
  id: manifest.id,
  name: msg`Checklist`,
  nodeKinds: [
    {
      kind: manifest.nodeKinds[0].kind,
      label: msg`Checklist`,
      icon: ListChecks,
      newLabel: msg`New checklist`,
      defaultName: msg`Untitled checklist`,
      editor: lazy(() => import("./list-editor")),
    },
  ],
});
