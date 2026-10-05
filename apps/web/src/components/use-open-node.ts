import { useCallback } from "react";
import { useNavigate } from "@tanstack/react-router";
import { opensInEditor, openEditor, type Node } from "@/lib/api";
import { useSpaces } from "@/lib/queries";
import { PreviewModal } from "@/components/preview-modal";

/**
 * What clicking a node in a flat listing does: a folder opens in its space's
 * browser — or in the outside-shares browser when the caller is not a member
 * of that space — an editable file opens in the editor, anything else in the
 * preview with `siblings` to page through.
 */
export function useOpenNode(): (node: Node, siblings: Node[]) => void {
  const navigate = useNavigate();
  const spaces = useSpaces();
  return useCallback(
    (node, siblings) => {
      if (node.kind === "dir") {
        if (spaces.data?.some((s) => s.id === node.space_id)) {
          void navigate({
            to: "/drive/$spaceId",
            params: { spaceId: node.space_id },
            search: { dir: node.id },
          });
        } else {
          void navigate({ to: "/drive/shared", search: { dir: node.id } });
        }
      } else if (opensInEditor(node)) {
        openEditor(node);
      } else {
        void PreviewModal.show({ node, siblings });
      }
    },
    [navigate, spaces.data],
  );
}
