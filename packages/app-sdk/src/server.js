// @pp-hub/app-sdk/server — verify the token pp-hub signs for an app's backend (HS256 JWT).
// Node ≥ 18, node:crypto only. Types: server.d.ts beside this file.
import { createHmac, timingSafeEqual } from "node:crypto";

/** A token that is malformed, badly signed, expired or meant for another app. */
export class TokenError extends Error {
  /** @param {string} message */
  constructor(message) {
    super(message);
    this.name = "TokenError";
  }
}

/** @param {string} part */
function decodeJSON(part, what) {
  try {
    const value = JSON.parse(Buffer.from(part, "base64url").toString("utf8"));
    if (value && typeof value === "object" && !Array.isArray(value)) return value;
  } catch {
    // fall through
  }
  throw new TokenError(`token ${what} is not a base64url JSON object`);
}

/**
 * Verifies an HS256 token signed with the app's backend secret and returns its claims.
 * @param {string} token
 * @param {string} secret
 * @param {{ appId?: string, now?: number }} [opts]
 */
export function verifyToken(token, secret, opts = {}) {
  if (typeof token !== "string" || !token) throw new TokenError("no token");
  if (typeof secret !== "string" || !secret) throw new TokenError("no secret");
  const parts = token.split(".");
  if (parts.length !== 3) throw new TokenError("token is not a JWT");
  const [h, p, s] = parts;
  const header = decodeJSON(h, "header");
  if (header.alg !== "HS256") throw new TokenError(`token alg must be HS256, not ${header.alg}`);
  if (header.typ !== undefined && header.typ !== "JWT")
    throw new TokenError(`token typ must be JWT, not ${header.typ}`);

  const want = createHmac("sha256", secret).update(`${h}.${p}`).digest();
  const got = Buffer.from(s, "base64url");
  if (got.length !== want.length || !timingSafeEqual(got, want))
    throw new TokenError("token signature does not match");

  const claims = decodeJSON(p, "payload");
  const now = opts.now ?? Math.floor(Date.now() / 1000);
  if (typeof claims.exp !== "number") throw new TokenError("token has no exp");
  if (now >= claims.exp) throw new TokenError("token expired");
  if (opts.appId !== undefined && claims.aud !== `pp-app:${opts.appId}`)
    throw new TokenError(
      `token audience ${JSON.stringify(claims.aud)} is not pp-app:${opts.appId}`,
    );
  return claims;
}

/**
 * The bearer token of a request's `Authorization` header, or null.
 * @param {Headers | Record<string, string | string[] | undefined>} headers
 */
export function tokenFromRequest(headers) {
  if (!headers) return null;
  let value;
  if (typeof headers.get === "function") value = headers.get("authorization");
  else {
    for (const [k, v] of Object.entries(headers)) {
      if (k.toLowerCase() === "authorization") {
        value = Array.isArray(v) ? v[0] : v;
        break;
      }
    }
  }
  const m = typeof value === "string" ? /^Bearer\s+(\S+)\s*$/i.exec(value) : null;
  return m ? m[1] : null;
}
