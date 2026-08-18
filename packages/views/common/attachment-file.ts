import { File, FileAudio, FileImage, FileText, FileVideo } from "lucide-react";
import type { LucideIcon } from "lucide-react";

/** Compact human-readable byte size ("1.2 MB"), no locale dependency. */
export function formatBytes(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes < 0) return "";
  if (bytes < 1024) return `${bytes} B`;
  const units = ["KB", "MB", "GB"];
  let value = bytes / 1024;
  let i = 0;
  while (value >= 1024 && i < units.length - 1) {
    value /= 1024;
    i += 1;
  }
  const rounded = value >= 10 ? Math.round(value) : Math.round(value * 10) / 10;
  return `${rounded} ${units[i]}`;
}

/** Pick a file-type icon from the stored content type, falling back on the
 *  filename extension when the content type is generic or missing. */
export function fileIconFor(contentType: string, filename: string): LucideIcon {
  const type = (contentType || "").toLowerCase();
  if (type.startsWith("image/")) return FileImage;
  if (type.startsWith("video/")) return FileVideo;
  if (type.startsWith("audio/")) return FileAudio;
  if (type.startsWith("text/")) return FileText;
  const ext = filename.toLowerCase().split(".").pop();
  if (ext && ["png", "jpg", "jpeg", "gif", "webp", "svg"].includes(ext)) return FileImage;
  if (ext && ["mp4", "mov", "webm"].includes(ext)) return FileVideo;
  if (ext && ["mp3", "wav", "m4a"].includes(ext)) return FileAudio;
  return File;
}
