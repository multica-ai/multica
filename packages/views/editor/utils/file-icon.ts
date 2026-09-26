/**
 * A file's type glyph — one mapping for every surface that shows a file by
 * its kind: comment file cards, the issue deliverables list and overview.
 */

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
import { getPreviewKind } from "./preview";

function extensionOf(filename: string): string {
  const base = (filename ?? "").toLowerCase().split(/[\\/]/).pop() ?? "";
  const dot = base.lastIndexOf(".");
  return dot <= 0 ? "" : base.slice(dot + 1);
}

// Files people read as documents. Data (CSV, JSON) and code are not, even
// though the viewer shows them as text.
const DOCUMENT_EXTENSIONS = new Set([
  "pdf", "md", "markdown", "txt", "rtf",
  "doc", "docx", "odt", "pages",
  "ppt", "pptx", "odp", "key",
  "xls", "xlsx", "ods", "numbers",
]);

const SPREADSHEET_EXTENSIONS = new Set(["csv", "tsv", "xls", "xlsx", "ods", "numbers"]);
const ARCHIVE_EXTENSIONS = new Set(["zip", "tar", "gz", "tgz", "bz2", "xz", "7z", "rar"]);

export function isDocumentFile(contentType: string, filename: string): boolean {
  const kind = getPreviewKind(contentType, filename);
  if (kind === "pdf" || kind === "markdown") return true;
  return DOCUMENT_EXTENSIONS.has(extensionOf(filename));
}

export function fileIcon(contentType: string, filename: string): LucideIcon {
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
    case "table":
      return FileSpreadsheet;
    case "html":
    case "structured":
    case "text":
      return FileCode;
    default:
      return DOCUMENT_EXTENSIONS.has(ext) ? FileText : File;
  }
}
