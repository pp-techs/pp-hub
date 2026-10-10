/** How often, at most, a busy upload queue refetches what it changed. */
export const UPLOAD_REFRESH_MS = 1000;

export interface UploadInvalidatorDeps {
  invalidateFolder: (spaceId: string, parentId: string | undefined) => void;
  invalidateTree: (spaceId: string) => void;
  invalidateSpaces: () => void;
  /** True while any transfer is still uploading. */
  isBusy: () => boolean;
  delayMs?: number;
}

/**
 * Coalesces per-file upload completions into at most one refetch per window.
 * The window is fixed, not a trailing debounce, so a long upload still
 * refreshes its listing once per window. The space list (used bytes) waits
 * until nothing is uploading.
 */
export function createUploadInvalidator(deps: UploadInvalidatorDeps): {
  notify: (spaceId: string, parentId: string | undefined) => void;
  dispose: () => void;
} {
  const delay = deps.delayMs ?? UPLOAD_REFRESH_MS;
  const folders = new Map<string, [string, string | undefined]>();
  const trees = new Set<string>();
  let spacesDirty = false;
  let timer: Parameters<typeof clearTimeout>[0];

  const arm = () => {
    if (timer === undefined) timer = setTimeout(flush, delay);
  };

  function flush() {
    timer = undefined;
    for (const [spaceId, parentId] of folders.values()) deps.invalidateFolder(spaceId, parentId);
    for (const spaceId of trees) deps.invalidateTree(spaceId);
    folders.clear();
    trees.clear();
    if (spacesDirty) {
      if (deps.isBusy()) {
        arm();
      } else {
        deps.invalidateSpaces();
        spacesDirty = false;
      }
    }
  }

  return {
    notify(spaceId, parentId) {
      folders.set(`${spaceId}\n${parentId ?? ""}`, [spaceId, parentId]);
      trees.add(spaceId);
      spacesDirty = true;
      arm();
    },
    dispose() {
      clearTimeout(timer);
      timer = undefined;
    },
  };
}
