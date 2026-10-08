import { describe, expect, it } from "vite-plus/test";
import { msg } from "@lingui/core/macro";
import type { ExtManifest } from "@pp-hub/ext-sdk";
import { extNotificationMessage, parseExtKind } from "../src/lib/ext-notification";

const item = (kind: string) => ({
  kind: kind as never,
  actor: { id: "u1", name: "Ann", avatar_url: "" },
  excerpt: "Fix login",
  node: { id: "n1", name: "Roadmap", space_id: "s1" },
});

const kanban = {
  id: "kanban",
  notifications: {
    assigned: ({ actor, excerpt }: { actor: string; excerpt: string }) =>
      msg`${actor} assigned you ${excerpt}`,
  },
} as unknown as ExtManifest;

const lookup = (id: string) => (id === "kanban" ? kanban : undefined);

describe("parseExtKind", () => {
  it("splits ext kinds and rejects core or malformed ones", () => {
    expect(parseExtKind("ext:kanban:assigned")).toEqual({ extId: "kanban", verb: "assigned" });
    expect(parseExtKind("comment")).toBeNull();
    expect(parseExtKind("ext:kanban")).toBeNull();
    expect(parseExtKind("ext::x")).toBeNull();
  });
});

describe("extNotificationMessage", () => {
  it("is null for core kinds", () => {
    expect(extNotificationMessage(item("reply"), lookup)).toBeNull();
  });

  it("uses the manifest wording for a known verb", () => {
    const m = extNotificationMessage(item("ext:kanban:assigned"), lookup);
    expect(m?.message).toContain("assigned you");
    expect(m?.values).toMatchObject({ actor: "Ann", excerpt: "Fix login" });
  });

  it("falls back for an unknown verb or extension", () => {
    for (const kind of ["ext:kanban:moved", "ext:other:assigned"]) {
      const m = extNotificationMessage(item(kind), lookup);
      expect(m?.message).toContain("sent you an update on");
      expect(m?.values).toMatchObject({ actor: "Ann", doc: "Roadmap" });
    }
  });
});
