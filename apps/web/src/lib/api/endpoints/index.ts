// API endpoints — typed functions that call the ky client.
// Each file covers one resource domain.

import { api, ApiError } from "../client";
import type {
  ActivityEvent,
  AdminApp,
  AdminSettingsMap,
  ArchivedPersonalSpace,
  AdminStats,
  AdminUser,
  CollabSession,
  CreateShareBody,
  DocComment,
  DocCommentThread,
  DocumentKind,
  Extension,
  EditSession,
  InheritedAccess,
  MarkdownDocument,
  MarkdownTemplate,
  MoveImpact,
  Node,
  NotificationPage,
  PageLineage,
  PageSummary,
  RecentNode,
  RelativeEmbed,
  RelativeNode,
  RestoreResult,
  SearchKind,
  SearchResult,
  Share,
  UsageCategory,
  ShareInfo,
  LinkPreview,
  NodePerson,
  PathEntry,
  PendingShare,
  PendingShareCreated,
  Publication,
  PublicationBody,
  PublicPublication,
  Space,
  TrashNode,
  TreeNode,
  UnfurledLink,
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
  allow_public_links?: boolean;
  /** Organization spaces only. */
  external_sharing?: boolean;
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

/** The space's picture. Bytes are sniffed server-side: PNG, JPEG, GIF or WebP. */
export async function uploadSpaceAvatar(
  spaceId: string,
  file: File,
): Promise<{ avatar_url: string }> {
  const form = new FormData();
  form.append("avatar", file);
  const res = await api.post(`api/v1/spaces/${spaceId}/avatar`, { body: form });
  if (!res.ok) throw new ApiError(res.status, await res.text());
  return res.json();
}

/** Removes the picture; the space falls back to its initials everywhere. */
export async function clearSpaceAvatar(spaceId: string): Promise<void> {
  const res = await api.delete(`api/v1/spaces/${spaceId}/avatar`);
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

/** Personal spaces left behind by deleted accounts. Admin only. */
export async function getArchivedPersonalSpaces(): Promise<ArchivedPersonalSpace[]> {
  const res = await api.get("api/v1/admin/personal-spaces");
  if (!res.ok) throw new ApiError(res.status, await res.text());
  return res.json() as Promise<ArchivedPersonalSpace[]>;
}

/** Deletes an archived personal space and everything in it, for good. */
export async function purgeArchivedPersonalSpace(spaceId: string): Promise<void> {
  const res = await api.delete(`api/v1/admin/personal-spaces/${spaceId}`);
  if (!res.ok) throw new ApiError(res.status, await res.text());
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

/** A folder's children by node id — works for grantees outside the space. */
export async function getNodeChildren(nodeId: string): Promise<Node[]> {
  const res = await api.get(`api/v1/nodes/${nodeId}/children`);
  if (!res.ok) throw new ApiError(res.status, await res.text());
  return res.json() as Promise<Node[]>;
}

/** From the topmost ancestor the caller can view down to the node itself. */
export async function getNodePath(nodeId: string): Promise<PathEntry[]> {
  const res = await api.get(`api/v1/nodes/${nodeId}/path`);
  if (!res.ok) throw new ApiError(res.status, await res.text());
  return res.json() as Promise<PathEntry[]>;
}

/** Mention candidates for a document: members plus grantees and its creator. */
export async function getNodePeople(nodeId: string): Promise<NodePerson[]> {
  const res = await api.get(`api/v1/nodes/${nodeId}/people`);
  if (!res.ok) throw new ApiError(res.status, await res.text());
  return res.json() as Promise<NodePerson[]>;
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

/**
 * `template` (markdown only) seeds the new document from one of
 * `getMarkdownTemplates()` instead of leaving it empty, written in `locale` —
 * the language the picker listed it in.
 */
export async function createDocument(
  spaceId: string,
  kind: DocumentKind,
  name: string,
  parentId?: string,
  template?: { id: string; locale: string },
): Promise<Node> {
  const res = await api.post(`api/v1/spaces/${spaceId}/documents`, {
    json: {
      kind,
      name,
      parent_id: parentId,
      template: template?.id,
      locale: template?.locale,
    },
  });
  if (!res.ok) throw new ApiError(res.status, await res.text());
  return res.json() as Promise<Node>;
}

/** The extensions the server carries; `enabled` is effective for `spaceId`, or the instance switch without one. */
export async function getExtensions(spaceId?: string): Promise<Extension[]> {
  const res = await api.get("api/v1/extensions", {
    searchParams: spaceId ? { space_id: spaceId } : undefined,
  });
  if (!res.ok) throw new ApiError(res.status, await res.text());
  return res.json() as Promise<Extension[]>;
}

/** Instance admin: turns an extension on or off for the whole instance. */
export async function setInstanceExtension(extId: string, enabled: boolean): Promise<Extension[]> {
  const res = await api.patch(`api/v1/admin/extensions/${extId}`, { json: { enabled } });
  if (!res.ok) throw new ApiError(res.status, await res.text());
  return res.json() as Promise<Extension[]>;
}

/** Space admin: turns an extension the instance enabled off for one space, or back on. */
export async function setSpaceExtension(
  spaceId: string,
  extId: string,
  enabled: boolean,
): Promise<Extension[]> {
  const res = await api.put(`api/v1/spaces/${spaceId}/extensions/${extId}`, { json: { enabled } });
  if (!res.ok) throw new ApiError(res.status, await res.text());
  return res.json() as Promise<Extension[]>;
}

/** Creates an extension node (a kanban board) in a folder, or at the space's top level. */
export async function createExtNode(
  spaceId: string,
  extKind: string,
  name: string,
  parentId?: string,
): Promise<Node> {
  const res = await api.post(`api/v1/spaces/${spaceId}/ext-nodes`, {
    json: { ext_kind: extKind, name, parent_id: parentId },
  });
  if (!res.ok) throw new ApiError(res.status, await res.text());
  return res.json() as Promise<Node>;
}

/** An app's nodes in a space, by name, only those the caller can open. */
export async function listExtNodes(spaceId: string, extId: string): Promise<Node[]> {
  const res = await api.get(`api/v1/spaces/${spaceId}/extensions/${extId}/nodes`);
  if (!res.ok) throw new ApiError(res.status, await res.text());
  return res.json() as Promise<Node[]>;
}

/**
 * Instance admin: installs an uploaded app (a .zip), or with `upgrade`
 * replaces the installed version of the same id (M51).
 */
export async function installApp(file: Blob, upgrade: boolean, id?: string): Promise<AdminApp> {
  const body = new FormData();
  body.append("package", file, "app.zip");
  const res = await api.post("api/v1/admin/apps", {
    body,
    searchParams: { upgrade: upgrade ? "true" : undefined, id },
  });
  if (!res.ok) throw new ApiError(res.status, await res.text());
  return res.json() as Promise<AdminApp>;
}

export async function getAdminApp(extId: string): Promise<AdminApp> {
  const res = await api.get(`api/v1/admin/apps/${extId}`);
  if (!res.ok) throw new ApiError(res.status, await res.text());
  return res.json() as Promise<AdminApp>;
}

/** The app's backend address; "" goes back to the manifest's. */
export async function updateApp(extId: string, backendURL: string): Promise<AdminApp> {
  const res = await api.patch(`api/v1/admin/apps/${extId}`, { json: { backend_url: backendURL } });
  if (!res.ok) throw new ApiError(res.status, await res.text());
  return res.json() as Promise<AdminApp>;
}

/** A new key for the app's backend tokens, returned this once. */
export async function rotateAppSecret(extId: string): Promise<{ secret: string; app: AdminApp }> {
  const res = await api.post(`api/v1/admin/apps/${extId}/backend-secret`);
  if (!res.ok) throw new ApiError(res.status, await res.text());
  return res.json() as Promise<{ secret: string; app: AdminApp }>;
}

/** Uninstalls an uploaded app; `purge` also deletes what its items hold. */
export async function removeApp(extId: string, purge: boolean): Promise<void> {
  const res = await api.delete(`api/v1/admin/apps/${extId}`, {
    searchParams: purge ? { purge: "true" } : undefined,
  });
  if (!res.ok) throw new ApiError(res.status, await res.text());
}

/** Renames a node; the server answers no node, only success. */
export async function renameNode(nodeId: string, name: string): Promise<void> {
  const res = await api.patch(`api/v1/nodes/${nodeId}/rename`, {
    json: { name },
  });
  if (!res.ok) throw new ApiError(res.status, await res.text());
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

/**
 * Relocates a node. An undefined parent means the top level of the space. With
 * `index` the node is a sub-page placed at that position among the parent's
 * sub-pages (counted without the node itself; the server clamps it).
 */
export async function moveNode(nodeId: string, parentId?: string, index?: number): Promise<void> {
  const res = await api.patch(`api/v1/nodes/${nodeId}/move`, {
    json: { parent_id: parentId ?? "", ...(index !== undefined && { index }) },
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
export async function createShare(
  nodeId: string,
  body: CreateShareBody,
): Promise<Share | PendingShareCreated> {
  const res = await api.post(`api/v1/nodes/${nodeId}/shares`, { json: body });
  if (!res.ok) throw new ApiError(res.status, await res.text());
  return res.json() as Promise<Share | PendingShareCreated>;
}

/** Invitations to email addresses that have no account yet. Share-gated. */
export async function getPendingShares(nodeId: string): Promise<PendingShare[]> {
  const res = await api.get(`api/v1/nodes/${nodeId}/pending-shares`);
  if (!res.ok) throw new ApiError(res.status, await res.text());
  return res.json() as Promise<PendingShare[]>;
}

export async function revokePendingShare(nodeId: string, pendingId: string): Promise<void> {
  const res = await api.delete(`api/v1/nodes/${nodeId}/pending-shares/${pendingId}`);
  if (!res.ok) throw new ApiError(res.status, await res.text());
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

// ─── Publishing (M50) ───────────────────────────────────────────────────────

/** The node's publication, or `null` when it has none (404). */
export async function getPublication(nodeId: string): Promise<Publication | null> {
  const res = await api.get(`api/v1/nodes/${nodeId}/publication`);
  if (res.status === 404) return null;
  if (!res.ok) throw new ApiError(res.status, await res.text());
  return res.json() as Promise<Publication>;
}

/** Publishes, or republishes: a new snapshot of the source under the same URL. */
export async function publishNode(nodeId: string, body: PublicationBody): Promise<Publication> {
  const res = await api.post(`api/v1/nodes/${nodeId}/publication`, { json: body });
  if (!res.ok) throw new ApiError(res.status, await res.text());
  return res.json() as Promise<Publication>;
}

/** Changes password or expiry without taking a new snapshot. */
export async function updatePublication(
  nodeId: string,
  body: PublicationBody,
): Promise<Publication> {
  const res = await api.patch(`api/v1/nodes/${nodeId}/publication`, { json: body });
  if (!res.ok) throw new ApiError(res.status, await res.text());
  return res.json() as Promise<Publication>;
}

export async function unpublishNode(nodeId: string): Promise<void> {
  const res = await api.delete(`api/v1/nodes/${nodeId}/publication`);
  if (!res.ok) throw new ApiError(res.status, await res.text());
}

/**
 * What a visitor of `/p/<token>` is told. The JSON Accept is what tells this
 * request apart from the browser navigation that loaded the SPA.
 */
export async function getPublicPublication(token: string): Promise<PublicPublication> {
  const res = await api.get(`p/${token}`, { headers: { Accept: "application/json" } });
  if (!res.ok) throw new ApiError(res.status, await res.text());
  return res.json() as Promise<PublicPublication>;
}

/**
 * Trades the password for a frame URL carrying a grant. A wrong password is
 * 422, never 401: the client reads 401 as "your session is gone".
 */
export async function unlockPublication(
  token: string,
  password: string,
): Promise<{ content_url: string }> {
  const res = await api.post(`p/${token}/unlock`, {
    json: { password },
    headers: { Accept: "application/json" },
  });
  if (!res.ok) throw new ApiError(res.status, await res.text());
  return res.json() as Promise<{ content_url: string }>;
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

/** Mints an OnlyOffice view session for an office document inside a share link. */
export async function createShareEditSession(
  token: string,
  nodeId?: string,
  locale?: string,
  theme?: "light" | "dark",
): Promise<EditSession> {
  const query = new URLSearchParams();
  if (nodeId) query.set("node", nodeId);
  if (locale) query.set("ui", locale);
  if (theme) query.set("theme", theme);
  const suffix = query.size > 0 ? `?${query.toString()}` : "";
  const res = await api.get(`s/${token}/edit-session${suffix}`);
  if (!res.ok) throw new ApiError(res.status, await res.text());
  return res.json() as Promise<EditSession>;
}

// ─── Block documents ────────────────────────────────────────────────────────

/** The room a markdown document is edited in right now, and what this user may do there. */
export async function getCollabSession(nodeId: string): Promise<CollabSession> {
  const res = await api.get(`api/v1/nodes/${nodeId}/collab`, { cache: "no-store" });
  if (!res.ok) throw new ApiError(res.status, await res.text());
  return res.json() as Promise<CollabSession>;
}

/** WebSocket address of a collab room, on this origin (the session cookie authenticates it). */
export function collabSocketUrl(room: string): string {
  const scheme = window.location.protocol === "https:" ? "wss" : "ws";
  return `${scheme}://${window.location.host}/api/v1/collab/${encodeURIComponent(room)}`;
}

/** Where a document's attachment is served; `hash` is what follows `attachment:` in the markdown. */
export function attachmentUrl(nodeId: string, hash: string): string {
  return `/api/v1/nodes/${nodeId}/attachments/${encodeURIComponent(hash)}`;
}

/**
 * Starting points for a new markdown document, named in `locale` — the UI's
 * language, which need not be the browser's or the saved profile's.
 */
export async function getMarkdownTemplates(locale: string): Promise<MarkdownTemplate[]> {
  const res = await api.get("api/v1/templates/markdown", { searchParams: { locale } });
  if (!res.ok) throw new ApiError(res.status, await res.text());
  return res.json() as Promise<MarkdownTemplate[]>;
}

/**
 * Reads a link server-side for a bookmark card in `documentId`. Needs edit
 * access to the document: the thumbnail is stored as one of its attachments.
 */
export async function unfurlLink(documentId: string, url: string): Promise<UnfurledLink> {
  const res = await api.post(`api/v1/nodes/${documentId}/unfurl`, { json: { url } });
  if (!res.ok) throw new ApiError(res.status, await res.text());
  return res.json() as Promise<UnfurledLink>;
}

/**
 * A markdown file parsed for read-only rendering: the current version, or
 * `versionId` when given (it must be one of this node's versions).
 */
export async function getNodeDocument(
  nodeId: string,
  versionId?: string,
): Promise<MarkdownDocument> {
  const res = await api.get(`api/v1/nodes/${nodeId}/document`, {
    searchParams: versionId ? { version: versionId } : {},
  });
  if (!res.ok) throw new ApiError(res.status, await res.text());
  return res.json() as Promise<MarkdownDocument>;
}

/** A markdown file inside a share link, parsed for rendering. The link must allow downloads. */
export async function getShareDocument(token: string, nodeId: string): Promise<MarkdownDocument> {
  const res = await api.get(`s/${token}/document`, { searchParams: { node: nodeId } });
  if (!res.ok) throw new ApiError(res.status, await res.text());
  return res.json() as Promise<MarkdownDocument>;
}

/** An attachment of `docId` as a share-link visitor reaches it. */
export function shareAttachmentUrl(token: string, docId: string, hash: string): string {
  return `/s/${token}/attachments/${encodeURIComponent(hash)}?node=${docId}`;
}

/** The document as a zip: the `.md` with its attachments under `assets/`. */
export function exportDocumentUrl(nodeId: string): string {
  return `/api/v1/nodes/${nodeId}/export`;
}

/**
 * Where a relative reference in a document (`images/a.png`, as written in
 * the markdown) is served: the file beside the document, on the document's
 * terms. `versionId` is the version being read, whose author decides how far
 * the document reaches.
 */
export function relativeUrl(nodeId: string, path: string, versionId?: string): string {
  const query = new URLSearchParams({ path });
  if (versionId) query.set("version", versionId);
  return `/api/v1/nodes/${nodeId}/relative?${query}`;
}

/** A relative reference of `docId` as a share-link visitor reaches it. */
export function shareRelativeUrl(token: string, docId: string, path: string): string {
  return `/s/${token}/relative?${new URLSearchParams({ node: docId, path })}`;
}

/** What a relative link in `nodeId` names, when the caller may open it. */
export async function getRelativeNode(nodeId: string, path: string): Promise<RelativeNode> {
  const res = await api.get(`api/v1/nodes/${nodeId}/relative/meta`, { searchParams: { path } });
  if (!res.ok) throw new ApiError(res.status, await res.text());
  return res.json() as Promise<RelativeNode>;
}

/** What a relative link in `docId` names, when the link covers it. */
export async function getShareRelativeNode(
  token: string,
  docId: string,
  path: string,
): Promise<RelativeNode> {
  const res = await api.get(`s/${token}/relative/meta`, { searchParams: { node: docId, path } });
  if (!res.ok) throw new ApiError(res.status, await res.text());
  return res.json() as Promise<RelativeNode>;
}

/**
 * Copies the file a relative reference names into the document's
 * attachments. Needs edit on the document and download on the file.
 */
export async function embedRelative(nodeId: string, path: string): Promise<RelativeEmbed> {
  const res = await api.post(`api/v1/nodes/${nodeId}/relative/embed`, { json: { path } });
  if (!res.ok) throw new ApiError(res.status, await res.text());
  return res.json() as Promise<RelativeEmbed>;
}

// ─── Pages (M43) ────────────────────────────────────────────────────────────
// A page is a markdown document; a sub-page is one whose parent is another
// document. Creating one is `createDocument` with the parent document as
// `parentId`, retitling is `renameNode`, removing is `deleteNode`/`restoreNode`.

/** A page and its document ancestors (root first), as far up as the caller can view. */
export async function getNodePage(nodeId: string): Promise<PageLineage> {
  const res = await api.get(`api/v1/nodes/${nodeId}/page`);
  if (!res.ok) throw new ApiError(res.status, await res.text());
  return res.json() as Promise<PageLineage>;
}

/** A document's direct, live sub-pages the caller can view, oldest first. */
export async function getNodePages(nodeId: string): Promise<PageSummary[]> {
  const res = await api.get(`api/v1/nodes/${nodeId}/pages`);
  if (!res.ok) throw new ApiError(res.status, await res.text());
  return res.json() as Promise<PageSummary[]>;
}

/** Live documents that link to, or hold as a sub-page, `nodeId` — those the caller can view. */
export async function getNodeBacklinks(nodeId: string): Promise<PageSummary[]> {
  const res = await api.get(`api/v1/nodes/${nodeId}/backlinks`);
  if (!res.ok) throw new ApiError(res.status, await res.text());
  return res.json() as Promise<PageSummary[]>;
}

/** `getNodePage` through a share link: ancestors stop at the link's root. */
export async function getSharePage(token: string, nodeId: string): Promise<PageLineage> {
  const res = await api.get(`s/${token}/page`, { searchParams: { node: nodeId } });
  if (!res.ok) throw new ApiError(res.status, await res.text());
  return res.json() as Promise<PageLineage>;
}

/** `getNodePages` through a share link. */
export async function getSharePages(token: string, nodeId: string): Promise<PageSummary[]> {
  const res = await api.get(`s/${token}/pages`, { searchParams: { node: nodeId } });
  if (!res.ok) throw new ApiError(res.status, await res.text());
  return res.json() as Promise<PageSummary[]>;
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

// ─── Guest sign-in (a one-time code, no account) ─────────────────────────────

/** Whether this instance admits guests: the sign-in page offers a code only then. */
export async function getGuestConfig(): Promise<{ enabled: boolean }> {
  const res = await api.get("auth/guest");
  if (!res.ok) return { enabled: false };
  return res.json() as Promise<{ enabled: boolean }>;
}

/**
 * Asks for a code. The server answers 204 whether or not one was sent, so the
 * form cannot be used to learn who was shared with.
 */
export async function requestGuestCode(email: string): Promise<void> {
  const res = await api.post("auth/guest/code", { json: { email } });
  if (!res.ok) throw new ApiError(res.status, await res.text());
}

/** Checks the code; on success the server has set the guest's session cookie. */
export async function verifyGuestCode(email: string, code: string): Promise<void> {
  const res = await api.post("auth/guest/verify", { json: { email, code } });
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

/** Grant entry points inside one space — where the caller's access begins. */
export async function getShared(spaceId: string): Promise<Node[]> {
  const res = await api.get(`api/v1/shared?space=${spaceId}`);
  if (!res.ok) throw new ApiError(res.status, await res.text());
  return res.json() as Promise<Node[]>;
}

/** Grant entry points in spaces the caller is not a member of. */
export async function getExternalShared(): Promise<Node[]> {
  const res = await api.get("api/v1/shared?scope=external");
  if (!res.ok) throw new ApiError(res.status, await res.text());
  return res.json() as Promise<Node[]>;
}

// ─── Comments and notifications ─────────────────────────────────────────────

/**
 * Every comment thread on a node, resolved ones included. For an extension
 * node `subject` narrows to one part of it ("card:<id>"); without it, all.
 */
export async function getCommentThreads(
  nodeId: string,
  subject?: string,
): Promise<DocCommentThread[]> {
  const res = await api.get(
    `api/v1/nodes/${nodeId}/comments`,
    subject ? { searchParams: { subject } } : undefined,
  );
  if (!res.ok) throw new ApiError(res.status, await res.text());
  const body = (await res.json()) as { threads: DocCommentThread[] };
  return body.threads;
}

/**
 * Starts a thread; the server picks its id, which the caller then anchors in
 * the document. `subject` is required on an extension node, forbidden on a document.
 */
export async function createCommentThread(
  nodeId: string,
  body: string,
  mentions?: string[],
  subject?: string,
): Promise<DocCommentThread> {
  const res = await api.post(`api/v1/nodes/${nodeId}/comment-threads`, {
    json: { body, mentions, subject },
  });
  if (!res.ok) throw new ApiError(res.status, await res.text());
  return res.json() as Promise<DocCommentThread>;
}

export async function replyToThread(
  threadId: string,
  body: string,
  mentions?: string[],
): Promise<DocCommentThread> {
  const res = await api.post(`api/v1/comment-threads/${threadId}/comments`, {
    json: { body, mentions },
  });
  if (!res.ok) throw new ApiError(res.status, await res.text());
  return res.json() as Promise<DocCommentThread>;
}

/** Author only. */
export async function editComment(commentId: string, body: string): Promise<DocComment> {
  const res = await api.patch(`api/v1/comments/${commentId}`, { json: { body } });
  if (!res.ok) throw new ApiError(res.status, await res.text());
  return res.json() as Promise<DocComment>;
}

/** The author, or anyone who can edit the document. The last comment takes its thread with it. */
export async function deleteComment(commentId: string): Promise<void> {
  const res = await api.delete(`api/v1/comments/${commentId}`);
  if (!res.ok) throw new ApiError(res.status, await res.text());
}

export async function setThreadResolved(
  threadId: string,
  resolved: boolean,
): Promise<DocCommentThread> {
  const res = await api.post(
    `api/v1/comment-threads/${threadId}/${resolved ? "resolve" : "reopen"}`,
  );
  if (!res.ok) throw new ApiError(res.status, await res.text());
  return res.json() as Promise<DocCommentThread>;
}

/** Newest first; pass the previous page's `next_cursor` for the next one. */
export async function getNotifications(cursor?: string, limit = 20): Promise<NotificationPage> {
  const searchParams: Record<string, string> = { limit: String(limit) };
  if (cursor) searchParams.cursor = cursor;
  const res = await api.get("api/v1/notifications", { searchParams });
  if (!res.ok) throw new ApiError(res.status, await res.text());
  return res.json() as Promise<NotificationPage>;
}

export async function getUnreadNotificationCount(): Promise<number> {
  const res = await api.get("api/v1/notifications/unread-count");
  if (!res.ok) throw new ApiError(res.status, await res.text());
  const body = (await res.json()) as { count: number };
  return body.count;
}

/**
 * Marks the given notifications read, or every one of them when `ids` is
 * `"all"`. Answers the unread count left.
 */
export async function markNotificationsRead(ids: string[] | "all"): Promise<number> {
  const res = await api.post("api/v1/notifications/read", {
    json: ids === "all" ? { all: true } : { ids },
  });
  if (!res.ok) throw new ApiError(res.status, await res.text());
  const body = (await res.json()) as { count: number };
  return body.count;
}

/** Server-Sent Events: one `notification` event per new item, for the signed-in user. */
export function notificationStreamUrl(): string {
  return "/api/v1/notifications/stream";
}
