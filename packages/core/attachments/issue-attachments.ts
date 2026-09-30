/**
 * Issue attachments for the sidebar — human-uploaded files on this issue
 * (description inputs and comment files). Distinct from deliverables
 * (every comment file, including agents): this list never includes agents.
 *
 * Description files are those the current markdown actually references, plus
 * pending uploads still sitting in the description editor. Unbound
 * comment-composer files also have `comment_id` null until send, so they
 * must not be treated as description files just because they are unattached.
 *
 * Comment files come from timeline comments (patched live), same as
 * deliverables — the issue-wide list is not refreshed when a bind lands.
 *
 * Version grouping reuses deliverable identity (filename + content type).
 *
 * Pure — no React, no DOM, no platform APIs.
 */

import type { Attachment } from "../types/attachment";
import { contentReferencesAttachment } from "../types/attachment-url";
import {
  deliverableKey,
  type DeliverableFile,
  type DeliverableSourceComment,
} from "./deliverables";

export type IssueAttachmentFile = DeliverableFile;

export const MEMBER_UPLOADER_TYPE = "member";

export interface CollectIssueAttachmentFilesInput {
  description?: string | null;
  /** Issue-wide list (plus description pending). Used only for description refs. */
  attachments?: ReadonlyArray<Attachment> | null;
  /** Description-editor uploads not yet referenced / rebound. */
  pendingDescriptionAttachments?: ReadonlyArray<Attachment> | null;
  comments?: ReadonlyArray<DeliverableSourceComment | null | undefined> | null;
}

function isMemberUpload(attachment: Attachment): boolean {
  return attachment.uploader_type === MEMBER_UPLOADER_TYPE;
}

function compareUploadOrder(a: Attachment, b: Attachment): number {
  const at = Date.parse(a.created_at);
  const bt = Date.parse(b.created_at);
  if (at !== bt) return (Number.isNaN(at) ? 0 : at) - (Number.isNaN(bt) ? 0 : bt);
  return a.id < b.id ? -1 : a.id > b.id ? 1 : 0;
}

function groupVersions(uploads: Iterable<Attachment>): IssueAttachmentFile[] {
  const byKey = new Map<string, Attachment[]>();
  const seen = new Set<string>();

  for (const attachment of uploads) {
    if (!attachment?.id || seen.has(attachment.id)) continue;
    seen.add(attachment.id);
    const key = deliverableKey(attachment);
    const versions = byKey.get(key);
    if (versions) versions.push(attachment);
    else byKey.set(key, [attachment]);
  }

  const files: IssueAttachmentFile[] = [];
  for (const [key, versions] of byKey) {
    versions.sort(compareUploadOrder);
    files.push({ key, versions, latest: versions[versions.length - 1]! });
  }
  return files.sort((a, b) => compareUploadOrder(b.latest, a.latest));
}

function* descriptionUploads(input: CollectIssueAttachmentFilesInput): Iterable<Attachment> {
  const description = input.description ?? "";
  for (const attachment of input.attachments ?? []) {
    if (!isMemberUpload(attachment)) continue;
    if (contentReferencesAttachment(description, attachment)) yield attachment;
  }
  for (const attachment of input.pendingDescriptionAttachments ?? []) {
    if (isMemberUpload(attachment)) yield attachment;
  }
}

function* commentUploads(
  comments: CollectIssueAttachmentFilesInput["comments"],
): Iterable<Attachment> {
  for (const comment of comments ?? []) {
    if (!comment || comment.deleted_at) continue;
    if (comment.type !== undefined && comment.type !== "comment") continue;
    for (const attachment of comment.attachments ?? []) {
      if (isMemberUpload(attachment)) yield attachment;
    }
  }
}

/** Human-uploaded files on the issue, newest first by each file's latest version. */
export function collectIssueAttachmentFiles(
  input: CollectIssueAttachmentFilesInput,
): IssueAttachmentFile[] {
  return groupVersions([...descriptionUploads(input), ...commentUploads(input.comments)]);
}
