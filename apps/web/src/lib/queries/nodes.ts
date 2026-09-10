// Node (file/folder) and version hooks.

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  createFolder,
  deleteNode,
  getChildren,
  getVersions,
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
