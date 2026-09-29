import type { SecurityEventFilters } from "@/lib/api";

// Query keys — single source of truth for cache keys.
export const keys = {
  // The signed-in user's own profile — one identity cache, read by the rail,
  // the share dialog and the route guards alike.
  me: ["me"] as const,
  sessions: ["sessions"] as const,
  spaces: ["spaces"] as const,
  children: (spaceId: string, parentId?: string) =>
    ["children", spaceId, parentId ?? null] as const,
  node: (nodeId: string) => ["node", nodeId] as const,
  versions: (nodeId: string) => ["versions", nodeId] as const,
  collab: (nodeId: string) => ["collab", nodeId] as const,
  // A markdown file parsed for read-only rendering. `null` is the current
  // version, which moves with every write-back; a past version never changes.
  document: (nodeId: string, versionId?: string) =>
    ["document", nodeId, versionId ?? null] as const,
  shareDocument: (token: string, nodeId: string) => ["share-document", token, nodeId] as const,
  // Named in the UI's language, so the locale is part of the key.
  markdownTemplates: (locale: string) => ["templates", "markdown", locale] as const,
  tree: (spaceId: string, parentId?: string) => ["tree", spaceId, parentId ?? null] as const,
  search: (query: string, spaceId?: string, kind?: string, ownerId?: string, modified?: number) =>
    ["search", query, spaceId ?? null, kind ?? null, ownerId ?? null, modified ?? null] as const,
  recent: (spaceId: string, limit?: number) => ["recent", spaceId, limit ?? null] as const,
  starred: (spaceId: string, limit?: number) => ["starred", spaceId, limit ?? null] as const,
  activity: (spaceId: string, limit?: number) => ["activity", spaceId, limit ?? null] as const,
  usage: (spaceId: string) => ["usage", spaceId] as const,
  trash: (spaceId: string) => ["trash", spaceId] as const,
  shared: (spaceId: string) => ["shared", spaceId] as const,
  shares: (nodeId: string) => ["shares", nodeId] as const,
  shareInfo: (token: string) => ["share", token] as const,
  users: ["users"] as const,
  // People/group caches live under their own roots, NOT under "spaces":
  // invalidating keys.spaces must not wipe them (prefix matching).
  members: (spaceId: string) => ["members", spaceId] as const,
  departures: (spaceId: string) => ["departures", spaceId] as const,
  linkPreview: (spaceId: string) => ["link-preview", spaceId] as const,
  groups: (spaceId: string) => ["groups", spaceId] as const,
  groupMembers: (spaceId: string, groupId: string) => ["groups", spaceId, groupId] as const,
  grants: (nodeId: string) => ["grants", nodeId] as const,
  // Its own root: a grant on a folder changes the inherited access of every
  // node below it, which the client cannot enumerate, so a change invalidates
  // the whole root rather than one node's entry.
  inheritedAccess: (nodeId: string) => ["inherited-access", nodeId] as const,
  adminStats: ["admin", "stats"] as const,
  // Sibling of adminStats under the "admin" root, never a child of it: the
  // user mutations invalidate `adminStats` exactly, and a log page hanging
  // below that key would be refetched by every suspend/promote for nothing.
  securityEvents: (filters: SecurityEventFilters, offset: number) =>
    [
      "admin",
      "security-events",
      filters.kind ?? null,
      filters.node ?? null,
      filters.from ?? null,
      filters.to ?? null,
      offset,
    ] as const,
  adminSettings: ["admin", "settings"] as const,
  // Comment threads of one markdown document.
  comments: (nodeId: string) => ["comments", nodeId] as const,
  // The inbox: the list pages and the unread badge share the root, so one
  // invalidation after "mark read" or a live event refreshes both.
  notifications: ["notifications"] as const,
  notificationList: ["notifications", "list"] as const,
  notificationUnread: ["notifications", "unread"] as const,
};
