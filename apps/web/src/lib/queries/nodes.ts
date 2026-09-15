// Node (file/folder) and version hooks.

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  createFolder,
  deleteNode,
  getChildren,
  getVersions,
  getTree,
  getTrash,
  getShared,
  renameNode,
  restoreVersion,
  uploadFile,
} from "@/lib/api";
import { keys } from "./keys";

/** Query options factory for children listing. */
export function childrenOptions(spaceId: string, parentId?: string) {
  return {
    queryKey: keys.children(spaceId, parentId),
    queryFn: () => getChildren(spaceId, parentId),
  } as const;
}

export function useChildren(spaceId: string, parentId?: string) {
  return useQuery(childrenOptions(spaceId, parentId));
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
  return useMutation({
    mutationFn: (nodeId: string) => deleteNode(nodeId),
    onSuccess: invalidate,
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

export function treeOptions(spaceId: string | undefined, parentId?: string) {
  return {
    queryKey: keys.tree(spaceId ?? "", parentId),
    queryFn: () => getTree(spaceId as string, parentId),
    // The rail mounts above the space route, so it renders for a beat with no
    // space selected. Without this the tree would request `/spaces/undefined`.
    enabled: !!spaceId,
  } as const;
}

export function useTree(spaceId: string | undefined, parentId?: string) {
  return useQuery(treeOptions(spaceId, parentId));
}

export function useTrash(spaceId: string) {
  return useQuery({
    queryKey: ["trash", spaceId] as const,
    queryFn: () => getTrash(spaceId),
  });
}

export function useShared() {
  return useQuery({
    queryKey: ["shared"] as const,
    queryFn: getShared,
  });
}
