/**
 * File type detection utilities for the file viewer.
 */

export type FileCategory = "notebook" | "markdown" | "pdf" | "code" | "image" | "text";

const MARKDOWN_EXTENSIONS = new Set([".md", ".mdx", ".markdown"]);

const IMAGE_EXTENSIONS = new Set([".png", ".jpg", ".jpeg", ".gif", ".svg", ".webp", ".bmp", ".ico"]);

/**
 * Map of file extensions to Shiki language identifiers.
 */
const CODE_EXTENSIONS: Record<string, string> = {
  ".ts": "typescript",
  ".tsx": "tsx",
  ".js": "javascript",
  ".jsx": "jsx",
  ".mjs": "javascript",
  ".cjs": "javascript",
  ".py": "python",
  ".go": "go",
  ".rs": "rust",
  ".rb": "ruby",
  ".java": "java",
  ".kt": "kotlin",
  ".swift": "swift",
  ".c": "c",
  ".cpp": "cpp",
  ".h": "c",
  ".hpp": "cpp",
  ".cs": "csharp",
  ".php": "php",
  ".r": "r",
  ".R": "r",
  ".sql": "sql",
  ".sh": "bash",
  ".bash": "bash",
  ".zsh": "bash",
  ".fish": "fish",
  ".ps1": "powershell",
  ".html": "html",
  ".htm": "html",
  ".css": "css",
  ".scss": "scss",
  ".less": "less",
  ".json": "json",
  ".jsonc": "jsonc",
  ".yaml": "yaml",
  ".yml": "yaml",
  ".toml": "toml",
  ".xml": "xml",
  ".graphql": "graphql",
  ".gql": "graphql",
  ".vue": "vue",
  ".svelte": "svelte",
  ".astro": "astro",
  ".prisma": "prisma",
  ".dockerfile": "dockerfile",
  ".tf": "hcl",
  ".lua": "lua",
  ".perl": "perl",
  ".pl": "perl",
  ".ex": "elixir",
  ".exs": "elixir",
  ".erl": "erlang",
  ".hs": "haskell",
  ".scala": "scala",
  ".clj": "clojure",
  ".dart": "dart",
  ".zig": "zig",
  ".nim": "nim",
  ".ml": "ocaml",
  ".v": "v",
  ".wasm": "wasm",
  ".ini": "ini",
  ".cfg": "ini",
  ".conf": "ini",
  ".env": "dotenv",
  ".makefile": "makefile",
};

function getExtension(path: string): string {
  const basename = path.split("/").pop() ?? path;

  // Handle special filenames
  const lower = basename.toLowerCase();
  if (lower === "dockerfile") return ".dockerfile";
  if (lower === "makefile") return ".makefile";

  const dotIndex = basename.lastIndexOf(".");
  if (dotIndex === -1) return "";
  return basename.slice(dotIndex).toLowerCase();
}

export function detectFileCategory(path: string): FileCategory {
  const ext = getExtension(path);

  if (ext === ".ipynb") return "notebook";
  if (ext === ".pdf") return "pdf";
  if (MARKDOWN_EXTENSIONS.has(ext)) return "markdown";
  if (IMAGE_EXTENSIONS.has(ext)) return "image";
  if (ext in CODE_EXTENSIONS) return "code";

  return "text";
}

export function getLanguageFromPath(path: string): string {
  const ext = getExtension(path);
  return CODE_EXTENSIONS[ext] ?? "text";
}
