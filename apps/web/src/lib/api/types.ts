// Shared TypeScript interfaces matching the server API.

/**
 * One server-side session row for the signed-in user: a device that currently
 * holds a live cookie. `current` marks the one making the request, which is
 * the only one the UI must not offer to revoke from under itself.
 */
export interface Session {
  id: string;
  user_agent: string;
  ip: string;
  created_at: string;
  last_used_at: string;
  expires_at: string;
  current: boolean;
}

/**
 * Account-level settings, stored in the `users.settings` JSONB column and
 * carried on the identity response so the language is known before the first
 * paint. Every field is optional; absent means "never chosen", which is what
 * leaves browser detection in charge.
 */
export interface UserPreferences {
  locale?: string;
  email_on_share?: boolean;
}

/** `markdown` writes an empty `.md` that opens in the block editor; the rest are office templates. */
export type DocumentKind = "document" | "spreadsheet" | "presentation" | "markdown";

export interface UserProfile {
  id: string;
  email: string;
  name: string;
  display_name: string;
  avatar_url: string;
  bio: string;
  /** From the identity provider, E.164 when it gave digits. Empty when unset. */
  phone: string;
  /**
   * From the identity provider's OIDC `gender` claim: "female", "male", or
   * whatever free text the IdP holds. Empty when unset.
   */
  gender: string;
  is_admin: boolean;
  /**
   * Whether "new space" is an action this account has at all. False for an
   * ordinary member once the identity provider's organizations own
   * membership: a space is a company, and companies are created there, not
   * here. Hide the control rather than letting it answer 403.
   */
  can_create_spaces: boolean;
  created_at: string;
  last_seen?: string;
  preferences: UserPreferences;
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
  /** The space's picture. Empty when it has none — the UI falls back to initials. */
  avatar_url: string;
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
  /**
   * How many people are in the space's Admin group. Zero is a real state —
   * organization membership is Logto's to decide, so pp-hub can end up
   * mirroring a space nobody administers.
   */
  admin_count: number;
  /** The Logto organization this space mirrors. Absent when unlinked. */
  idp_org_id?: string;
}

/** A space member as the link preview names them. */
export interface SpaceMemberRef {
  user_id: string;
  email: string;
  name: string;
}

/**
 * What linking an unlinked space to a new Logto organization does to its
 * members: `pushable` join the organization with their current role,
 * `stranded` have no account in the Logto tenant and become departures.
 */
export interface LinkPreview {
  pushable: SpaceMemberRef[];
  stranded: SpaceMemberRef[];
}

/**
 * Where pp-hub learned that somebody had left the organization. Mirrors
 * `space_departures.source`.
 */
export type DepartureSource = "signin" | "webhook" | "reconcile" | "admin";

/**
 * Somebody removed from the space's organization whose content has not been
 * handed over yet.
 *
 * Their access ended the moment the removal was seen: the membership row and
 * every group membership are already gone. What is still outstanding is the
 * destructive half — revoking their grants, re-homing what they created under
 * other people's folders and filing the roots they owned into the Archive —
 * which waits for a space admin to confirm.
 */
export interface SpaceDeparture {
  user_id: string;
  email: string;
  name: string;
  display_name: string;
  avatar_url: string;
  /** RFC3339. When the departure was first observed, not when it happened. */
  detected_at: string;
  source: DepartureSource;
  /** Live files they created in this space, still waiting to be handed over. */
  files_owned: number;
}

/**
 * The answer to adding a member by email. `added` means the address already
 * had an account upstream and is a member now; `invited` means the identity
 * provider emailed them an organization invitation, accepted for them the
 * first time they sign in with that address.
 */
export interface AddMemberResult {
  status: "added" | "invited";
  /** Set on `added`. */
  user_id?: string;
  /** Set on `invited`. */
  email?: string;
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

export interface SmtpSettingValue {
  host: string;
  port: number;
  username: string;
  from: string;
  tls: "starttls" | "implicit" | "none";
  password_set: boolean;
}

export interface AdminSetting {
  key: string;
  value: unknown;
  source: "database" | "env" | "default";
  description: string;
  default: unknown;
  type: "string" | "number" | "smtp";
}

export type AdminSettingsMap = Record<string, AdminSetting>;
/**
 * One row of the instance security log.
 *
 * Almost everything is optional because the interesting rows are the ones
 * with no account behind them: a `login_failed` has no `user_id` and no
 * `user_email`, and carries the failure code in `subject` instead. Treat a
 * missing user as ordinary, not as a malformed row.
 */
export interface SecurityEvent {
  id: string;
  kind: string;
  user_id?: string;
  user_email?: string;
  user_name?: string;
  /** Who or what the event was about when no account matched — e.g. the
   *  attempted address, or the reason a sign-in was refused. */
  subject?: string;
  ip?: string;
  user_agent?: string;
  request_id?: string;
  /** The file or folder the event concerns — reads, grants, shares. Survives
   *  the node being purged, so it can point at nothing any more. */
  node_id?: string;
  /** The node's current name, or the one recorded with the event once the
   *  node is gone. */
  node_name?: string;
  /** Per-kind payload the server writes as free-form JSON. Never `any`:
   *  every consumer has to narrow it before reading a field. */
  detail?: unknown;
  created_at: string;
}

export interface SecurityEventPage {
  events: SecurityEvent[];
  total: number;
}

/**
 * What the security log is narrowed by, shared by the console and the CSV
 * export. `from`/`to` are ISO instants and `to` is exclusive; `node` matches
 * that node and everything under it.
 */
export interface SecurityEventFilters {
  kind?: string;
  node?: string;
  from?: string;
  to?: string;
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
  /**
   * What the current user may do to this node, resolved server-side on every
   * listing and single-node GET. See `capsOf` — the UI gates on this rather
   * than guessing, because two rows of one folder can differ.
   */
  caps?: { view: boolean; download: boolean; share: boolean; edit: boolean; owner: boolean };
  /**
   * Whether this node itself carries a live share — direct, group or link,
   * any grantee. Not access: a node reached only through an ancestor's grant
   * is not itself shared, and a node shared with someone else is true even
   * when the caller holds no grant on it. Omitted (falsy) on surfaces that
   * do not resolve caps, e.g. recent/starred/search.
   */
  shared?: boolean;
  /**
   * Live nodes inside this folder that somebody else created. Only resolved on
   * a single-folder GET — a listing would need one subtree walk per row — and
   * only useful to warn before a destructive action: trashing or purging a
   * folder reaches other people's work too.
   */
  foreign_owned?: number;
  /**
   * A folder the team owns rather than a person. `owner` is a byline there:
   * access comes only from grants on the folder, revoking one actually removes
   * access to what the grantee uploaded, and the folder outlives its creator.
   */
  team_folder?: boolean;
  /** Anything inside a team folder, including the folder itself. */
  in_team?: boolean;
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
  /**
   * How many of the caller's own live nodes are buried inside this tombstone.
   */
  buried_own?: number;
  /**
   * True when the caller can see this row *only* because their work is inside
   * it. Somebody else trashed the folder; the row explains where their files
   * went and warns that a purge is coming. Restore and purge are not theirs
   * to run, and the server refuses them.
   */
  read_only?: boolean;
}

/**
 * Somebody (or something) that reaches a node by inheritance: a person, a
 * group, or a share link.
 */
export interface Principal {
  kind: "user" | "group" | "link";
  name: string;
}

/**
 * The access delta a move would cause, asked before it happens. Access is
 * inherited from the folder chain, so moving a file out of a colleague's
 * folder takes it away from everyone that folder reached, and moving it into a
 * shared folder hands it to everyone that one reaches. A delta alone never
 * blocks the move — it is the documented way to narrow sharing — but it
 * should not be silent. The caller themself never appears in either list.
 * A move that is not the caller's to make is refused instead (403, with a
 * sentence saying why); see `MoveDialog`.
 */
export interface MoveImpact {
  losing: Principal[];
  gaining: Principal[];
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
  /**
   * Why this hit matched: its name, or text inside the file (markdown and
   * plain text are indexed). Absent on the lists that reuse this shape.
   */
  match?: "name" | "content";
  /** Content hits only: ~160 characters around the match, `…` already added where cut. */
  snippet?: string;
  /** `[start, end)` code-point offsets into `snippet`, sorted and non-overlapping. */
  snippet_highlights?: [number, number][];
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

/**
 * One slice of the storage breakdown. The slices add up to the space's
 * `used_bytes`. Most are live files by MIME family; two are not:
 * `versions` is the non-current versions of live files (`files` counts those
 * versions) and `trash` is every version of every trashed file (`files`
 * counts the trashed files).
 */
export interface UsageCategory {
  category: "video" | "image" | "audio" | "document" | "other" | "versions" | "trash";
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

/**
 * One row of the `shares` table. `kind` is closed on purpose: the node's
 * access list renders a different line for each, and a widened `string` is
 * what once let a group grant render as the literal word "group".
 *
 * A `group` row names `grantee_group_id`, which this shape does not carry —
 * group access is read through `Grant` instead, so the share list filters
 * these out rather than showing a row it cannot label.
 */
export interface Share {
  id: string;
  node_id?: string;
  kind: "direct" | "link" | "group";
  level: "view" | "edit";
  can_download: boolean;
  can_share: boolean;
  /** Direct shares only: who holds it. The id is what an update posts back. */
  grantee_user_id?: string;
  /** Display name, falling back to name then email. Direct shares only. */
  grantee_name?: string;
  grantee_email?: string;
  url?: string;
  expires_at?: string;
  created_at?: string;
  /**
   * Live nodes inside the shared node that this grantee created. Revoking
   * does not touch them — ownership is intrinsic, there is no row to delete —
   * so the dialog warns before the revoke. Direct shares only.
   */
  grantee_owned?: number;
}

/**
 * What `POST /nodes/{id}/shares` takes. A person is named by user id — the
 * member picker already holds it, and the server resolves it against the
 * space's members, so nobody outside the space can be named. Posting a direct
 * share for someone who already holds one on the node updates it in place.
 *
 * A link is spelled out in full because the database insists on its shape:
 * view level, no re-sharing, download the one choice. `expires_in` is in
 * seconds, 0 for never.
 */
export type CreateShareBody =
  | ({ kind: "direct"; user_id: string } & GrantCaps)
  | { kind: "link"; level: "view"; can_download: boolean; can_share: false; expires_in: number };

/**
 * Someone reaching a node from above it, as `GET /nodes/{id}/inherited-access`
 * reports: the owners — of the node itself, or of a folder over it — and every
 * grant on an ancestor. Grants on the node itself are not here; those are the
 * node's own `Share`/`Grant` rows, the ones this node's dialog can change.
 *
 * Rows arrive deduplicated and ordered nearest-first, which is the order the
 * dialog lists them in.
 */
export interface InheritedAccess {
  kind: "owner" | "user" | "group" | "link";
  principal_id: string;
  /** Display name. An untranslated English literal for `link`: render a label instead. */
  name: string;
  /** Where the access is held: the node itself for its own owner, else a folder above. */
  via_id: string;
  via_name: string;
  level: "view" | "edit";
  can_download: boolean;
  can_share: boolean;
}

export interface EditSession {
  editor_url: string;
  token: string;
  access_token_ttl: number;
  /**
   * Whether this session may save. A reader gets a session too — OnlyOffice
   * is the only thing here that renders an office document — but it opens in
   * the viewer and the WOPI write calls refuse its token.
   */
  can_write: boolean;
}

/**
 * `GET /nodes/{id}/collab`: where the block editor connects for a markdown
 * document. `room` carries the epoch (`<nodeId>.<epoch>`) — a new one is
 * minted each time the server loads the document, and a client holding a
 * Y.Doc from an older room must discard it rather than merge it in.
 */
export interface CollabSession {
  room: string;
  /** Edit capability, re-read on every reconnect. The server drops a reader's writes. */
  can_write: boolean;
  /** The markdown holds constructs the block editor cannot write back unchanged. */
  lossy: boolean;
}

/** One node of a ProseMirror document as JSON — the shape `mdoc.Parse` emits and Tiptap reads. */
export interface DocumentNodeJSON {
  type: string;
  attrs?: Record<string, unknown>;
  content?: DocumentNodeJSON[];
  marks?: { type: string; attrs?: Record<string, unknown> }[];
  text?: string;
}

/**
 * `GET /nodes/{id}/document` (and `/s/{token}/document`): a markdown file
 * parsed on the server, for rendering without joining the collab room.
 * `version` is the version number the document was read from.
 */
export interface MarkdownDocument {
  doc: DocumentNodeJSON;
  lossy: boolean;
  version: number;
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
  /**
   * Display name of whoever created the link. Absent when they have none:
   * the address behind the account is not something an anonymous visitor
   * is told.
   */
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

// ─── Comments and notifications ─────────────────────────────────────────────

/** Who wrote a comment or caused a notification. */
export interface SocialUser {
  id: string;
  name: string;
  avatar_url: string;
}

/** One message in a comment thread. */
export interface DocComment {
  id: string;
  author: SocialUser;
  body: string;
  created_at: string;
  edited_at?: string;
}

/**
 * A discussion anchored in a markdown document by a `comment` mark carrying
 * its id. The mark lives in the document; everything else lives here.
 */
export interface DocCommentThread {
  id: string;
  resolved: boolean;
  resolved_at?: string;
  resolved_by?: SocialUser;
  created_at: string;
  created_by: SocialUser;
  comments: DocComment[];
}

export type NotificationKind = "comment" | "reply" | "mention";

export interface AppNotification {
  id: string;
  kind: NotificationKind;
  actor: SocialUser;
  node: { id: string; name: string; space_id: string };
  thread_id?: string;
  comment_id?: string;
  excerpt: string;
  created_at: string;
  read_at?: string;
}

export interface NotificationPage {
  items: AppNotification[];
  next_cursor?: string;
}

/** `GET /templates/markdown`: a starting point for a new markdown document, in the caller's locale. */
export interface MarkdownTemplate {
  id: string;
  name: string;
  description: string;
}

/**
 * `POST /nodes/{id}/unfurl`: what the server read from a link for a bookmark
 * card. `thumbnail` is already stored as an attachment of the document
 * (`attachment:<hash>`) — the card never hotlinks another site's image.
 */
export interface UnfurledLink {
  url: string;
  title: string;
  description: string;
  thumbnail?: string;
}
