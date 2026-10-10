import { afterEach, beforeEach, expect, test, vi } from "vite-plus/test";
import { createUploadInvalidator } from "../src/lib/upload-invalidation.ts";

function setup(busy = false) {
  const state = { busy };
  const deps = {
    invalidateFolder: vi.fn(),
    invalidateTree: vi.fn(),
    invalidateSpaces: vi.fn(),
    isBusy: () => state.busy,
  };
  return { state, deps, inv: createUploadInvalidator(deps) };
}

beforeEach(() => {
  vi.useFakeTimers();
});
afterEach(() => {
  vi.useRealTimers();
});

test("many notifies for one folder inside a window refetch it once", () => {
  const { deps, inv } = setup();
  for (let i = 0; i < 5; i++) {
    inv.notify("s1", "f1");
    vi.advanceTimersByTime(150);
  }
  vi.advanceTimersByTime(1000);
  expect(deps.invalidateFolder).toHaveBeenCalledTimes(1);
  expect(deps.invalidateFolder).toHaveBeenCalledWith("s1", "f1");
  expect(deps.invalidateTree).toHaveBeenCalledTimes(1);
  expect(deps.invalidateSpaces).toHaveBeenCalledTimes(1);
});

test("each notified folder is refetched once", () => {
  const { deps, inv } = setup();
  inv.notify("s1", "f1");
  inv.notify("s1", undefined);
  inv.notify("s1", "f1");
  vi.advanceTimersByTime(1000);
  expect(deps.invalidateFolder).toHaveBeenCalledTimes(2);
  expect(deps.invalidateFolder).toHaveBeenCalledWith("s1", "f1");
  expect(deps.invalidateFolder).toHaveBeenCalledWith("s1", undefined);
  expect(deps.invalidateTree).toHaveBeenCalledTimes(1);
});

test("spaces wait until nothing is uploading", () => {
  const { state, deps, inv } = setup(true);
  inv.notify("s1", "f1");
  vi.advanceTimersByTime(1000);
  expect(deps.invalidateFolder).toHaveBeenCalledTimes(1);
  expect(deps.invalidateSpaces).not.toHaveBeenCalled();
  state.busy = false;
  vi.advanceTimersByTime(1000);
  expect(deps.invalidateSpaces).toHaveBeenCalledTimes(1);
  vi.advanceTimersByTime(5000);
  expect(deps.invalidateSpaces).toHaveBeenCalledTimes(1);
  expect(deps.invalidateFolder).toHaveBeenCalledTimes(1);
});

test("dispose before the window fires nothing", () => {
  const { deps, inv } = setup();
  inv.notify("s1", "f1");
  inv.dispose();
  vi.advanceTimersByTime(5000);
  expect(deps.invalidateFolder).not.toHaveBeenCalled();
  expect(deps.invalidateTree).not.toHaveBeenCalled();
  expect(deps.invalidateSpaces).not.toHaveBeenCalled();
});
