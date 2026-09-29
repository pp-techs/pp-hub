import { msg } from "@lingui/core/macro";
import type { UploadAdapter, UploadResult } from "@slash-editor/core";
import { ApiError, attachmentUrl, startResumableUpload } from "@/lib/api";
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

/**
 * The URL a node view renders for `src`. An `attachment:` reference resolves
 * against the document being edited; anything else (a link someone typed
 * into the markdown) is used as written.
 */
export function resolveAttachment(documentId: string, src: string | null): string | undefined {
  if (!src) return undefined;
  return src.startsWith(SCHEME) ? attachmentUrl(documentId, src.slice(SCHEME.length)) : src;
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
