// Re-export all query hooks for convenience, plus the plain options factories
// (`childrenOptions`, `treeOptions`, `versionsOptions`) for route loaders.

export { keys } from "./keys";

// Auth
export { useMe, requireAuth, fetchMe, signOut } from "./auth";

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
  childrenOptions,
  nodeOptions,
  treeOptions,
  versionsOptions,
  useChildren,
  useNode,
  useInvalidateChildren,
  useCreateFolder,
  useCreateTeamFolder,
  useUpload,
  useRenameNode,
  useDeleteNode,
  useMoveNode,
  useMoveNodes,
  useMoveImpact,
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

// Search
export { useNodeSearch, MIN_QUERY_LENGTH } from "./search";

// Space settings + admin
export {
  useUpdateSpace,
  useDeleteSpace,
  adminStatsOptions,
  useAdminStats,
  useUpdateAdminUser,
  adminSettingsOptions,
  useAdminSettings,
  useUpdateAdminSettings,
  useDeleteAdminSetting,
} from "./spaces-admin";

// Home surfaces
export { useRecent, useStarred, useActivity, useUsage, useToggleStar } from "./home";

// Shares
export {
  sharesOptions,
  shareInfoOptions,
  useShares,
  useShareInfo,
  useCreateShare,
  useRevokeShare,
  useShareChildren,
} from "./shares";

// Users
export {
  usersOptions,
  useUpdateProfile,
  useUploadAvatar,
  useSessions,
  useRevokeSession,
  useRevokeOtherSessions,
  useUsers,
  useSecurityEvents,
  SECURITY_PAGE_SIZE,
  useSpaceMembers,
  useAddSpaceMember,
  useSetMemberAdmin,
  useRemoveSpaceMember,
  useSpaceDepartures,
  useCompleteDeparture,
} from "./users";
