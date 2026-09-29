import { msg } from "@lingui/core/macro";
import type { UploadAdapter, UploadResult } from "@slash-editor/core";
import { ApiError, startResumableUpload } from "@/lib/api";
import { i18n } from "@/lib/i18n";

/**
 * How the markdown refers to a file the document owns: `attachment:<hash>`.
 * The hash names a blob in the document's attachment set, so the reference
 * survives copying the document and needs no rewriting when it moves.
 */
// eslint-disable-next-line lingui/no-unlocalized-strings -- URL scheme stored in the markdown
const SCHEME = "attachment:";

/** A file node also records what it holds, so the block can show it before anyone opens it. */
export interface AttachmentResult extends UploadResult {
  name: string;
  size: number;
  mime: string | null;
}

const UPLOAD_FAILED = msg`Upload failed`;
const CONNECTION_LOST = msg`Connection lost`;

/**
 * Uploads a dropped or picked file as an attachment of `documentId`: the
 * same resumable tus upload as any file, addressed to the document instead
 * of a folder. The response that commits it names the stored blob in
 * `Upload-Attachment`, and that hash is what the markdown keeps.
 */
export function attachmentAdapter(documentId: string): UploadAdapter<AttachmentResult> {
  return {
    async upload(file, { signal }) {
      const upload = startResumableUpload({
        file,
        target: { kind: "attachment", documentId },
        onProgress: () => {},
      });
      // The block was deleted mid-upload: stop sending. The server expires
      // what it staged.
      const abort = () => upload.abort();
      signal.addEventListener("abort", abort, { once: true });
      try {
        const committed = await upload.promise;
        // eslint-disable-next-line lingui/no-unlocalized-strings -- HTTP header name
        const hash = committed.header("Upload-Attachment");
        if (!hash) throw new Error(i18n._(UPLOAD_FAILED));
        return {
          url: `${SCHEME}${hash}`,
          name: file.name,
          size: file.size,
          mime: file.type || null,
        };
      } catch (e) {
        throw new Error(describe(e), { cause: e });
      } finally {
        signal.removeEventListener("abort", abort);
      }
    },
  };
}

/** Turns an attachment hash into a URL: the document's own route, a share link's, … */
export type AttachmentUrl = (hash: string) => string;

/**
 * The URL a node view renders for `src`. An `attachment:` reference resolves
 * through `attachmentUrl` — the editor's document, or the share link or
 * version a read-only view renders — and anything else (a link someone
 * typed into the markdown) is used as written.
 */
export function resolveAttachment(
  src: string | null,
  attachmentUrl: AttachmentUrl,
): string | undefined {
  if (!src) return undefined;
  return src.startsWith(SCHEME) ? attachmentUrl(src.slice(SCHEME.length)) : src;
}

/**
 * An uploaded image's alt text, from its file name: `q3-roadmap.png` reads
 * as "q3 roadmap" — better than no text at all, and the markdown keeps it
 * as `![…]`. A name that is nothing but an extension keeps the whole name.
 */
export function altFromFileName(name: string): string {
  const dot = name.lastIndexOf(".");
  const base = (dot > 0 ? name.slice(0, dot) : name).replace(/[_-]+/g, " ").trim();
  return base || name;
}

/**
 * What the failed block says. It is written into the shared document, so it
 * is the uploader's language every collaborator reads — the server's own
 * `detail` when it gave one (quota, size limit), else a generic line.
 */
function describe(e: unknown): string {
  if (e instanceof ApiError) {
    return e.status === 0 ? i18n._(CONNECTION_LOST) : e.message || i18n._(UPLOAD_FAILED);
  }
  return e instanceof Error ? e.message : i18n._(UPLOAD_FAILED);
}
