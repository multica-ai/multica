/**
 * Preview dispatch table for the AttachmentPreviewModal.
 *
 * Add new previewable kinds here. To add a type:
 *   1. Add a new branch returning a new PreviewKind literal.
 *   2. Add the corresponding renderer in attachment-preview-modal.tsx's dispatch.
 *   3. If the renderer needs the file body as text, also extend isTextPreviewable
 *      in server/internal/handler/file.go so the proxy endpoint accepts it.
 *   4. If the renderer fetches a binary, decide whether to use download_url
 *      (CloudFront, no auth on the client side) or a new authenticated proxy.
 */

export type PreviewKind =
  | "pdf"
  | "video"
  | "audio"
  | "markdown"
  | "html"
  | "text";

const EXT_LANGUAGE_MAP: Record<string, string> = {
  // Markdown
  md: "markdown",
  markdown: "markdown",
  // Plain text — left undefined intentionally; lowlight renders the body
  // unhighlighted when no language is supplied.
  txt: "plaintext",
  log: "plaintext",
  // Web
  html: "xml",
  htm: "xml",
  xml: "xml",
  svg: "xml",
  css: "css",
  scss: "scss",
  sass: "scss",
  less: "less",
  // Config / data
  json: "json",
  yml: "yaml",
  yaml: "yaml",
  toml: "ini",
  ini: "ini",
  conf: "ini",
  // Shell
  sh: "bash",
  bash: "bash",
  zsh: "bash",
  // Languages
  py: "python",
  rb: "ruby",
  go: "go",
  rs: "rust",
  ts: "typescript",
  tsx: "typescript",
  js: "javascript",
  jsx: "javascript",
  mjs: "javascript",
  cjs: "javascript",
  java: "java",
  kt: "kotlin",
  swift: "swift",
  c: "c",
  cc: "cpp",
  cpp: "cpp",
  h: "c",
  hpp: "cpp",
  cs: "csharp",
  php: "php",
  lua: "lua",
  vim: "vim",
  sql: "sql",
  csv: "plaintext",
  tsv: "plaintext",
};

// Build files that are commonly extension-less.
const BASENAME_LANGUAGE_MAP: Record<string, string> = {
  dockerfile: "dockerfile",
  makefile: "makefile",
};

// IMPORTANT — KEEP IN SYNC with isTextPreviewable() in
// server/internal/handler/file.go. If an extension lands here but the proxy
// rejects it, the user sees a 415 fallback in the modal. If the proxy accepts
// but this set doesn't, the Eye button doesn't appear at all.
//
// TODO(follow-up): extract to a JSON single-source-of-truth + generator
// (mirror reserved-slugs pattern in server/internal/handler/reserved_slugs.json).
const TEXT_EXTENSIONS = new Set<string>([
  "md", "markdown", "txt", "log", "csv", "tsv",
  "html", "htm", "json", "xml",
  "yml", "yaml", "toml", "ini", "conf",
  "sh", "bash", "zsh",
  "py", "rb", "go", "rs",
  "ts", "tsx", "js", "jsx", "mjs", "cjs",
  "css", "scss", "sass", "less",
  "sql",
  "java", "kt", "swift",
  "c", "cc", "cpp", "h", "hpp",
  "cs", "php", "lua", "vim",
]);

const TEXT_CONTENT_TYPES = new Set<string>([
  "application/json",
  "application/javascript",
  "application/xml",
  "application/x-yaml",
  "application/yaml",
  "application/toml",
  "application/x-sh",
  "application/x-httpd-php",
]);

const TEXT_BASENAMES = new Set<string>(["dockerfile", "makefile"]);

// Extension fallbacks for media kinds — used when contentType is empty
// (URL-only preview source, no server-side metadata available).
const VIDEO_EXTS = new Set<string>([
  "mp4", "m4v", "mov", "webm", "mkv", "avi", "ogv",
]);
const AUDIO_EXTS = new Set<string>([
  "mp3", "wav", "m4a", "ogg", "oga", "flac", "aac", "opus",
]);

function extOf(filename: string): string {
  const base = filename.toLowerCase().split(/[\\/]/).pop() ?? "";
  const dot = base.lastIndexOf(".");
  if (dot <= 0) return "";
  return base.slice(dot + 1);
}

function baseOf(filename: string): string {
  return (filename.toLowerCase().split(/[\\/]/).pop() ?? "").trim();
}

function normalizeContentType(contentType: string): string {
  const ct = (contentType ?? "").toLowerCase().trim();
  const semi = ct.indexOf(";");
  return (semi >= 0 ? ct.slice(0, semi) : ct).trim();
}

function isTextLike(contentType: string, filename: string): boolean {
  const ct = normalizeContentType(contentType);
  if (ct.startsWith("text/")) return true;
  if (TEXT_CONTENT_TYPES.has(ct)) return true;
  const ext = extOf(filename);
  if (ext && TEXT_EXTENSIONS.has(ext)) return true;
  return TEXT_BASENAMES.has(baseOf(filename));
}

// Dispatch on PreviewKind. New cases go in attachment-preview-modal.tsx;
// remember that the modal frame (header, close, Download CTA, ESC handling)
// is shared — sub-renderers only own the content area.
export function getPreviewKind(
  contentType: string,
  filename: string,
): PreviewKind | null {
  const ct = normalizeContentType(contentType);

  const ext = extOf(filename);

  if (ct === "application/pdf" || ext === "pdf") return "pdf";
  if (ct.startsWith("video/") || (ext && VIDEO_EXTS.has(ext))) return "video";
  if (ct.startsWith("audio/") || (ext && AUDIO_EXTS.has(ext))) return "audio";

  // Markdown — covers both the well-typed case and the common
  // server-side sniffer fallback (text/plain for .md).
  if (ct === "text/markdown" || ext === "md" || ext === "markdown") {
    return "markdown";
  }
  if (ct === "text/html" || ext === "html" || ext === "htm") {
    return "html";
  }

  if (isTextLike(contentType, filename)) return "text";
  return null;
}

export function isPreviewable(contentType: string, filename: string): boolean {
  return getPreviewKind(contentType, filename) !== null;
}

// Pick the hljs language token for a file. Returns undefined when the file
// doesn't have a recognizable extension — callers can fall back to a plain
// `<pre>` render. Kept tiny and ext-driven on purpose: lowlight's `common`
// pack covers the ~50 languages people upload in practice, anything else
// rendered as plain text is preferable to importing the full pack.
export function extensionToLanguage(filename: string): string | undefined {
  const ext = extOf(filename);
  if (ext && EXT_LANGUAGE_MAP[ext]) return EXT_LANGUAGE_MAP[ext];
  const base = baseOf(filename);
  if (BASENAME_LANGUAGE_MAP[base]) return BASENAME_LANGUAGE_MAP[base];
  return undefined;
}
