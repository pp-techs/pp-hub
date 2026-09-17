// Shared TypeScript interfaces matching the server API.

export interface Me {
  id: string;
  name: string;
  email: string;
}

export interface UserProfile {
  id: string;
  email: string;
  name: string;
  display_name: string;
  avatar_url: string;
  bio: string;
  is_admin: boolean;
  created_at: string;
  last_seen?: string;
}

export interface UserSearchResult {
  id: string;
  email: string;
  name: string;
  display_name: string;
  avatar_url: string;
}

export interface SpaceMember {
  user_id: string;
  email: string;
  name: string;
  display_name?: string;
  avatar_url: string;
  /** Whether this member is a space admin (member of the Admin group). */
  is_admin: boolean;
  created_at: string;
  /** Live files this member created in this space. */
  files_owned: number;
  last_seen?: string;
}

export interface Space {
  id: string;
  name: string;
  is_admin: boolean;
  used_bytes: number;
  /** Byte allowance for the space. 0 means unlimited. */
  quota_bytes: number;
  slug?: string;
  description?: string;
  /** Promote new members to admin by default. */
  default_as_admin: boolean;
  /** ID of the archive folder root, if one exists. */
  archive_root_id?: string;
  /** Days to keep trashed items. 0 keeps them indefinitely. */
  trash_retention_days?: number;
}

/**
 * A promise of membership to an email address that may not have an account
 * yet — which is why it cannot live in space_members.
 */
export interface Invitation {
  id: string;
  space_id: string;
  email: string;
  /** Whether this invitation grants admin membership. */
  as_admin: boolean;
  invited_by: string;
  expires_at?: string;
  created_at: string;
  /** Returned once, at creation. There is no mail transport yet. */
  url?: string;
}
/** Creating an invitation for an existing account adds them directly. */
export interface InviteResult {
  added_member?: boolean;
  email?: string;
  id?: string;
  url?: string;
  as_admin?: boolean;
}

export interface AdminStats {
  users: number;
  suspended_users: number;
  spaces: number;
  files: number;
  trashed: number;
  storage_bytes: number;
  jobs_pending: number;
  jobs_failed: number;
}

export interface AdminUser {
  id: string;
  email: string;
  name: string;
  display_name: string;
  avatar_url: string;
  is_admin: boolean;
  suspended: boolean;
  spaces: number;
  last_seen?: string;
  created_at: string;
}

export interface Node {
  id: string;
  space_id: string;
  parent_id?: string;
  kind: "dir" | "file";
  name: string;
  size?: number;
  mime?: string;
  version?: number;
  modified_at?: string; // ISO timestamp: latest version.CreatedAt (file) or node.CreatedAt (folder)
  owner?: string; // display name of creator
  owner_id?: string; // user id for avatar initials
  /** Access capabilities for the current user on this node. */
  caps?: { view: boolean; download: boolean; share: boolean; edit: boolean; owner: boolean };
}

/**
 * A node listed in the trash. Carries the three facts a restore decision needs
 * that a live node does not: when it was deleted, where it came from, and how
 * much comes back with it.
 */
export interface TrashNode extends Node {
  deleted_at: string;
  /** Who trashed it. Empty for items deleted before this was recorded. */
  deleted_by?: string;
  /** When automatic purging removes it. Empty when the space keeps trash. */
  purges_at?: string;
  /** Ancestor folder names joined with " / ". Empty at the top of a space. */
  location: string;
  /** Direct children, for folders. Files report 0. */
  child_count: number;
}

/**
 * Restoring is not always a straight undo — the original folder may itself be
 * in the trash, and a live sibling may have taken the name. The server makes
 * both adjustments and reports them so the UI can say where the node went.
 */
export interface RestoreResult {
  node: Node;
  relocated: boolean;
  renamed_from?: string;
}

/**
 * A search hit. Results span spaces, so a name alone does not identify a file —
 * the space and the holding folder travel with it.
 */
export interface SearchResult extends Node {
  space_name: string;
  /** Immediate parent folder; empty at the top level of a space. */
  parent_name: string;
}

/** A file the caller themselves last touched, with what they did to it. */
export interface RecentNode extends SearchResult {
  last_action: ActivityKind;
  last_action_at: string;
}

/** The verbs the activity feed records. */
export type ActivityKind =
  | "uploaded"
  | "created"
  | "renamed"
  | "moved"
  | "trashed"
  | "restored"
  | "purged"
  | "shared"
  | "unshared"
  | "reverted"
  | "invited"
  | "joined";

/**
 * One line of the activity feed. The actor name and node name are stored with
 * the event, so an entry still reads correctly after a rename or a purge.
 */
export interface ActivityEvent {
  id: string;
  space_id: string;
  space_name: string;
  actor: string;
  actor_id: string;
  node_id?: string;
  node_name?: string;
  kind: ActivityKind;
  detail?: Record<string, unknown>;
  created_at: string;
}

/** One slice of the storage breakdown. */
export interface UsageCategory {
  category: "video" | "image" | "audio" | "document" | "other";
  bytes: number;
  files: number;
}

/** Narrows a search to folders or files. Absent means both. */
export type SearchKind = "dir" | "file";

export type NodeView = "grid" | "list";

export interface TreeNode {
  id: string;
  parent_id?: string;
  kind: "dir" | "file";
  name: string;
  has_children: boolean;
}
export interface Version {
  id: string;
  version_no: number;
  size: number;
  mime: string;
  created_at: string;
}

export interface Share {
  id: string;
  node_id?: string;
  kind: string;
  level: "view" | "edit";
  can_download: boolean;
  can_share: boolean;
  grantee_email?: string;
  url?: string;
  expires_at?: string;
  created_at?: string;
}

export interface EditSession {
  editor_url: string;
  token: string;
  access_token_ttl: number;
}

/**
 * What an anonymous visitor is told about the link itself. A link is always
 * view level (edit-via-link is post-MVP); `can_download` is the only choice
 * the sharer makes.
 */
export interface ShareInfo {
  node: Node;
  level: "view";
  can_download: boolean;
  /** Display name of whoever created the link. */
  shared_by?: string;
  expires_at?: string;
}

/**
 * What a share or a grant hands over, exactly as the server stores it: a level
 * plus two independent flags. `can_download` is a choice on a viewer and is
 * implied by `edit` (the database requires it — an editor can always fetch what
 * it edits); `can_share` is independent of level, so a viewer may be allowed to
 * pass their view on and an editor may be denied re-sharing.
 */
export interface GrantCaps {
  level: "view" | "edit";
  can_download: boolean;
  can_share: boolean;
}

// Re-export ApiError for convenience
export { ApiError } from "./client";

/**
 * A named set of people inside one space, and the unit access is granted to.
 *
 * `is_everyone` marks the automatic group holding every member: the server
 * maintains its membership, and it cannot be renamed, deleted, or edited by
 * hand. It is how someone granted nothing individually reaches anything.
 *
 * `is_admin` marks the automatic group holding space admins: membership is
 * managed via the members panel, not here.
 */
export interface Group {
  id: string;
  space_id: string;
  name: string;
  description: string;
  is_everyone: boolean;
  is_admin: boolean;
  member_count: number;
  /** How many folders this group opens — what deleting it would take away. */
  grant_count: number;
  created_at: string;
}

export interface GroupMember {
  user_id: string;
  email: string;
  name: string;
  display_name?: string;
  avatar_url?: string;
  added_at: string;
}

/**
 * A group's standing access to one folder.
 *
 * `level` is the access tier. `can_download` and `can_share` are optional flags.
 * `can_download` is only meaningful with level="view".
 * `can_share` is only meaningful with level="edit".
 */
export interface Grant {
  id: string;
  group_id: string;
  group_name: string;
  is_admin: boolean;
  is_everyone: boolean;
  level: "view" | "edit";
  can_download: boolean;
  can_share: boolean;
}
