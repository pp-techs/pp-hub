// Query keys — single source of truth for cache keys.
export const keys = {
  me: ["me"] as const,
  spaces: ["spaces"] as const,
  children: (spaceId: string, parentId?: string) =>
    ["children", spaceId, parentId ?? null] as const,
  versions: (nodeId: string) => ["versions", nodeId] as const,
  tree: (spaceId: string, parentId?: string) => ["tree", spaceId, parentId ?? null] as const,
  shares: (nodeId: string) => ["shares", nodeId] as const,
  shareInfo: (token: string) => ["share", token] as const,
};
