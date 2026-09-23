import {
  File,
  FileArchive,
  FileAudio,
  FileCode,
  FileSpreadsheet,
  FileText,
  FileVideo,
  ImageIcon,
  type LucideIcon,
} from "lucide-react";
import { getPreviewKind } from "../../../editor/utils/preview";

/** The overview's filters. Every file lands in exactly one. */
export type DeliverableCategory = "image" | "document" | "video" | "other";

export const DELIVERABLE_CATEGORIES: readonly DeliverableCategory[] = [
  "image",
  "document",
  "video",
  "other",
];

function extensionOf(filename: string): string {
  const base = (filename ?? "").toLowerCase().split(/[\\/]/).pop() ?? "";
  const dot = base.lastIndexOf(".");
  return dot <= 0 ? "" : base.slice(dot + 1);
}

// Files people read as documents. Data (CSV, JSON) and code are "other" even
// though the viewer shows them as text: nobody filters for "documents" to
// find a migration script.
const DOCUMENT_EXTENSIONS = new Set([
  "pdf", "md", "markdown", "txt", "rtf",
  "doc", "docx", "odt", "pages",
  "ppt", "pptx", "odp", "key",
  "xls", "xlsx", "ods", "numbers",
]);

const SPREADSHEET_EXTENSIONS = new Set(["csv", "tsv", "xls", "xlsx", "ods", "numbers"]);
const ARCHIVE_EXTENSIONS = new Set(["zip", "tar", "gz", "tgz", "bz2", "xz", "7z", "rar"]);

export function deliverableCategory(
  contentType: string,
  filename: string,
): DeliverableCategory {
  const kind = getPreviewKind(contentType, filename);
  if (kind === "image") return "image";
  if (kind === "video") return "video";
  if (kind === "pdf" || kind === "markdown") return "document";
  return DOCUMENT_EXTENSIONS.has(extensionOf(filename)) ? "document" : "other";
}

export function deliverableIcon(contentType: string, filename: string): LucideIcon {
  const ext = extensionOf(filename);
  if (SPREADSHEET_EXTENSIONS.has(ext)) return FileSpreadsheet;
  if (ARCHIVE_EXTENSIONS.has(ext)) return FileArchive;
  switch (getPreviewKind(contentType, filename)) {
    case "image":
      return ImageIcon;
    case "video":
      return FileVideo;
    case "audio":
      return FileAudio;
    case "pdf":
    case "markdown":
      return FileText;
    case "html":
    case "text":
      return FileCode;
    default:
      return DOCUMENT_EXTENSIONS.has(ext) ? FileText : File;
  }
}
