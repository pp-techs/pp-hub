import { describe, expect, it } from "vite-plus/test";
import type { Extension } from "../src/lib/api";
import { sidebarApps } from "../src/lib/ext-apps";

const ext = (id: string, enabled: boolean): Extension =>
  ({ id, name: id, node_kinds: [`${id}.item`], enabled }) as Extension;

describe("sidebarApps", () => {
  const registered = (id: string) => id !== "unknown";

  it("lists apps that are on and carried by this build, in server order", () => {
    const list = [ext("kanban", true), ext("unknown", true), ext("checklist", true)];
    expect(sidebarApps(list, registered).map((e) => e.id)).toEqual(["kanban", "checklist"]);
  });

  it("leaves out apps that are off", () => {
    expect(sidebarApps([ext("kanban", false)], registered)).toEqual([]);
  });

  it("is empty before the extensions list has loaded", () => {
    expect(sidebarApps(undefined, registered)).toEqual([]);
  });
});
