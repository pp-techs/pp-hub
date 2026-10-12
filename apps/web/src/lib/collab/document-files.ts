import { msg } from "@lingui/core/macro";
import type { ResolveSrc } from "@slash-editor/core";
import type { MouseEvent } from "react";
import { toast } from "sonner";
import {
  attachmentUrl,
  getRelativeNode,
  getShareRelativeNode,
  relativeUrl,
  shareAttachmentUrl,
  shareNodeContentUrl,
  shareRelativeUrl,
  type RelativeNode,
} from "@/lib/api";
import { ATTACHMENT_SCHEME } from "@/lib/collab/attachments";
import { i18n } from "@/lib/i18n";

/**
 * Where one rendering of a markdown document — the editor, a read-only view,
 * a past version, a share link — fetches the files the document references,
 * and how it opens a relative link.
 *
 * Two kinds of reference: `attachment:<hash>`, a file the document owns, and
 * a relative path (`images/a.png`, `../Spec.md`), a file beside the document
 * in the drive, resolved by the server against the document's place in the
 * tree each time it is read.
 */
export interface DocumentFiles {
  /** Where an `attachment:<hash>` reference is served. */
  attachment: (hash: string) => string;
  /** Where a relative reference, as written in the markdown, is served. */
  relative: (path: string) => string;
  /** Opens what a relative link names. */
  openRelative: (path: string) => void;
}

/**
 * Whether `ref` is a relative reference — the server's rule
 * (`mdoc.RelativePath`), loosely: no scheme or host, not rooted, a path
 * before any `?` or `#`, naming a file rather than a folder (`a/`, `.`,
 * `..`). What slips past this, the server refuses, and the block shows that.
 */
export function isRelativeRef(ref: string | null | undefined): ref is string {
  if (!ref || ref.length > 1024 || ref.startsWith("/") || ref.startsWith("\\")) return false;
  const path = ref.split(/[?#]/, 1)[0];
  if (!path || /^[a-z][a-z0-9+.-]*:/i.test(path)) return false;
  const last = path.slice(path.lastIndexOf("/") + 1);
  return last !== "" && last !== "." && last !== "..";
}

/**
 * The `resolveSrc` of a rendering's media nodes. Display only: the stored
 * `src` stays as the markdown wrote it. Anything else (a link someone typed)
 * is used as written.
 */
export function documentFilesResolver(files: DocumentFiles): ResolveSrc {
  return (src) => {
    if (src.startsWith(ATTACHMENT_SCHEME))
      return files.attachment(src.slice(ATTACHMENT_SCHEME.length));
    return isRelativeRef(src) ? files.relative(src) : src;
  };
}

/**
 * A click on a relative link inside a rendered document opens it through
 * `files` instead of letting the browser resolve the path against the app's
 * own URL. While the editor is editable a plain click edits the link, so
 * only a Ctrl/⌘-click opens it there.
 */
export function relativeLinkClick(files: DocumentFiles, editable: () => boolean) {
  return (event: MouseEvent<HTMLElement>) => {
    const anchor = (event.target as Element).closest?.("a[href]");
    // eslint-disable-next-line lingui/no-unlocalized-strings -- attribute name
    const href = anchor?.getAttribute("href");
    if (!isRelativeRef(href)) return;
    if (editable() && !event.metaKey && !event.ctrlKey) return;
    event.preventDefault();
    event.stopPropagation();
    files.openRelative(href);
  };
}

const CANNOT_OPEN = msg`This link leads nowhere you can open`;

/** Opens what a relative link names, saying so when it names nothing the reader can open. */
function openWith(
  lookup: (path: string) => Promise<RelativeNode>,
  open: (node: RelativeNode) => void,
) {
  return (path: string) => {
    lookup(path).then(open, () => toast.error(i18n._(CANNOT_OPEN)));
  };
}

/**
 * A signed-in rendering of `nodeId`. `versionId` is the version shown, whose
 * author decides how far the document reaches. A linked page opens through
 * `openPage`; any other file or folder at `/open/<id>`, in a new tab.
 */
export function nodeDocumentFiles(
  nodeId: string,
  { versionId, openPage }: { versionId?: string; openPage: (pageId: string) => void },
): DocumentFiles {
  return {
    attachment: (hash) => attachmentUrl(nodeId, hash),
    relative: (path) => relativeUrl(nodeId, path, versionId),
    openRelative: openWith(
      (path) => getRelativeNode(nodeId, path),
      (node) =>
        // eslint-disable-next-line lingui/no-unlocalized-strings -- app route and window target
        node.markdown ? openPage(node.id) : void window.open(`/open/${node.id}`, "_blank"),
    ),
  };
}

/**
 * `docId` inside a share link. A linked page the link covers opens through
 * `openPage`, a file in place in a new tab; a folder has no view of its own
 * here.
 */
export function shareDocumentFiles(
  token: string,
  docId: string,
  openPage: (pageId: string) => void,
): DocumentFiles {
  return {
    attachment: (hash) => shareAttachmentUrl(token, docId, hash),
    relative: (path) => shareRelativeUrl(token, docId, path),
    openRelative: openWith(
      (path) => getShareRelativeNode(token, docId, path),
      (node) => {
        if (node.markdown) openPage(node.id);
        // eslint-disable-next-line lingui/no-unlocalized-strings -- window target
        else if (node.kind === "file") window.open(shareNodeContentUrl(token, node.id), "_blank");
        else toast.error(i18n._(CANNOT_OPEN));
      },
    ),
  };
}
