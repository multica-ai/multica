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
