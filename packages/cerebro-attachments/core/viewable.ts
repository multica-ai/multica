/**
 * Viewable-attachment detection.
 *
 * An attachment is "viewable" when it makes sense to render it inline in the
 * artifact viewer rather than triggering a download. The viewer handles
 * HTML, Markdown, plain text and JSON; everything else (images, PDF, binary)
 * keeps the existing browser-handled behavior on the original CDN URL.
 *
 * Detection prefers `content_type` (set when the file was uploaded) and
 * falls back to the filename extension for legacy uploads where the MIME
 * type was stored as `application/octet-stream`.
 */

const VIEWABLE_CONTENT_TYPES = new Set([
  "text/html",
  "text/markdown",
  "text/plain",
  "application/json",
]);

const VIEWABLE_EXTENSIONS = new Set([
  "html",
  "htm",
  "md",
  "markdown",
  "txt",
  "text",
  "json",
]);

function extensionOf(filename: string): string {
  const dot = filename.lastIndexOf(".");
  if (dot < 0) return "";
  return filename.slice(dot + 1).toLowerCase();
}

export type ViewableKind = "html" | "markdown" | "text" | "json";

export function viewableKind(
  contentType: string,
  filename: string,
): ViewableKind | null {
  const ct = contentType.toLowerCase().split(";")[0]?.trim() ?? "";
  if (ct === "text/html") return "html";
  if (ct === "text/markdown") return "markdown";
  if (ct === "application/json") return "json";
  if (ct === "text/plain") return "text";

  const ext = extensionOf(filename);
  if (ext === "html" || ext === "htm") return "html";
  if (ext === "md" || ext === "markdown") return "markdown";
  if (ext === "json") return "json";
  if (ext === "txt" || ext === "text") return "text";

  return null;
}

export function isViewableAttachment(
  contentType: string,
  filename: string,
): boolean {
  if (VIEWABLE_CONTENT_TYPES.has(contentType.toLowerCase().split(";")[0]?.trim() ?? "")) {
    return true;
  }
  return VIEWABLE_EXTENSIONS.has(extensionOf(filename));
}
