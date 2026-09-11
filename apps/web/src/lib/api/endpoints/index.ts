// API endpoints — typed functions that call the ky client.
// Each file covers one resource domain.

import { api, ApiError } from "../client";
import type { EditSession, Me, Node, Share, ShareInfo, Space, TreeNode, Version } from "../types";

// ─── Auth ───────────────────────────────────────────────────────────────────

export async function getMe(): Promise<Me> {
  const res = await api.get("api/v1/me");
  if (!res.ok) throw new ApiError(res.status, await res.text());
  return res.json() as Promise<Me>;
}

// ─── Spaces ─────────────────────────────────────────────────────────────────

export async function getSpaces(): Promise<Space[]> {
  const res = await api.get("api/v1/spaces");
  if (!res.ok) throw new ApiError(res.status, await res.text());
  return res.json() as Promise<Space[]>;
}

export async function createSpace(name: string): Promise<Space> {
  const res = await api.post("api/v1/spaces", { json: { name } });
  if (!res.ok) throw new ApiError(res.status, await res.text());
  return res.json() as Promise<Space>;
}

// ─── Nodes ───────────────────────────────────────────────────────────────────

export async function getChildren(spaceId: string, parentId?: string): Promise<Node[]> {
  const url = parentId
    ? `api/v1/spaces/${spaceId}/children?parent=${parentId}`
    : `api/v1/spaces/${spaceId}/children`;
  const res = await api.get(url);
  if (!res.ok) throw new ApiError(res.status, await res.text());
  return res.json() as Promise<Node[]>;
}

export async function createFolder(
  spaceId: string,
  name: string,
  parentId?: string,
): Promise<Node> {
  const res = await api.post(`api/v1/spaces/${spaceId}/dirs`, {
    json: { name, parent_id: parentId },
  });
  if (!res.ok) throw new ApiError(res.status, await res.text());
  return res.json() as Promise<Node>;
}

export async function uploadFile(spaceId: string, file: File, parentId?: string): Promise<Node> {
  const fd = new FormData();
  if (parentId) fd.append("parent_id", parentId);
  fd.append("file", file);
  const res = await api.post(`api/v1/spaces/${spaceId}/files`, { body: fd });
  if (!res.ok) throw new ApiError(res.status, await res.text());
  return res.json() as Promise<Node>;
}

export async function renameNode(nodeId: string, name: string): Promise<Node> {
  const res = await api.patch(`api/v1/nodes/${nodeId}/rename`, {
    json: { name },
  });
  if (!res.ok) throw new ApiError(res.status, await res.text());
  return res.json() as Promise<Node>;
}

export async function deleteNode(nodeId: string): Promise<void> {
  const res = await api.delete(`api/v1/nodes/${nodeId}`);
  if (!res.ok) throw new ApiError(res.status, await res.text());
}

// ─── Versions ────────────────────────────────────────────────────────────────

export async function getVersions(nodeId: string): Promise<Version[]> {
  const res = await api.get(`api/v1/nodes/${nodeId}/versions`);
  if (!res.ok) throw new ApiError(res.status, await res.text());
  return res.json() as Promise<Version[]>;
}

export async function restoreVersion(nodeId: string, versionId: string): Promise<void> {
  const res = await api.post(`api/v1/nodes/${nodeId}/versions/${versionId}/restore`);
  if (!res.ok) throw new ApiError(res.status, await res.text());
}

// ─── Shares ──────────────────────────────────────────────────────────────────

export async function getShares(nodeId: string): Promise<Share[]> {
  const res = await api.get(`api/v1/nodes/${nodeId}/shares`);
  if (!res.ok) throw new ApiError(res.status, await res.text());
  return res.json() as Promise<Share[]>;
}

export async function createShare(nodeId: string, body: Record<string, unknown>): Promise<Share> {
  const res = await api.post(`api/v1/nodes/${nodeId}/shares`, { json: body });
  if (!res.ok) throw new ApiError(res.status, await res.text());
  return res.json() as Promise<Share>;
}

export async function revokeShare(nodeId: string, shareId: string): Promise<void> {
  const res = await api.delete(`api/v1/nodes/${nodeId}/shares/${shareId}`);
  if (!res.ok) throw new ApiError(res.status, await res.text());
}

// ─── Share Links ─────────────────────────────────────────────────────────────

export async function getShareInfo(token: string): Promise<ShareInfo> {
  const res = await api.get(`s/${token}`);
  if (!res.ok) throw new ApiError(res.status, await res.text());
  return res.json() as Promise<ShareInfo>;
}

// ─── WOPI ───────────────────────────────────────────────────────────────────

export async function createEditSession(nodeId: string): Promise<EditSession> {
  const res = await api.post(`api/v1/nodes/${nodeId}/edit-session`);
  if (!res.ok) throw new ApiError(res.status, await res.text());
  return res.json() as Promise<EditSession>;
}

// ─── Auth Actions ───────────────────────────────────────────────────────────

export async function logout(): Promise<void> {
  const res = await api.post("auth/logout");
  if (!res.ok) throw new ApiError(res.status, await res.text());
}

// ─── Tree / Trash / Shared (Sidebar) ─────────────────────────────────────────

export async function getTree(spaceId: string, parentId?: string): Promise<TreeNode[]> {
  const url = parentId
    ? `api/v1/spaces/${spaceId}/tree?parent=${parentId}`
    : `api/v1/spaces/${spaceId}/tree`;
  const res = await api.get(url);
  if (!res.ok) throw new ApiError(res.status, await res.text());
  return res.json() as Promise<TreeNode[]>;
}

export async function getTrash(spaceId: string): Promise<Node[]> {
  const res = await api.get(`api/v1/spaces/${spaceId}/trash`);
  if (!res.ok) throw new ApiError(res.status, await res.text());
  return res.json() as Promise<Node[]>;
}

export async function getShared(): Promise<Node[]> {
  const res = await api.get(`api/v1/spaces/shared`);
  if (!res.ok) throw new ApiError(res.status, await res.text());
  return res.json() as Promise<Node[]>;
}
