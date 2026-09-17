// Node (file/folder) and version hooks.

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  copyNode,
  createFolder,
  deleteNode,
  emptyTrash,
  getChildren,
  getNode,
  getVersions,
  getTree,
  getTrash,
  getShared,
  moveNode,
  purgeNode,
  renameNode,
  restoreNode,
  restoreVersion,
  uploadFile,
} from "@/lib/api";
import type { Node } from "@/lib/api";
import { keys } from "./keys";

/** Query options factory for children listing. */
export function childrenOptions(spaceId: string, parentId?: string) {
  return {
    queryKey: keys.children(spaceId, parentId),
    queryFn: () => getChildren(spaceId, parentId),
    // Keeps the previous folder's rows on screen while the next folder loads,
    // so clicking a folder in the sidebar swaps content in place instead of
    // blanking to "Loading…" and back — the visible stutter this exists to
    // remove. Same pattern as `useNodeSearch`.
    placeholderData: (prev: Node[] | undefined) => prev,
  } as const;
}

export function useChildren(spaceId: string, parentId?: string) {
  return useQuery(childrenOptions(spaceId, parentId));
}

/** A single node, for surfaces routed by node id alone (the editor page). */
export function useNode(nodeId: string | undefined) {
  return useQuery({
    queryKey: keys.node(nodeId ?? ""),
    queryFn: () => getNode(nodeId as string),
    enabled: !!nodeId,
  });
}

/** Invalidates the file listing for a space (root + every open folder). */
export function useInvalidateChildren(spaceId: string) {
  const qc = useQueryClient();
  return () => qc.invalidateQueries({ queryKey: ["children", spaceId] });
}

export function useCreateFolder(spaceId: string, parentId?: string) {
  const invalidate = useInvalidateChildren(spaceId);
  return useMutation({
    mutationFn: (name: string) => createFolder(spaceId, name, parentId),
    onSuccess: invalidate,
  });
}

export function useUpload(spaceId: string, parentId?: string) {
  const invalidate = useInvalidateChildren(spaceId);
  return useMutation({
    mutationFn: (file: File) => uploadFile(spaceId, file, parentId),
    onSuccess: invalidate,
  });
}

export function useRenameNode(spaceId: string) {
  const invalidate = useInvalidateChildren(spaceId);
  return useMutation({
    mutationFn: ({ nodeId, name }: { nodeId: string; name: string }) => renameNode(nodeId, name),
    onSuccess: invalidate,
  });
}

export function useDeleteNode(spaceId: string) {
  const invalidate = useInvalidateChildren(spaceId);
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (nodeId: string) => deleteNode(nodeId),
    onSuccess: () => {
      void invalidate();
      // The node just became a trash row.
      void qc.invalidateQueries({ queryKey: keys.trash(spaceId) });
    },
  });
}

export function useCopyNode(spaceId: string) {
  const invalidate = useInvalidateChildren(spaceId);
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ nodeId, parentId }: { nodeId: string; parentId?: string }) =>
      copyNode(nodeId, parentId),
    onSuccess: () => {
      void invalidate();
      void qc.invalidateQueries({ queryKey: ["tree", spaceId] });
      // A copy adds references, so the space's usage moved.
      void qc.invalidateQueries({ queryKey: keys.spaces });
    },
  });
}

export function useMoveNode(spaceId: string) {
  const invalidate = useInvalidateChildren(spaceId);
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ nodeId, parentId }: { nodeId: string; parentId?: string }) =>
      moveNode(nodeId, parentId),
    onSuccess: () => {
      void invalidate();
      // Both the source and destination folders changed shape.
      void qc.invalidateQueries({ queryKey: ["tree", spaceId] });
    },
  });
}

// ─── Versions ────────────────────────────────────────────────────────────────

export function versionsOptions(nodeId: string | null) {
  return {
    queryKey: keys.versions(nodeId ?? ""),
    queryFn: () => getVersions(nodeId!),
    enabled: nodeId !== null,
  } as const;
}

export function useVersions(nodeId: string | null) {
  return useQuery(versionsOptions(nodeId));
}

export function useRestoreVersion(spaceId: string) {
  const invalidate = useInvalidateChildren(spaceId);
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ nodeId, versionId }: { nodeId: string; versionId: string }) =>
      restoreVersion(nodeId, versionId),
    onSuccess: (_d, { nodeId }) => {
      void invalidate();
      void qc.invalidateQueries({ queryKey: keys.versions(nodeId) });
    },
  });
}

// ─── Tree / Trash / Shared (Sidebar) ─────────────────────────────────────────

export function treeOptions(spaceId: string | undefined, parentId?: string, enabled = true) {
  return {
    queryKey: keys.tree(spaceId ?? "", parentId),
    queryFn: () => getTree(spaceId as string, parentId),
    // The rail mounts above the space route, so it renders for a beat with no
    // space selected. Without this the tree would request `/spaces/undefined`.
    enabled: !!spaceId && enabled,
  } as const;
}

export function useTree(spaceId: string | undefined, parentId?: string, enabled = true) {
  return useQuery(treeOptions(spaceId, parentId, enabled));
}

export function useTrash(spaceId: string) {
  return useQuery({
    queryKey: keys.trash(spaceId),
    queryFn: () => getTrash(spaceId),
  });
}

export function useShared(spaceId: string) {
  return useQuery({
    queryKey: keys.shared(spaceId),
    queryFn: () => getShared(spaceId),
  });
}

/**
 * Lifting a node out of the trash. The listing, the tree and the trash panel
 * all change, so all three are invalidated; the result says whether the node
 * had to be relocated or renamed on the way back, which the caller reports.
 */
export function useRestoreNode(spaceId: string) {
  const invalidate = useInvalidateChildren(spaceId);
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (nodeId: string) => restoreNode(nodeId),
    onSuccess: () => {
      void invalidate();
      void qc.invalidateQueries({ queryKey: keys.trash(spaceId) });
      void qc.invalidateQueries({ queryKey: ["tree", spaceId] });
    },
  });
}

export function usePurgeNode(spaceId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (nodeId: string) => purgeNode(nodeId),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: keys.trash(spaceId) });
      // Storage is released, so the rail meter is stale.
      void qc.invalidateQueries({ queryKey: keys.spaces });
    },
  });
}

export function useEmptyTrash(spaceId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => emptyTrash(spaceId),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: keys.trash(spaceId) });
      void qc.invalidateQueries({ queryKey: keys.spaces });
    },
  });
}
