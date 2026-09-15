// Groups and the grants that give them access to folders.
import { api, ApiError } from "./client";
import type { Grant, Group, GroupMember, SharePerm } from "./types";

async function problem(res: Response, fallback: string): Promise<never> {
  // The server answers RFC 7807, and its `detail` is the sentence worth
  // showing — "the Everyone group is maintained automatically" beats "Failed".
  let detail = "";
  try {
    const body = (await res.json()) as { detail?: string };
    detail = body.detail ?? "";
  } catch {
    // Not JSON; fall through to the generic message.
  }
  throw new ApiError(res.status, detail || fallback);
}

export async function getGroups(spaceId: string): Promise<Group[]> {
  const res = await api.get(`api/v1/spaces/${spaceId}/groups`);
  if (!res.ok) return problem(res, "Could not load groups");
  return res.json();
}

export async function createGroup(
  spaceId: string,
  body: { name: string; description?: string },
): Promise<Group> {
  const res = await api.post(`api/v1/spaces/${spaceId}/groups`, { json: body });
  if (!res.ok) return problem(res, "Could not create the group");
  return res.json();
}

export async function updateGroup(
  spaceId: string,
  groupId: string,
  body: { name: string; description?: string },
): Promise<Group> {
  const res = await api.patch(`api/v1/spaces/${spaceId}/groups/${groupId}`, { json: body });
  if (!res.ok) return problem(res, "Could not rename the group");
  return res.json();
}

export async function deleteGroup(
  spaceId: string,
  groupId: string,
): Promise<{ grants_revoked: number }> {
  const res = await api.delete(`api/v1/spaces/${spaceId}/groups/${groupId}`);
  if (!res.ok) return problem(res, "Could not delete the group");
  return res.json();
}

export async function getGroupMembers(spaceId: string, groupId: string): Promise<GroupMember[]> {
  const res = await api.get(`api/v1/spaces/${spaceId}/groups/${groupId}/members`);
  if (!res.ok) return problem(res, "Could not load the group's members");
  return res.json();
}

export async function addGroupMember(
  spaceId: string,
  groupId: string,
  userId: string,
): Promise<void> {
  const res = await api.post(`api/v1/spaces/${spaceId}/groups/${groupId}/members`, {
    json: { user_id: userId },
  });
  if (!res.ok) await problem(res, "Could not add them to the group");
}

export async function removeGroupMember(
  spaceId: string,
  groupId: string,
  userId: string,
): Promise<void> {
  const res = await api.delete(`api/v1/spaces/${spaceId}/groups/${groupId}/members/${userId}`);
  if (!res.ok) await problem(res, "Could not remove them from the group");
}

// --- Grants on a folder ---

export async function getGrants(nodeId: string): Promise<Grant[]> {
  const res = await api.get(`api/v1/nodes/${nodeId}/grants`);
  if (!res.ok) return problem(res, "Could not load who has access");
  return res.json();
}

export async function upsertGrant(nodeId: string, groupId: string, perm: SharePerm): Promise<void> {
  const res = await api.put(`api/v1/nodes/${nodeId}/grants`, {
    json: { group_id: groupId, perm },
  });
  if (!res.ok) await problem(res, "Could not grant access");
}

export async function revokeGrant(nodeId: string, groupId: string): Promise<void> {
  const res = await api.delete(`api/v1/nodes/${nodeId}/grants/${groupId}`);
  if (!res.ok) await problem(res, "Could not revoke access");
}
