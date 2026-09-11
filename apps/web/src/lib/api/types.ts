// Shared TypeScript interfaces matching the server API.

export interface Me {
  id: string;
  name: string;
  email: string;
}

export interface Space {
  id: string;
  name: string;
  role: string;
  used_bytes: number;
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
}
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
  kind: string;
  perm: string;
  grantee_email?: string;
  url?: string;
}

export interface EditSession {
  editor_url: string;
  token: string;
}

export interface ShareInfo {
  node: Node;
  perm: string;
}

// Re-export ApiError for convenience
export { ApiError } from "./client";
