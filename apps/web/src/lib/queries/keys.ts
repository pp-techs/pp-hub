// Query keys — single source of truth for cache keys.
export const keys = {
  me: ["me"] as const,
  profile: ["profile"] as const,
  spaces: ["spaces"] as const,
  children: (spaceId: string, parentId?: string) =>
    ["children", spaceId, parentId ?? null] as const,
  node: (nodeId: string) => ["node", nodeId] as const,
  versions: (nodeId: string) => ["versions", nodeId] as const,
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
  userSearch: ["users", "search"] as const,
  members: (spaceId: string) => ["spaces", spaceId, "members"] as const,
  invitations: (spaceId: string) => ["spaces", spaceId, "invitations"] as const,
  groups: (spaceId: string) => ["spaces", spaceId, "groups"] as const,
  groupMembers: (spaceId: string, groupId: string) =>
    ["spaces", spaceId, "groups", groupId, "members"] as const,
  grants: (nodeId: string) => ["grants", nodeId] as const,
  adminStats: ["admin", "stats"] as const,
};
