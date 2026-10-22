import { describe, expect, it } from "vite-plus/test";
import { i18n } from "@lingui/core";
import { unzipSync, strFromU8 } from "fflate";
import type { Extension } from "../src/lib/api";
import { BridgeError, serveBridge } from "../src/ext/app-bridge";
import { extensionById, extKindOf, registerPackagedApps } from "../src/ext/registry";
import { AppPackageError, zipFolder } from "../src/lib/app-package";

const tasks: Extension = {
  id: "tasks",
  name: "Tasks",
  node_kinds: ["tasks.list"],
  version: "1.0.0",
  description: "",
  author: "",
  license: "",
  homepage: "",
  instance: true,
  space: true,
  enabled: true,
  package: {
    digest: "d1",
    entry_url: "https://hub.example/x/tasks/d1/index.html",
    icon_url: "https://hub.example/x/tasks/d1/icon.svg",
    kinds: [
      {
        kind: "tasks.list",
        label: "Task list",
        new_label: "New task list",
        default_name: "Untitled tasks",
        new_hint: "",
        icon_url: "",
      },
    ],
    notifications: { assigned: "{actor} assigned you {excerpt}" },
    translations: {
      vi: {
        "Task list": "Danh sách việc",
        "{actor} assigned you {excerpt}": "{actor} giao cho bạn {excerpt}",
      },
    },
    has_backend: false,
    connect: [],
  },
};

describe("packaged apps in the registry", () => {
  it("draws an uploaded app from the listing, in the manifest's words and language", () => {
    i18n.loadAndActivate({ locale: "en", messages: {} });
    registerPackagedApps([tasks]);
    const kind = extKindOf("tasks.list");
    expect(kind).toBeDefined();
    expect(i18n._(kind!.label)).toBe("Task list");
    expect(i18n._(kind!.newLabel)).toBe("New task list");

    const note = extensionById("tasks")!.notifications!.assigned!({
      actor: "Ann",
      excerpt: "Fix it",
    });
    expect(i18n._(note)).toBe("Ann assigned you Fix it");

    // A locale switch replaces the catalog; the app's strings follow it.
    i18n.loadAndActivate({ locale: "vi", messages: {} });
    expect(i18n._(kind!.label)).toBe("Danh sách việc");
    expect(i18n._(note)).toBe("Ann giao cho bạn Fix it");
    // A string the manifest does not translate stays in English.
    expect(i18n._(kind!.newLabel)).toBe("New task list");
  });

  it("forgets an app that is no longer listed", () => {
    registerPackagedApps([tasks]);
    expect(extKindOf("tasks.list")).toBeDefined();
    registerPackagedApps([]);
    expect(extKindOf("tasks.list")).toBeUndefined();
    expect(extensionById("tasks")).toBeUndefined();
  });
});

/** Sends one request over a bridge and waits for its answer. */
function call(port: MessagePort, msg: unknown): Promise<unknown> {
  return new Promise((resolve) => {
    port.onmessage = (e) => resolve(e.data);
    port.postMessage(msg);
  });
}

describe("serveBridge", () => {
  it("answers a known method, and says why an unknown or failing one did not work", async () => {
    const { port1, port2 } = new MessageChannel();
    const bridge = serveBridge(port1, {
      echo: async (p) => p.value,
      nothing: async () => undefined,
      denied: async () => {
        throw Object.assign(new Error("insufficient permission"), { status: 403 });
      },
      invalid: async () => {
        throw new BridgeError(400, "id must be an id");
      },
    });
    expect(await call(port2, { id: 1, method: "echo", params: { value: 7 } })).toEqual({
      id: 1,
      result: 7,
    });
    expect(await call(port2, { id: 2, method: "nothing" })).toEqual({ id: 2, result: null });
    expect(await call(port2, { id: 3, method: "missing" })).toMatchObject({
      id: 3,
      error: { status: 404 },
    });
    expect(await call(port2, { id: 4, method: "denied" })).toEqual({
      id: 4,
      error: { status: 403, message: "insufficient permission" },
    });
    expect(await call(port2, { id: 5, method: "invalid" })).toMatchObject({
      id: 5,
      error: { status: 400 },
    });
    // An inherited property is not a method.
    expect(await call(port2, { id: 6, method: "toString" })).toMatchObject({
      id: 6,
      error: { status: 404 },
    });
    bridge.close();
    port2.close();
  });
});

function picked(path: string, body: string): File {
  const f = new File([body], path.split("/").pop()!);
  Object.defineProperty(f, "webkitRelativePath", { value: path });
  return f;
}

describe("zipFolder", () => {
  it("packages a folder under its paths, leaving out dot-files and dependencies", async () => {
    const blob = await zipFolder([
      picked("tasks/pp-app.json", "{}"),
      picked("tasks/web/index.html", "<p>hi</p>"),
      picked("tasks/.DS_Store", "x"),
      picked("tasks/.git/HEAD", "x"),
      picked("tasks/node_modules/a/index.js", "x"),
    ]);
    const entries = unzipSync(new Uint8Array(await blob.arrayBuffer()));
    expect(Object.keys(entries).sort()).toEqual(["tasks/pp-app.json", "tasks/web/index.html"]);
    expect(strFromU8(entries["tasks/web/index.html"]!)).toBe("<p>hi</p>");
  });

  it("refuses a folder with nothing to package", async () => {
    await expect(zipFolder([picked("tasks/.env", "x")])).rejects.toBeInstanceOf(AppPackageError);
  });
});
