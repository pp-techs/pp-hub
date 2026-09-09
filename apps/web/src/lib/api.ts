// Typed API client for the pp-hub server (same-origin httpOnly cookie session).
// The SPA never stores tokens; 401 responses drive the auth guard in the router.

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

export class ApiError extends Error {
  status: number;

  constructor(status: number, message: string) {
    super(message);
    this.name = "ApiError";
    this.status = status;
  }
}

export async function api<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(path, init);
  if (!res.ok) {
    const body = await res.text().catch(() => "");
    throw new ApiError(res.status, `${res.status}: ${body}`);
  }
  return (await res.json()) as T;
}

export function postJson(data: unknown): RequestInit {
  return {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify(data),
  };
}

export function patchJson(data: unknown): RequestInit {
  return {
    method: "PATCH",
    headers: { "content-type": "application/json" },
    body: JSON.stringify(data),
  };
}

export function fmtSize(n: number): string {
  if (n < 1024) return `${n} B`;
  if (n < 1 << 20) return `${(n / 1024).toFixed(1)} KB`;
  return `${(n / (1 << 20)).toFixed(1)} MB`;
}

const OFFICE_RE = /\.(docx|xlsx|pptx|doc|xls|ppt|odt|ods|odp)$/i;

export function isOfficeFile(name: string): boolean {
  return OFFICE_RE.test(name);
}

export type PreviewKind = "thumbnail" | "pdf" | "video" | "text";

export function previewKind(mime?: string): PreviewKind {
  if (mime?.startsWith("image/")) return "thumbnail";
  if (mime === "application/pdf") return "pdf";
  if (mime?.startsWith("video/")) return "video";
  return "text";
}
