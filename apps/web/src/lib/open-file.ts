import type { RegisteredRouter } from "@tanstack/react-router";
import type { Node, Space } from "@/lib/api";
import { isOfficeFile } from "@/lib/api";
import { useUiStore } from "@/lib/ui-store";
import { queryClient } from "@/lib/query-client";
import { keys } from "@/lib/queries/keys";

/**
 * Opening a file is a navigation inside the drive shell: a document, a sheet,
 * a board or a PDF takes the content column under the same rail and header,
 * at `/drive/$spaceId/file/$nodeId` — or `/drive/shared/file/$nodeId` for a
 * file shared from a space the caller is not a member of. It used to be a new
 * browser tab per file, a full page with no way back but its own arrow.
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
 * The absolute address of a node — `/open/$nodeId`, the one the share mails
 * carry. It resolves per account, so the same link opens a file in the editor,
 * a folder in its listing, or the no-access page for someone it was not
 * shared with.
 */
export function nodeLink(nodeId: string): string {
  // eslint-disable-next-line lingui/no-unlocalized-strings -- URL path
  return new URL(`/open/${encodeURIComponent(nodeId)}`, location.origin).href;
}

/**
 * The address of an office file on a page of its own — `/office/$nodeId`, the
 * editor filling the window with none of the drive around it.
 */
export function officeLink(nodeId: string): string {
  // eslint-disable-next-line lingui/no-unlocalized-strings -- URL path
  return new URL(`/office/${encodeURIComponent(nodeId)}`, location.origin).href;
}

/**
 * A file opens in a tab of its own (or the tab it is already open in), but a
 * page link followed inside a document stays in that document's tab. The
 * navigation cannot carry that, so it is left here for the tab host to take
 * when the node arrives — see `takeSameTab`.
 */
let sameTabFor: string | null = null;

/** Whether `nodeId` arrived through a same-tab open; reading it clears it. */
export function takeSameTab(nodeId: string): boolean {
  const same = sameTabFor === nodeId;
  sameTabFor = null;
  return same;
}

/**
 * Opens a file in the shell. A node whose space is not known yet goes
 * through `/open/$nodeId`, which resolves it — the address mailed to people.
 * `sameTab` replaces what the current tab shows instead of opening a tab.
 *
 * An office file follows this browser's "open office files in"
 * preference: given its `name`, it opens on a page of its own in a browser tab
 * (`/office/$nodeId`: the editor alone, filling the window). A node passed without a name
 * — a notification, a page link — cannot be told apart and stays in the shell,
 * as does a same-tab open and a browser that blocks the new tab.
 */
export function openFile(
  node: Pick<Node, "id"> & Partial<Pick<Node, "space_id" | "name">>,
  search: FileSearch = {},
  { sameTab = false }: { sameTab?: boolean } = {},
): void {
  if (!router) return;
  if (
    !sameTab &&
    node.name &&
    isOfficeFile(node.name) &&
    useUiStore.getState().officeOpenIn === "new-tab"
  ) {
    // eslint-disable-next-line lingui/no-unlocalized-strings -- window target
    const opened = window.open(officeLink(node.id), "_blank");
    if (opened) {
      // `noopener` in the features string would make `open` return null, which
      // is indistinguishable from a blocked popup — sever the link by hand.
      opened.opener = null;
      return;
    }
  }
  sameTabFor = sameTab ? node.id : null;
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
