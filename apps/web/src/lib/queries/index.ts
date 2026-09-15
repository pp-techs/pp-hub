// Re-export all query hooks for convenience.
// Also exports Options factories with hook-like names for backward compatibility
// with old single-file pattern.

export { keys } from "./keys";

// Auth
export { useMe, requireAuth, fetchMe, logout } from "./auth";

// Spaces
export { useSpaces, useCreateSpace } from "./spaces";

// Nodes
export {
  useInvalidateChildren,
  useCreateFolder,
  useUpload,
  useRenameNode,
  useDeleteNode,
  useVersions,
  useRestoreVersion,
  useTree,
  useTrash,
  useShared,
} from "./nodes";

// Backward compat: old components call these as hooks returning query state
import { useMe } from "./auth";
import { useSpaces } from "./spaces";
import { useChildren, useVersions } from "./nodes";
export { useMe as meOptions, useSpaces as spacesOptions };
export const childrenOptions = (spaceId: string, parentId?: string) =>
  useChildren(spaceId, parentId);
export const versionsOptions = (nodeId: string | null) => useVersions(nodeId);

// Shares
export { useShares, useCreateShare, useRevokeShare } from "./shares";

// Backward compat
import { useShares, useShareInfo } from "./shares";
export const sharesOptions = (nodeId: string | null) => useShares(nodeId);
export const shareInfoOptions = (token: string) => useShareInfo(token);

// Users
export {
  useMyProfile,
  useUpdateProfile,
  useUploadAvatar,
  useUserSearch,
  useUsers,
  useSpaceMembers,
  useAddSpaceMember,
  useUpdateMemberRole,
  useRemoveSpaceMember,
} from "./users";
