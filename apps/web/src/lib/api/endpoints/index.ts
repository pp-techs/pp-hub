// API endpoints — typed functions that call the ky client.
// Each file covers one resource domain.

import { api, ApiError } from "../client";
import type {
  ActivityEvent,
  AdminSettingsMap,
  AdminStats,
  AdminUser,
  CollabSession,
  CreateShareBody,
  DocComment,
  DocCommentThread,
  DocumentKind,
  EditSession,
  InheritedAccess,
  MarkdownDocument,
  MarkdownTemplate,
  MoveImpact,
  Node,
  NotificationPage,
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

// ─── Comments and notifications ─────────────────────────────────────────────

/** Every comment thread on a markdown document, resolved ones included. */
export async function getCommentThreads(nodeId: string): Promise<DocCommentThread[]> {
  const res = await api.get(`api/v1/nodes/${nodeId}/comments`);
  if (!res.ok) throw new ApiError(res.status, await res.text());
  const body = (await res.json()) as { threads: DocCommentThread[] };
  return body.threads;
}

/** Starts a thread; the server picks its id, which the caller then anchors in the document. */
export async function createCommentThread(
  nodeId: string,
  body: string,
  mentions?: string[],
): Promise<DocCommentThread> {
  const res = await api.post(`api/v1/nodes/${nodeId}/comment-threads`, {
    json: { body, mentions },
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
