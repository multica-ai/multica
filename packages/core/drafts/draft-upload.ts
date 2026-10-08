import type { Attachment } from "../types";

/**
 * Persistable upload placeholder + result (MUL-5181, L2).
 *
 * Before this, a composer draft stored only COMPLETED `Attachment` rows, so an
 * upload that was still mid-flight when the composer closed had no persisted
 * representation: reopening showed nothing, and a reload/app-restart left no
 * trace that a file had ever been dropped. Modelled on Linear's local store
 * (a first-class `uploads` record carrying `uploadId` + `uploadState`, linked
 * to the draft), a `DraftUpload` lives in the persisted draft from the moment
 * the file is picked, so the upload's lifecycle survives the component that
 * started it.
 *
 * The `clientUploadId` is a client-minted id that ties the placeholder to the
 * module-level {@link UploadCoordinator} running the actual request. It is NOT
 * the server attachment id (which does not exist until the upload completes).
 */
export type UploadStatus = "uploading" | "uploaded" | "failed" | "interrupted";

interface DraftUploadBase {
  /** Client-minted id, stable across the placeholder's whole lifecycle. */
  clientUploadId: string;
  filename: string;
  /** Byte size, mirrored so a placeholder can render without the File. */
  size: number;
  contentType?: string;
}

/**
 * A placeholder whose bytes are not (or no longer) resolvable to an attachment:
 *  - `uploading`: request in flight (owned by the coordinator).
 *  - `failed`: the request errored; keep it so the user sees the failure.
 *  - `interrupted`: LEGACY, no longer produced. Builds before MUL-5391 coerced
 *    a reload-surviving `uploading` record into this; that record is now
 *    dropped instead. Still accepted, rendered, and dismissable so blobs those
 *    builds persisted keep working.
 */
export interface PendingDraftUpload extends DraftUploadBase {
  status: "uploading" | "failed" | "interrupted";
  /** Present for `failed`; the surfaced error message. */
  error?: string;
}

/** A completed upload carrying the full server attachment row. */
export interface UploadedDraftUpload extends DraftUploadBase {
  status: "uploaded";
  attachment: Attachment;
}

export type DraftUpload = PendingDraftUpload | UploadedDraftUpload;

/** True for a completed upload (narrows to {@link UploadedDraftUpload}). */
export function isUploaded(u: DraftUpload): u is UploadedDraftUpload {
  return u.status === "uploaded";
}

/** The completed attachment rows, in order. The submit-bindable set. */
export function uploadedAttachments(uploads: readonly DraftUpload[]): Attachment[] {
  const out: Attachment[] = [];
  for (const u of uploads) {
    if (u.status === "uploaded") out.push(u.attachment);
  }
  return out;
}

/** True while any upload is still in flight — the submit gate reads this. */
export function hasUploadingDraft(uploads: readonly DraftUpload[]): boolean {
  return uploads.some((u) => u.status === "uploading");
}

/** Wrap a completed attachment as an uploaded placeholder. */
export function attachmentToDraftUpload(attachment: Attachment): UploadedDraftUpload {
  return {
    clientUploadId: attachment.id || attachment.url,
    status: "uploaded",
    filename: attachment.filename,
    size: attachment.size_bytes,
    contentType: attachment.content_type || undefined,
    // `download_url` and `attachment_download_url` are minted for the current API
    // response and may be short-lived signed URLs; draft uploads survive dialog
    // closes and app restarts, so both are stripped here. `url`/`markdown_url`
    // stay as the durable render/download paths, and content-editor's session
    // merge backfills an empty download_url from the live upload result (the
    // download flow re-fetches attachment_download_url fresh via getAttachment).
    attachment: { ...attachment, download_url: "", attachment_download_url: "" },
  };
}

function looksLikeAttachment(value: unknown): value is Attachment {
  return (
    typeof value === "object" &&
    value !== null &&
    typeof (value as { id?: unknown }).id === "string" &&
    typeof (value as { filename?: unknown }).filename === "string" &&
    typeof (value as { url?: unknown }).url === "string"
  );
}

function isDraftUploadShape(value: unknown): value is DraftUpload {
  return (
    typeof value === "object" &&
    value !== null &&
    typeof (value as { clientUploadId?: unknown }).clientUploadId === "string" &&
    typeof (value as { status?: unknown }).status === "string"
  );
}

/**
 * Normalize a raw persisted array into `DraftUpload[]`:
 *  - already-`DraftUpload` entries are kept, EXCEPT any still in `uploading`,
 *    which are DROPPED — a reload/restart cannot resume the bytes, and no
 *    placeholder node survives in the body either (see the branch below).
 *  - bare `Attachment` rows (persisted by pre-L2 builds that stored only
 *    completed attachments) are wrapped as `uploaded`.
 *  - anything else is dropped.
 *
 * Runs on every load/rehydrate and on `set` writes, so the in-memory shape is
 * always canonical regardless of which build wrote the persisted blob.
 */
export function normalizeStoredUploads(raw: unknown): DraftUpload[] {
  if (!Array.isArray(raw)) return [];
  const out: DraftUpload[] = [];
  for (const item of raw) {
    if (isDraftUploadShape(item)) {
      if (item.status === "uploading") {
        // Dropped, not coerced to `interrupted`. The bytes were never
        // persisted, so this upload can neither resume nor be retried — and
        // the document has no node for it either, because a placeholder is
        // never serialised. Keeping a record no surface can act on only kept
        // an otherwise-empty draft alive for the full TTL. The absence of the
        // attachment in the body is what tells the user to attach it again.
        continue;
      } else if (item.status === "uploaded") {
        // Trust the persisted attachment only if it still looks like one.
        if (looksLikeAttachment((item as UploadedDraftUpload).attachment)) {
          out.push(item);
        }
      } else {
        out.push(item);
      }
    } else if (looksLikeAttachment(item)) {
      out.push(attachmentToDraftUpload(item));
    }
  }
  return out;
}
