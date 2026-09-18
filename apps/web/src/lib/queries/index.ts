// Re-export all query hooks for convenience.
// Also exports Options factories with hook-like names for backward compatibility
// with old single-file pattern.

export { keys } from "./keys";

// Auth
export { useMe, requireAuth, fetchMe, logout } from "./auth";

// Spaces
export { useSpaces, useSpace, fetchSpaces, useCreateSpace } from "./spaces";

// Groups and folder grants
export {
  useGroups,
  useGroupMembers,
  useGroupMemberships,
  useCreateGroup,
  useUpdateGroup,
  useDeleteGroup,
  useAddGroupMember,
  useRemoveGroupMember,
  useGrants,
  useUpsertGrant,
  useRevokeGrant,
} from "./groups";

// Nodes
export {
  useNode,
  useInvalidateChildren,
  useCreateFolder,
  useUpload,
  useRenameNode,
  useDeleteNode,
  useMoveNode,
  useCopyNode,
  useRestoreNode,
  usePurgeNode,
  useEmptyTrash,
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

// Search
export { useNodeSearch, MIN_QUERY_LENGTH } from "./search";

// Space settings + admin
export {
  useUpdateSpace,
  useDeleteSpace,
  useInvitations,
  useCreateInvitation,
  useRevokeInvitation,
  useAdminStats,
  useUpdateAdminUser,
} from "./spaces-admin";

// Home surfaces
export { useRecent, useStarred, useActivity, useUsage, useToggleStar } from "./home";

// Shares
export {
  useShares,
  useShareInfo,
  useCreateShare,
  useRevokeShare,
  useShareChildren,
} from "./shares";

// Users
export {
  useMyProfile,
  useUpdateProfile,
  useUploadAvatar,
  useUserSearch,
  useUsers,
  useSpaceMembers,
  useAddSpaceMember,
  useSetMemberAdmin,
  useRemoveSpaceMember,
} from "./users";
