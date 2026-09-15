// API endpoints — typed functions that call the ky client.
// Each file covers one resource domain.

import { api, ApiError } from "../client";
import type {
  ActivityEvent,
  AdminStats,
  AdminUser,
  Invitation,
  InviteResult,
  MemberRole,
  EditSession,
  Me,
  Node,
  RecentNode,
  RestoreResult,
  SearchKind,
  SearchResult,
  Share,
  UsageCategory,
  ShareInfo,
  Space,
  TrashNode,
  TreeNode,
  Version,
} from "../types";

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

/** Everything but the name is optional and falls back to the space defaults. */
export async function createSpace(
  name: string,
  settings: Omit<SpaceSettings, "name" | "trash_retention_days"> = {},
): Promise<Space> {
  const res = await api.post("api/v1/spaces", { json: { name, ...settings } });
  if (!res.ok) throw new ApiError(res.status, await res.text());
  return res.json() as Promise<Space>;
}

export interface SpaceSettings {
  name?: string;
  slug?: string;
  description?: string;
  default_role?: MemberRole;
  quota_bytes?: number;
  /** Days to keep trashed items. 0 keeps them indefinitely. */
  trash_retention_days?: number;
}

/** Absent fields keep their current value. */
export async function updateSpace(spaceId: string, settings: SpaceSettings): Promise<Space> {
  const res = await api.patch(`api/v1/spaces/${spaceId}`, { json: settings });
  if (!res.ok) throw new ApiError(res.status, await res.text());
  return res.json() as Promise<Space>;
}

/** Removes the space and everything in it. Not recoverable. */
export async function deleteSpace(spaceId: string): Promise<void> {
  const res = await api.delete(`api/v1/spaces/${spaceId}`);
  if (!res.ok) throw new ApiError(res.status, await res.text());
}

// ─── Invitations ─────────────────────────────────────────────────────────────

export async function getInvitations(spaceId: string): Promise<Invitation[]> {
  const res = await api.get(`api/v1/spaces/${spaceId}/invitations`);
  if (!res.ok) throw new ApiError(res.status, await res.text());
  return res.json() as Promise<Invitation[]>;
}

/**
 * Invites an email address. If it already belongs to an account the server
 * adds them as a member directly and says so in `added_member`.
 */
export async function createInvitation(
  spaceId: string,
  email: string,
  role: MemberRole,
): Promise<InviteResult> {
  const res = await api.post(`api/v1/spaces/${spaceId}/invitations`, { json: { email, role } });
  if (!res.ok) throw new ApiError(res.status, await res.text());
  return res.json() as Promise<InviteResult>;
}

export async function revokeInvitation(spaceId: string, inviteId: string): Promise<void> {
  const res = await api.delete(`api/v1/spaces/${spaceId}/invitations/${inviteId}`);
  if (!res.ok) throw new ApiError(res.status, await res.text());
}

export async function acceptInvitation(token: string): Promise<Space> {
  const res = await api.post("api/v1/invitations/accept", { json: { token } });
  if (!res.ok) throw new ApiError(res.status, await res.text());
  return res.json() as Promise<Space>;
}

// ─── Admin ───────────────────────────────────────────────────────────────────

export async function getAdminStats(): Promise<AdminStats> {
  const res = await api.get("api/v1/admin/stats");
  if (!res.ok) throw new ApiError(res.status, await res.text());
  return res.json() as Promise<AdminStats>;
}

export async function updateAdminUser(
  userId: string,
  patch: { is_admin?: boolean; suspended?: boolean },
): Promise<AdminUser> {
  const res = await api.patch(`api/v1/admin/users/${userId}`, { json: patch });
  if (!res.ok) throw new ApiError(res.status, await res.text());
  return res.json() as Promise<AdminUser>;
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

/** One node with its current version and owner. */
export async function getNode(nodeId: string): Promise<Node> {
  const res = await api.get(`api/v1/nodes/${nodeId}`);
  if (!res.ok) throw new ApiError(res.status, await res.text());
  return res.json() as Promise<Node>;
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

/**
 * Upload with progress. `fetch` (and so ky) exposes no upload progress events,
 * so the transfer tray needs XMLHttpRequest — this is the one endpoint that
 * does not go through the shared client.
 *
 * Returns an abort function alongside the promise; cancelling rejects it with
 * an ApiError of status 0.
 */
export function uploadFileWithProgress(
  spaceId: string,
  file: File,
  parentId: string | undefined,
  onProgress: (loaded: number, total: number) => void,
): { promise: Promise<Node>; abort: () => void } {
  const xhr = new XMLHttpRequest();
  const promise = new Promise<Node>((resolve, reject) => {
    const fd = new FormData();
    if (parentId) fd.append("parent_id", parentId);
    fd.append("file", file);

    xhr.upload.addEventListener("progress", (e) => {
      if (e.lengthComputable) onProgress(e.loaded, e.total);
    });
    xhr.addEventListener("load", () => {
      if (xhr.status >= 200 && xhr.status < 300) {
        // The last progress event can arrive before the response; make sure the
        // row reads 100% rather than freezing a beat short.
        onProgress(file.size, file.size);
        resolve(JSON.parse(xhr.responseText) as Node);
      } else {
        reject(new ApiError(xhr.status, xhr.responseText || xhr.statusText));
      }
    });
    xhr.addEventListener("error", () => reject(new ApiError(0, "Network error")));
    xhr.addEventListener("abort", () => reject(new ApiError(0, "Cancelled")));

    xhr.open("POST", `/api/v1/spaces/${spaceId}/files`);
    xhr.withCredentials = true;
    xhr.send(fd);
  });
  return { promise, abort: () => xhr.abort() };
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

/**
 * Duplicates a node. An undefined destination copies it alongside the original.
 * Files copy by blob reference, so duplicating a large file is cheap.
 */
export async function copyNode(nodeId: string, parentId?: string): Promise<Node> {
  const res = await api.post(`api/v1/nodes/${nodeId}/copy`, {
    json: { parent_id: parentId ?? "" },
  });
  if (!res.ok) throw new ApiError(res.status, await res.text());
  return res.json() as Promise<Node>;
}

/** Relocates a node. An undefined parent means the top level of the space. */
export async function moveNode(nodeId: string, parentId?: string): Promise<void> {
  const res = await api.patch(`api/v1/nodes/${nodeId}/move`, {
    json: { parent_id: parentId ?? "" },
  });
  if (!res.ok) throw new ApiError(res.status, await res.text());
}

// ─── Trash ───────────────────────────────────────────────────────────────────

export async function restoreNode(nodeId: string): Promise<RestoreResult> {
  const res = await api.post(`api/v1/nodes/${nodeId}/restore`);
  if (!res.ok) throw new ApiError(res.status, await res.text());
  return res.json() as Promise<RestoreResult>;
}

/** Permanent, unrecoverable. Takes the node's whole subtree with it. */
export async function purgeNode(nodeId: string): Promise<void> {
  const res = await api.delete(`api/v1/nodes/${nodeId}/purge`);
  if (!res.ok) throw new ApiError(res.status, await res.text());
}

export async function emptyTrash(spaceId: string): Promise<number> {
  const res = await api.post(`api/v1/spaces/${spaceId}/trash/empty`);
  if (!res.ok) throw new ApiError(res.status, await res.text());
  const body = (await res.json()) as { purged: number };
  return body.purged;
}

// ─── Search ──────────────────────────────────────────────────────────────────

export interface SearchOptions {
  /** Restrict to one space. Omit to search every space you belong to. */
  spaceId?: string;
  kind?: SearchKind;
  /** Restrict to files created by one user. */
  ownerId?: string;
  /** Days back, or an RFC 3339 instant. */
  modified?: number | string;
  limit?: number;
}

/**
 * Name search across the caller's spaces. The server answers a query shorter
 * than two characters with an empty list rather than an error, so the palette
 * can call this on every keystroke.
 */
export async function searchNodes(
  query: string,
  { spaceId, kind, ownerId, modified, limit }: SearchOptions = {},
): Promise<SearchResult[]> {
  const params = new URLSearchParams({ q: query });
  if (spaceId) params.set("space", spaceId);
  if (kind) params.set("kind", kind);
  if (ownerId) params.set("owner", ownerId);
  if (modified) params.set("modified", String(modified));
  if (limit) params.set("limit", String(limit));

  const res = await api.get(`api/v1/search?${params}`);
  if (!res.ok) throw new ApiError(res.status, await res.text());
  return res.json() as Promise<SearchResult[]>;
}

// ─── Home: recent, starred, activity, usage ─────────────────────────────────

/** Files the caller last touched, most recent first. */
export async function getRecent(limit?: number): Promise<RecentNode[]> {
  const res = await api.get(`api/v1/recent${limit ? `?limit=${limit}` : ""}`);
  if (!res.ok) throw new ApiError(res.status, await res.text());
  return res.json() as Promise<RecentNode[]>;
}

export async function getStarred(limit?: number): Promise<SearchResult[]> {
  const res = await api.get(`api/v1/starred${limit ? `?limit=${limit}` : ""}`);
  if (!res.ok) throw new ApiError(res.status, await res.text());
  return res.json() as Promise<SearchResult[]>;
}

/** Activity across every space you belong to, or one space when given. */
export async function getActivity(spaceId?: string, limit?: number): Promise<ActivityEvent[]> {
  const params = new URLSearchParams();
  if (spaceId) params.set("space", spaceId);
  if (limit) params.set("limit", String(limit));
  const qs = params.toString();
  const res = await api.get(`api/v1/activity${qs ? `?${qs}` : ""}`);
  if (!res.ok) throw new ApiError(res.status, await res.text());
  return res.json() as Promise<ActivityEvent[]>;
}

export async function getUsage(spaceId?: string): Promise<UsageCategory[]> {
  const res = await api.get(`api/v1/usage${spaceId ? `?space=${spaceId}` : ""}`);
  if (!res.ok) throw new ApiError(res.status, await res.text());
  return res.json() as Promise<UsageCategory[]>;
}

/** A star is the caller's own bookmark, so it needs only read access. */
export async function starNode(nodeId: string, starred: boolean): Promise<void> {
  const res = starred
    ? await api.post(`api/v1/nodes/${nodeId}/star`)
    : await api.delete(`api/v1/nodes/${nodeId}/star`);
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

/**
 * Lists a folder inside a shared subtree. `dir` must be the share root or one
 * of its descendants — the server verifies containment and answers 404 for
 * anything else, so a token cannot be used to walk the wider drive.
 */
export async function getShareChildren(token: string, dir?: string): Promise<Node[]> {
  const url = dir ? `s/${token}/children?dir=${dir}` : `s/${token}/children`;
  const res = await api.get(url);
  if (!res.ok) throw new ApiError(res.status, await res.text());
  return res.json() as Promise<Node[]>;
}

/** Download URL for one file inside a share. */
export function shareNodeDownloadUrl(token: string, nodeId: string): string {
  return `/s/${token}/node-download?node=${nodeId}`;
}

/** Inline content URL for one file inside a share, for previewing. */
export function shareNodeContentUrl(token: string, nodeId: string): string {
  return `/s/${token}/content?node=${nodeId}`;
}

/** Zip of a shared folder. Omit `dir` for the share root. */
export function shareDownloadAllUrl(token: string, dir?: string): string {
  return dir ? `/s/${token}/download-all?dir=${dir}` : `/s/${token}/download-all`;
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

export async function getTrash(spaceId: string): Promise<TrashNode[]> {
  const res = await api.get(`api/v1/spaces/${spaceId}/trash`);
  if (!res.ok) throw new ApiError(res.status, await res.text());
  return res.json() as Promise<TrashNode[]>;
}

export async function getShared(): Promise<Node[]> {
  const res = await api.get("api/v1/shared");
  if (!res.ok) throw new ApiError(res.status, await res.text());
  return res.json() as Promise<Node[]>;
}
