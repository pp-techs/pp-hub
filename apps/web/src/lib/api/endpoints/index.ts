// API endpoints — typed functions that call the ky client.
// Each file covers one resource domain.

import { api, ApiError } from "../client";
import type {
  ActivityEvent,
  AdminSettingsMap,
  AdminStats,
  AdminUser,
  CreateShareBody,
  DocumentKind,
  EditSession,
  InheritedAccess,
  MoveImpact,
  Node,
  RecentNode,
  RestoreResult,
  SearchKind,
  SearchResult,
  Share,
  UsageCategory,
  ShareInfo,
  LinkPreview,
  Space,
  TrashNode,
  TreeNode,
  Version,
} from "../types";

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
  default_as_admin?: boolean;
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

/** What linking this unlinked space would do to its members. Instance admin only. */
export async function getLinkPreview(spaceId: string): Promise<LinkPreview> {
  const res = await api.get(`api/v1/spaces/${spaceId}/link-preview`);
  if (!res.ok) throw new ApiError(res.status, await res.text());
  return res.json() as Promise<LinkPreview>;
}

/**
 * Creates a Logto organization for this space and queues the push of its
 * members; the space reads as linked once that has run. `confirmStranded`
 * acknowledges members with no Logto account, who become departures — the
 * server answers 409 without it when there are any.
 */
export async function linkSpace(spaceId: string, confirmStranded: boolean): Promise<void> {
  const res = await api.post(`api/v1/spaces/${spaceId}/link`, {
    json: { confirm_stranded: confirmStranded },
  });
  if (!res.ok) throw new ApiError(res.status, await res.text());
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

export async function getAdminSettings(): Promise<AdminSettingsMap> {
  const res = await api.get("api/v1/admin/settings");
  if (!res.ok) throw new ApiError(res.status, await res.text());
  return res.json() as Promise<AdminSettingsMap>;
}

export async function updateAdminSettings(
  patch: Record<string, unknown>,
): Promise<AdminSettingsMap> {
  const res = await api.patch("api/v1/admin/settings", { json: patch });
  if (!res.ok) throw new ApiError(res.status, await res.text());
  return res.json() as Promise<AdminSettingsMap>;
}

export async function deleteAdminSetting(key: string): Promise<AdminSettingsMap> {
  const res = await api.delete(`api/v1/admin/settings/${key}`);
  if (!res.ok) throw new ApiError(res.status, await res.text());
  return res.json() as Promise<AdminSettingsMap>;
}

export async function sendTestEmail(): Promise<{ to: string }> {
  const res = await api.post("api/v1/admin/settings/smtp/test");
  if (!res.ok) throw new ApiError(res.status, await res.text());
  return res.json() as Promise<{ to: string }>;
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

/**
 * Creates a folder the team owns: no personal owner, access only through
 * grants on it, and it survives its creator leaving the company. Space admins
 * only — a team folder is company structure, not somebody's folder.
 */
export async function createTeamFolder(spaceId: string, name: string): Promise<Node> {
  const res = await api.post(`api/v1/spaces/${spaceId}/team-folders`, { json: { name } });
  if (!res.ok) throw new ApiError(res.status, await res.text());
  return res.json() as Promise<Node>;
}

export async function createDocument(
  spaceId: string,
  kind: DocumentKind,
  name: string,
  parentId?: string,
): Promise<Node> {
  const res = await api.post(`api/v1/spaces/${spaceId}/documents`, {
    json: { kind, name, parent_id: parentId },
  });
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

/**
 * Who gains and who loses access if `nodeId` moves under `parentId`, asked
 * before the move. Access is inherited, so a move rehangs the node under a
 * different chain of owners and grants: moving out of a colleague's folder
 * takes it away from them, moving into a shared one hands it over.
 *
 * An undefined parent means the caller's top level, which inherits nothing.
 */
export async function moveImpact(nodeId: string, parentId?: string): Promise<MoveImpact> {
  const res = await api.get(`api/v1/nodes/${nodeId}/move-impact`, {
    searchParams: parentId ? { parent: parentId } : {},
  });
  if (!res.ok) throw new ApiError(res.status, await res.text());
  return res.json() as Promise<MoveImpact>;
}

// ─── Trash ───────────────────────────────────────────────────────────────────

export async function restoreNode(nodeId: string): Promise<RestoreResult> {
  const res = await api.post(`api/v1/nodes/${nodeId}/restore`);
  if (!res.ok) throw new ApiError(res.status, await res.text());
  return res.json() as Promise<RestoreResult>;
}

/**
 * Permanent and unrecoverable for everything the node's owner created.
 * Subtrees belonging to other people are lifted out to their own top level
 * instead of following the cascade; the count comes back so the UI can say so.
 */
export async function purgeNode(nodeId: string): Promise<{ rescued: number }> {
  const res = await api.delete(`api/v1/nodes/${nodeId}/purge`);
  if (!res.ok) throw new ApiError(res.status, await res.text());
  return res.json() as Promise<{ rescued: number }>;
}

export async function emptyTrash(spaceId: string): Promise<{ purged: number; rescued: number }> {
  const res = await api.post(`api/v1/spaces/${spaceId}/trash/empty`);
  if (!res.ok) throw new ApiError(res.status, await res.text());
  return res.json() as Promise<{ purged: number; rescued: number }>;
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

// ─── Space feeds: recent, starred, activity, usage ──────────────────────────

/**
 * Every feed is scoped to one space. The server accepts `?space=` as optional
 * and would otherwise answer across every space the caller belongs to, which
 * the UI no longer has a surface for: a row from another space cannot be
 * opened without leaving the space the rail is showing.
 */
export async function getRecent(spaceId: string, limit?: number): Promise<RecentNode[]> {
  const params = new URLSearchParams({ space: spaceId });
  if (limit) params.set("limit", String(limit));
  const res = await api.get(`api/v1/recent?${params}`);
  if (!res.ok) throw new ApiError(res.status, await res.text());
  return res.json() as Promise<RecentNode[]>;
}

export async function getStarred(spaceId: string, limit?: number): Promise<SearchResult[]> {
  const params = new URLSearchParams({ space: spaceId });
  if (limit) params.set("limit", String(limit));
  const res = await api.get(`api/v1/starred?${params}`);
  if (!res.ok) throw new ApiError(res.status, await res.text());
  return res.json() as Promise<SearchResult[]>;
}

/** The space's activity feed. */
export async function getActivity(spaceId: string, limit?: number): Promise<ActivityEvent[]> {
  const params = new URLSearchParams({ space: spaceId });
  if (limit) params.set("limit", String(limit));
  const res = await api.get(`api/v1/activity?${params}`);
  if (!res.ok) throw new ApiError(res.status, await res.text());
  return res.json() as Promise<ActivityEvent[]>;
}

export async function getUsage(spaceId: string): Promise<UsageCategory[]> {
  const res = await api.get(`api/v1/usage?space=${spaceId}`);
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

/**
 * Creates a share, or — for a direct share to someone who already holds one
 * on this node — updates it: 201 created, 200 updated, same body either way.
 */
export async function createShare(nodeId: string, body: CreateShareBody): Promise<Share> {
  const res = await api.post(`api/v1/nodes/${nodeId}/shares`, { json: body });
  if (!res.ok) throw new ApiError(res.status, await res.text());
  return res.json() as Promise<Share>;
}

export async function revokeShare(nodeId: string, shareId: string): Promise<void> {
  const res = await api.delete(`api/v1/nodes/${nodeId}/shares/${shareId}`);
  if (!res.ok) throw new ApiError(res.status, await res.text());
}

/**
 * Everyone reaching this node from above it: owners and ancestor grants. The
 * node's own grants are `getShares`/`getGrants`; together they are the whole
 * answer to who can open it. Takes share on the node, like `getShares`.
 */
export async function getInheritedAccess(nodeId: string): Promise<InheritedAccess[]> {
  const res = await api.get(`api/v1/nodes/${nodeId}/inherited-access`);
  if (!res.ok) throw new ApiError(res.status, await res.text());
  return res.json() as Promise<InheritedAccess[]>;
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

export async function createEditSession(
  nodeId: string,
  locale?: string,
  theme?: "light" | "dark",
): Promise<EditSession> {
  // `ui` is what the Document Server reads into `editorConfig.lang`, so the
  // OnlyOffice ribbon comes up in the language the app is currently in
  // rather than the account column, which is empty for anyone who never
  // chose one. The server ignores the parameter when it is absent or not a
  // locale it ships.
  //
  // `theme` travels the same way and for the same reason: the choice lives
  // in this browser's localStorage, and "system" only has an answer here.
  // Send the resolved one, never "system".
  const query = new URLSearchParams();
  if (locale) query.set("ui", locale);
  if (theme) query.set("theme", theme);
  const suffix = query.size > 0 ? `?${query.toString()}` : "";
  const res = await api.post(`api/v1/nodes/${nodeId}/edit-session${suffix}`);
  if (!res.ok) throw new ApiError(res.status, await res.text());
  return res.json() as Promise<EditSession>;
}

// ─── Auth Actions ───────────────────────────────────────────────────────────

/**
 * Ends the server-side session. `end_session_url` is the provider's
 * RP-initiated logout URL when it has one, and `""` when it does not — with a
 * URL the browser has to leave the origin, or the IdP would simply hand the
 * same account straight back on the next sign-in.
 */
export async function logout(): Promise<{ end_session_url: string }> {
  const res = await api.post("auth/logout");
  if (!res.ok) throw new ApiError(res.status, await res.text());
  return res.json() as Promise<{ end_session_url: string }>;
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

/** Grant entry points inside one space — where the caller's access begins. */
export async function getShared(spaceId: string): Promise<Node[]> {
  const res = await api.get(`api/v1/shared?space=${spaceId}`);
  if (!res.ok) throw new ApiError(res.status, await res.text());
  return res.json() as Promise<Node[]>;
}
