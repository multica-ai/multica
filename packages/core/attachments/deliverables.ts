/**
 * Issue deliverables — the files an issue as a whole has produced (MUL-7649).
 *
 * A deliverable file is something uploaded in a comment. Attachments on the
 * issue description are inputs (the brief, reference material), not output,
 * so they never count.
 *
 * Re-uploading a file under the same name and type is a new VERSION of that
 * file, not another deliverable: agents iterate on a report or a screenshot
 * across runs, and the reader wants the latest one with its history behind
 * it, not five near-identical rows.
 *
 * Built from comment entries rather than the issue-wide attachment list: the
 * list is fetched before the upload is bound to its comment and is not
 * refreshed when that bind lands, while the timeline is patched live and
 * already carries each comment's attachments.
 *
 * Pure — no React, no DOM, no platform APIs.
 */

import type { Attachment } from "../types/attachment";

/** The part of a timeline comment this module reads. */
export interface DeliverableSourceComment {
  id: string;
  type?: string;
  attachments?: ReadonlyArray<Attachment> | null;
  /** Tombstoned comments render no files, so they contribute none. */
  deleted_at?: string | null;
}

export interface DeliverableFile {
  /** Identity shared by every version: normalized content type + filename. */
  key: string;
  /** Every upload of this file, oldest first. Never empty. */
  versions: Attachment[];
  /** The newest upload — what lists show and open. */
  latest: Attachment;
}

function normalizeContentType(contentType: string): string {
  const ct = (contentType ?? "").toLowerCase().trim();
  const semi = ct.indexOf(";");
  return (semi >= 0 ? ct.slice(0, semi) : ct).trim();
}

/**
 * Two uploads are versions of one file when their name and type match. The
 * type is compared without parameters so `text/html; charset=utf-8` from one
 * client and `text/html` from another still line up.
 */
export function deliverableKey(
  attachment: Pick<Attachment, "filename" | "content_type">,
): string {
  return `${normalizeContentType(attachment.content_type)}\u0000${(attachment.filename ?? "").trim()}`;
}

// Oldest first. Attachment ids are UUIDv7, so the id breaks a created_at tie
// in upload order.
function compareUploadOrder(a: Attachment, b: Attachment): number {
  const at = Date.parse(a.created_at);
  const bt = Date.parse(b.created_at);
  if (at !== bt) return (Number.isNaN(at) ? 0 : at) - (Number.isNaN(bt) ? 0 : bt);
  return a.id < b.id ? -1 : a.id > b.id ? 1 : 0;
}

/**
 * Group every file uploaded in `comments` into deliverables, newest first
 * (by each file's latest version).
 */
export function collectDeliverableFiles(
  comments: ReadonlyArray<DeliverableSourceComment | null | undefined>,
): DeliverableFile[] {
  const byKey = new Map<string, Attachment[]>();
  const seen = new Set<string>();

  for (const comment of comments) {
    if (!comment || comment.deleted_at) continue;
    if (comment.type !== undefined && comment.type !== "comment") continue;
    for (const attachment of comment.attachments ?? []) {
      if (!attachment?.id || seen.has(attachment.id)) continue;
      seen.add(attachment.id);
      const key = deliverableKey(attachment);
      const versions = byKey.get(key);
      if (versions) versions.push(attachment);
      else byKey.set(key, [attachment]);
    }
  }

  const files: DeliverableFile[] = [];
  for (const [key, versions] of byKey) {
    versions.sort(compareUploadOrder);
    files.push({ key, versions, latest: versions[versions.length - 1]! });
  }
  return files.sort((a, b) => compareUploadOrder(b.latest, a.latest));
}

/**
 * The deliverable an attachment belongs to and its 1-based version number, or
 * undefined when the attachment is not a deliverable (a description file, a
 * file from another surface).
 */
export function findDeliverableVersion(
  files: ReadonlyArray<DeliverableFile>,
  attachmentId: string,
): { file: DeliverableFile; version: number } | undefined {
  if (!attachmentId) return undefined;
  for (const file of files) {
    const index = file.versions.findIndex((v) => v.id === attachmentId);
    if (index >= 0) return { file, version: index + 1 };
  }
  return undefined;
}
