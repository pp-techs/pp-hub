// User management API endpoints.
import { api, ApiError } from "./client";
import type {
  AdminUser,
  SecurityEventPage,
  Session,
  SpaceMember,
  UserProfile,
  UserSearchResult,
} from "./types";

// ─── Profile ─────────────────────────────────────────────────────────────────

export async function getMyProfile(): Promise<UserProfile> {
  const res = await api.get("api/v1/users/me");
  if (!res.ok) throw new ApiError(res.status, "Failed to get profile");
  return res.json();
}

export async function updateMyProfile(body: {
  display_name?: string;
  bio?: string;
}): Promise<UserProfile> {
  const res = await api.put("api/v1/users/me", { json: body });
  if (!res.ok) throw new ApiError(res.status, "Failed to update profile");
  return res.json();
}

export async function uploadAvatar(file: File): Promise<{ avatar_url: string }> {
  const form = new FormData();
  form.append("avatar", file);
  const res = await api.post("api/v1/users/me/avatar", { body: form });
  if (!res.ok) throw new ApiError(res.status, "Failed to upload avatar");
  return res.json();
}

// ─── Sessions ────────────────────────────────────────────────────────────────

/** Every live session of the caller, newest use first, current one flagged. */
export async function listSessions(): Promise<Session[]> {
  const res = await api.get("api/v1/users/me/sessions");
  if (!res.ok) throw new ApiError(res.status, "Failed to list sessions");
  return res.json();
}

export async function revokeSession(id: string): Promise<void> {
  const res = await api.delete(`api/v1/users/me/sessions/${id}`);
  if (!res.ok) throw new ApiError(res.status, "Failed to revoke session");
}

/** Ends every session but the caller's own — the "lost laptop" button. */
export async function revokeOtherSessions(): Promise<void> {
  const res = await api.delete("api/v1/users/me/sessions");
  if (!res.ok) throw new ApiError(res.status, "Failed to revoke sessions");
}

// ─── User Search ─────────────────────────────────────────────────────────────

export async function searchUsers(q: string, limit = 20): Promise<UserSearchResult[]> {
  const res = await api.get("api/v1/users/search", { searchParams: { q, limit } });
  if (!res.ok) throw new ApiError(res.status, "Search failed");
  return res.json();
}

// ─── Admin: List Users ───────────────────────────────────────────────────────

/**
 * Admin-only listing. Returns the admin shape rather than the public profile:
 * it carries the suspension flag and a space count, which a profile
 * deliberately does not.
 */
export async function listUsers(
  limit = 50,
  offset = 0,
): Promise<{ users: AdminUser[]; total: number }> {
  const res = await api.get("api/v1/users", { searchParams: { limit, offset } });
  if (!res.ok) throw new ApiError(res.status, "Failed to list users");
  return res.json();
}

// ─── Admin: Security Log ─────────────────────────────────────────────────────

/**
 * Admin-only, newest first, instance-wide. `kind` filters exactly; omit it for
 * every kind. The server clamps `limit` to 1–500 and defaults it to 100.
 */
export async function listSecurityEvents(params: {
  limit?: number;
  offset?: number;
  kind?: string;
}): Promise<SecurityEventPage> {
  const searchParams: Record<string, string | number> = {
    limit: params.limit ?? 100,
    offset: params.offset ?? 0,
  };
  if (params.kind) searchParams.kind = params.kind;
  const res = await api.get("api/v1/admin/security-events", { searchParams });
  if (!res.ok) throw new ApiError(res.status, "Failed to list security events");
  return res.json();
}

// ─── Space Members ───────────────────────────────────────────────────────────

export async function getSpaceMembers(spaceId: string): Promise<SpaceMember[]> {
  const res = await api.get(`api/v1/spaces/${spaceId}/members`);
  if (!res.ok) throw new ApiError(res.status, "Failed to get members");
  return res.json();
}

export async function addSpaceMember(
  spaceId: string,
  body: { user_id: string; as_admin: boolean },
): Promise<void> {
  const res = await api.post(`api/v1/spaces/${spaceId}/members`, { json: body });
  if (!res.ok) {
    const text = await res.text();
    throw new ApiError(res.status, text || "Failed to add member");
  }
}

export async function setMemberAdmin(
  spaceId: string,
  userId: string,
  as_admin: boolean,
): Promise<void> {
  const res = await api.post(`api/v1/spaces/${spaceId}/members/${userId}/set-admin`, {
    json: { as_admin },
  });
  if (!res.ok) throw new ApiError(res.status, "Failed to update admin status");
}

export async function removeSpaceMember(spaceId: string, userId: string): Promise<void> {
  const res = await api.delete(`api/v1/spaces/${spaceId}/members/${userId}`);
  if (!res.ok) throw new ApiError(res.status, "Failed to remove member");
}
