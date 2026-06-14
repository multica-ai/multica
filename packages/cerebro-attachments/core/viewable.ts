/**
 * Viewable-attachment detection.
 *
 * An attachment is "viewable" when it makes sense to render it inline in the
 * artifact viewer rather than triggering a download. The viewer handles
 * HTML, Markdown, plain text, JSON, text-based PDFs, and raster images;
 * everything else keeps the existing browser-handled behavior on the original
 * CDN URL.
 *
 * SVG is intentionally NOT viewable here. The server forces
 * `Content-Disposition: attachment` on `image/svg+xml` (SVG can carry inline
 * <script>), so an `<img src>` against the download endpoint would download
 * instead of render. Keeping SVG non-viewable mirrors that server-side
 * carve-out (storage/util.go `isInlineContentType`).
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
  "application/pdf",
]);

const VIEWABLE_EXTENSIONS = new Set([
  "html",
  "htm",
  "md",
  "markdown",
  "txt",
  "text",
  "json",
  "pdf",
]);

// Raster image formats an <img> tag renders natively. SVG is deliberately
// excluded — see the file header for the security rationale.
const VIEWABLE_IMAGE_EXTENSIONS = new Set([
  "png",
  "jpg",
  "jpeg",
  "gif",
  "webp",
  "avif",
  "bmp",
  "ico",
]);

function extensionOf(filename: string): string {
  const dot = filename.lastIndexOf(".");
  if (dot < 0) return "";
  return filename.slice(dot + 1).toLowerCase();
}

function normalizeContentType(contentType: string): string {
  return contentType.toLowerCase().split(";")[0]?.trim() ?? "";
}

// A renderable raster image: any image/* type except SVG (XSS carve-out).
function isViewableImage(contentType: string, filename: string): boolean {
  const ct = normalizeContentType(contentType);
  if (ct === "image/svg+xml") return false;
  if (ct.startsWith("image/")) return true;
  return VIEWABLE_IMAGE_EXTENSIONS.has(extensionOf(filename));
}

export type ViewableKind = "html" | "markdown" | "text" | "json" | "pdf" | "image";

export function viewableKind(
  contentType: string,
  filename: string,
): ViewableKind | null {
  const ct = normalizeContentType(contentType);
  if (ct === "text/html") return "html";
  if (ct === "text/markdown") return "markdown";
  if (ct === "application/json") return "json";
  if (ct === "application/pdf") return "pdf";
  if (ct === "text/plain") return "text";
  if (isViewableImage(contentType, filename)) return "image";

  const ext = extensionOf(filename);
  if (ext === "html" || ext === "htm") return "html";
  if (ext === "md" || ext === "markdown") return "markdown";
  if (ext === "json") return "json";
  if (ext === "pdf") return "pdf";
  if (ext === "txt" || ext === "text") return "text";

  return null;
}

export function isViewableAttachment(
  contentType: string,
  filename: string,
): boolean {
  if (VIEWABLE_CONTENT_TYPES.has(normalizeContentType(contentType))) {
    return true;
  }
  if (isViewableImage(contentType, filename)) {
    return true;
  }
  return VIEWABLE_EXTENSIONS.has(extensionOf(filename));
}
