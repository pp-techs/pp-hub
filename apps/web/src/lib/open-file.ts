import type { RegisteredRouter } from "@tanstack/react-router";
import type { Node, Space } from "@/lib/api";
import { queryClient } from "@/lib/query-client";
import { keys } from "@/lib/queries/keys";
import { useUiStore } from "@/lib/ui-store";

/**
 * Opening a file navigates to its drive-shell route. The preference decides
 * whether that navigation stays in this browser tab or starts in a new one.
 *
 * Callers are menus, toasts and command handlers as often as components, so
 * this reaches the router through the instance `main.tsx` binds rather than
 * through `useNavigate` — the same shape as `setUnauthorizedHandler`.
 */
let router: RegisteredRouter | null = null;

export function bindRouter(r: RegisteredRouter): void {
  router = r;
}

/** `?thread=` opens a document's comments on that thread; `?subject=` is the part of an extension node meant. */
export interface FileSearch {
  thread?: string;
  subject?: string;
}

export function validateFileSearch(search: Record<string, unknown>): FileSearch {
  return {
    thread: typeof search.thread === "string" && search.thread ? search.thread : undefined,
    subject: typeof search.subject === "string" && search.subject ? search.subject : undefined,
  };
}

/**
 * Where a node opens: inside its own space for a member, through the
 * outside-shares browser otherwise. `spaces` is the caller's space list.
 */
export function fileLocation(node: Pick<Node, "id" | "space_id">, spaces: Pick<Space, "id">[]) {
  return spaces.some((s) => s.id === node.space_id)
    ? ({
        to: "/drive/$spaceId/file/$nodeId",
        params: { spaceId: node.space_id, nodeId: node.id },
      } as const)
    : ({ to: "/drive/shared/file/$nodeId", params: { nodeId: node.id } } as const);
}

/**
 * The absolute address of a node — `/open/$nodeId`, the one share mails
 * carry. It resolves per account, so the same link opens a file in the editor,
 * a folder in its listing, or the no-access page for someone it was not
 * shared with.
 */
export function nodeLink(nodeId: string, search: FileSearch = {}): string {
  // eslint-disable-next-line lingui/no-unlocalized-strings -- URL path
  const link = new URL(`/open/${encodeURIComponent(nodeId)}`, location.origin);
  // eslint-disable-next-line lingui/no-unlocalized-strings -- URL search parameter names
  if (search.thread) link.searchParams.set("thread", search.thread);
  // eslint-disable-next-line lingui/no-unlocalized-strings -- URL search parameter names
  if (search.subject) link.searchParams.set("subject", search.subject);
  return link.href;
}

/**
 * Opens a file in the drive shell. A node whose space is not known yet goes
 * through `/open/$nodeId`, which resolves it — the address mailed to people.
 *
 * This browser's preference applies to every editor, including documents,
 * apps, viewers and office files. Opening in a new browser tab starts at the
 * resolving address so it can determine which drive surface the reader may
 * use; a blocked popup falls back to the current tab.
 */
export function openFile(
  node: Pick<Node, "id"> & Partial<Pick<Node, "space_id">>,
  search: FileSearch = {},
): void {
  if (useUiStore.getState().fileOpenIn === "new-tab") {
    // eslint-disable-next-line lingui/no-unlocalized-strings -- window target
    const opened = window.open(nodeLink(node.id, search), "_blank");
    if (opened) {
      // `noopener` in the features string would make `open` return null, which
      // is indistinguishable from a blocked popup — sever the link by hand.
      opened.opener = null;
      return;
    }
  }
  if (!router) return;
  const spaces = queryClient.getQueryData<Space[]>(keys.spaces);
  if (!node.space_id || !spaces) {
    void router.navigate({ to: "/open/$nodeId", params: { nodeId: node.id }, search });
    return;
  }
  void router.navigate({
    ...fileLocation({ id: node.id, space_id: node.space_id }, spaces),
    search,
  });
}
