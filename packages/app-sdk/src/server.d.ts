// Types of @pp-hub/app-sdk/server (server.js beside this file).

/** The claims pp-hub puts in the token it sends an app's backend. */
export interface TokenClaims {
  /** PP_EXTERNAL_URL of the instance. */
  iss: string;
  /** "pp-app:<app id>". */
  aud: string;
  /** The user id the request is made for. */
  sub: string;
  /** Issued at, seconds since the epoch. */
  iat: number;
  /** Expiry, seconds since the epoch (iat + 300). */
  exp: number;
  /** Unique token id. */
  jti: string;
  /** Node id. */
  node: string;
  /** Space id of the node. */
  space: string;
  /** The node's ext_kind. */
  kind: string;
  /** The user's capabilities on the node. */
  caps: { view: boolean; download: boolean; share: boolean; edit: boolean; owner: boolean };
  /** The user's display name. */
  name: string;
  /** The user's email. */
  email: string;
  /** The user's BCP 47 locale. */
  locale: string;
}

/** A token that is malformed, badly signed, expired or meant for another app. */
export declare class TokenError extends Error {
  constructor(message: string);
}

/** Options of `verifyToken`. */
export interface VerifyOptions {
  /** When given, `aud` must be "pp-app:" + appId. */
  appId?: string;
  /** The current time in seconds since the epoch; defaults to the clock. */
  now?: number;
}

/**
 * Verifies an HS256 token signed with the app's backend secret (constant-time) and returns its
 * claims. Throws `TokenError` on a malformed token, alg other than HS256, a bad signature, an
 * expired token or, with `appId`, an audience mismatch.
 */
export declare function verifyToken(
  token: string,
  secret: string,
  opts?: VerifyOptions,
): TokenClaims;

/** The bearer token of a request's `Authorization` header (plain object or `Headers`), or null. */
export declare function tokenFromRequest(
  headers: Headers | Record<string, string | string[] | undefined>,
): string | null;
