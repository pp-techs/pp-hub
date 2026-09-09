import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  api,
  patchJson,
  postJson,
  type EditSession,
  type Me,
  type Node,
  type Share,
  type ShareInfo,
  type Space,
  type Version,
} from "@/lib/api";

export const keys = {
  me: ["me"] as const,
  spaces: ["spaces"] as const,
  children: (spaceId: string, parentId?: string) =>
    ["children", spaceId, parentId ?? null] as const,
  versions: (nodeId: string) => ["versions", nodeId] as const,
  shares: (nodeId: string) => ["shares", nodeId] as const,
  shareInfo: (token: string) => ["share", token] as const,
};

export function meOptions() {
  return useQuery({
    queryKey: keys.me,
    queryFn: () => api<Me>("/api/v1/me"),
    retry: false,
    staleTime: Infinity,
  });
}

export function spacesOptions() {
  return useQuery({
    queryKey: keys.spaces,
    queryFn: () => api<Space[]>("/api/v1/spaces"),
  });
}

export function childrenOptions(spaceId: string, parentId?: string) {
  return useQuery({
    queryKey: keys.children(spaceId, parentId),
    queryFn: () =>
      api<Node[]>(`/api/v1/spaces/${spaceId}/children${parentId ? `?parent=${parentId}` : ""}`),
  });
}

export function versionsOptions(nodeId: string | null) {
  return useQuery({
    queryKey: keys.versions(nodeId ?? ""),
    queryFn: () => api<Version[]>(`/api/v1/nodes/${nodeId}/versions`),
    enabled: nodeId !== null,
  });
}

export function sharesOptions(nodeId: string | null) {
  return useQuery({
    queryKey: keys.shares(nodeId ?? ""),
    queryFn: () => api<Share[]>(`/api/v1/nodes/${nodeId}/shares`),
    enabled: nodeId !== null,
  });
}

export function shareInfoOptions(token: string) {
  return useQuery({
    queryKey: keys.shareInfo(token),
    queryFn: () => api<ShareInfo>(`/s/${token}`),
    retry: false,
  });
}

/** Invalidates the file listing for a space (root + every open folder). */
export function useInvalidateChildren(spaceId: string) {
  const qc = useQueryClient();
  return () => void qc.invalidateQueries({ queryKey: ["children", spaceId] });
}

export function useCreateSpace() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (name: string) => api<Space>("/api/v1/spaces", postJson({ name })),
    onSuccess: () => void qc.invalidateQueries({ queryKey: keys.spaces }),
  });
}

export function useCreateFolder(spaceId: string, parentId?: string) {
  const invalidate = useInvalidateChildren(spaceId);
  return useMutation({
    mutationFn: (name: string) =>
      api<Node>(`/api/v1/spaces/${spaceId}/dirs`, postJson({ name, parent_id: parentId })),
    onSuccess: invalidate,
  });
}

export function useUpload(spaceId: string, parentId?: string) {
  const invalidate = useInvalidateChildren(spaceId);
  return useMutation({
    mutationFn: (file: File) => {
      const fd = new FormData();
      if (parentId) fd.append("parent_id", parentId);
      fd.append("file", file);
      return api<Node>(`/api/v1/spaces/${spaceId}/files`, { method: "POST", body: fd });
    },
    onSuccess: invalidate,
  });
}

export function useRenameNode(spaceId: string) {
  const invalidate = useInvalidateChildren(spaceId);
  return useMutation({
    mutationFn: ({ nodeId, name }: { nodeId: string; name: string }) =>
      api<Node>(`/api/v1/nodes/${nodeId}/rename`, patchJson({ name })),
    onSuccess: invalidate,
  });
}

export function useDeleteNode(spaceId: string) {
  const invalidate = useInvalidateChildren(spaceId);
  return useMutation({
    mutationFn: (nodeId: string) =>
      fetch(`/api/v1/nodes/${nodeId}`, { method: "DELETE" }).then((r) => {
        if (!r.ok) throw new Error(`${r.status}`);
      }),
    onSuccess: invalidate,
  });
}

export function useRestoreVersion(spaceId: string) {
  const invalidate = useInvalidateChildren(spaceId);
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ nodeId, versionId }: { nodeId: string; versionId: string }) =>
      api(`/api/v1/nodes/${nodeId}/versions/${versionId}/restore`, { method: "POST" }),
    onSuccess: (_d, { nodeId }) => {
      invalidate();
      void qc.invalidateQueries({ queryKey: keys.versions(nodeId) });
    },
  });
}

export function useCreateShare() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ nodeId, body }: { nodeId: string; body: Record<string, unknown> }) =>
      api<Share>(`/api/v1/nodes/${nodeId}/shares`, postJson(body)),
    onSuccess: (_d, { nodeId }) => void qc.invalidateQueries({ queryKey: keys.shares(nodeId) }),
  });
}

export function useRevokeShare() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ nodeId, shareId }: { nodeId: string; shareId: string }) =>
      fetch(`/api/v1/nodes/${nodeId}/shares/${shareId}`, { method: "DELETE" }).then((r) => {
        if (!r.ok) throw new Error(`${r.status}`);
      }),
    onSuccess: (_d, { nodeId }) => void qc.invalidateQueries({ queryKey: keys.shares(nodeId) }),
  });
}

export function useEditSession() {
  return useMutation({
    mutationFn: (nodeId: string) =>
      api<EditSession>(`/api/v1/nodes/${nodeId}/edit-session`, { method: "POST" }),
  });
}

export async function logout() {
  await fetch("/auth/logout", { method: "POST" });
}
