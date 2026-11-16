import type { DeliverableFile } from "@multica/core/attachments/deliverables";
import { getPreviewKind } from "../../../editor/utils/preview";
import { isDocumentFile } from "../../../editor/utils/file-icon";

/** The overview's filters. Every file lands in exactly one. */
export type DeliverableCategory = "image" | "document" | "video" | "other";

export const DELIVERABLE_CATEGORIES: readonly DeliverableCategory[] = [
  "image",
  "document",
  "video",
  "other",
];

// Data (CSV, JSON) and code are "other" even though the viewer shows them as
// text: nobody filters for "documents" to find a migration script.
export function deliverableCategory(
  contentType: string,
  filename: string,
): DeliverableCategory {
  const kind = getPreviewKind(contentType, filename);
  if (kind === "image") return "image";
  if (kind === "video") return "video";
  return isDocumentFile(contentType, filename) ? "document" : "other";
}

/** Images are individually visible even when their filenames match. */
export function displayDeliverableFiles(files: readonly DeliverableFile[]): DeliverableFile[] {
  return files.flatMap((file) => {
    if (deliverableCategory(file.latest.content_type, file.latest.filename) !== "image") return [file];
    return file.versions.map((attachment) => ({
      ...file,
      key: `${file.key}\u0000${attachment.id}`,
      latest: attachment,
    }));
  }).sort((a, b) => b.latest.created_at.localeCompare(a.latest.created_at) || b.latest.id.localeCompare(a.latest.id));
}
export function displayedVersion(file: DeliverableFile): number {
  return file.versions.findIndex((version) => version.id === file.latest.id) + 1;
}
