import { createHmac } from "node:crypto";
import { describe, expect, it } from "vite-plus/test";
import { TokenError, tokenFromRequest, verifyToken } from "../src/server.js";

const secret = "c2VjcmV0LXNlY3JldC1zZWNyZXQtc2VjcmV0LXNlY3I";
const now = 1_800_000_000;
const claims = {
  iss: "https://hub.example.com",
  aud: "pp-app:tasks",
  sub: "u1",
  iat: now,
  exp: now + 300,
  jti: "j1",
  node: "n1",
  space: "s1",
  kind: "tasks.list",
  caps: { view: true, download: true, share: false, edit: true, owner: false },
  name: "Ann",
  email: "ann@example.com",
  locale: "en",
};

const b64 = (v: unknown) => Buffer.from(JSON.stringify(v)).toString("base64url");
function sign(payload: object, key = secret, header: object = { alg: "HS256", typ: "JWT" }) {
  const body = `${b64(header)}.${b64(payload)}`;
  return `${body}.${createHmac("sha256", key).update(body).digest("base64url")}`;
}

describe("verifyToken", () => {
  it("accepts a token signed with the secret and returns its claims", () => {
    expect(verifyToken(sign(claims), secret, { appId: "tasks", now })).toEqual(claims);
    expect(verifyToken(sign(claims, secret, { alg: "HS256" }), secret, { now })).toEqual(claims);
  });

  it.each<[string, string, { appId?: string; now?: number }]>([
    ["a bad signature", sign(claims, "other"), { now }],
    ["an expired token", sign(claims), { now: now + 300 }],
    ["a wrong audience", sign(claims), { appId: "notes", now }],
    ["alg none", `${b64({ alg: "none" })}.${b64(claims)}.`, { now }],
    ["alg HS512", sign(claims, secret, { alg: "HS512" }), { now }],
    ["a missing exp", sign({ ...claims, exp: undefined }), { now }],
    ["garbage", "not.a.jwt", { now }],
    ["two parts", "a.b", { now }],
  ])("rejects %s", (_, token, opts) => {
    expect(() => verifyToken(token, secret, opts)).toThrow(TokenError);
  });

  it("uses the clock by default", () => {
    const fresh = Math.floor(Date.now() / 1000);
    expect(verifyToken(sign({ ...claims, exp: fresh + 60 }), secret).sub).toBe("u1");
    expect(() => verifyToken(sign({ ...claims, exp: fresh - 1 }), secret)).toThrow(/expired/);
  });
});

describe("tokenFromRequest", () => {
  it("reads a bearer token from a plain object or Headers", () => {
    expect(tokenFromRequest({ authorization: "Bearer abc.def.ghi" })).toBe("abc.def.ghi");
    expect(tokenFromRequest({ Authorization: "bearer xyz" })).toBe("xyz");
    expect(tokenFromRequest(new Headers({ Authorization: "Bearer h.t.k" }))).toBe("h.t.k");
    expect(tokenFromRequest({ authorization: ["Bearer first"] })).toBe("first");
  });

  it("returns null without a bearer token", () => {
    expect(tokenFromRequest({})).toBeNull();
    expect(tokenFromRequest({ authorization: "Basic dXNlcg==" })).toBeNull();
    expect(tokenFromRequest(new Headers())).toBeNull();
  });
});
