import { BrowserWindow, ipcMain } from "electron";
import { open, realpath, stat } from "fs/promises";
import { basename, extname, isAbsolute, relative, resolve, sep } from "path";
import type { LocalFileReadRequest, LocalFileReadResult } from "@multica/core/paths/local-file-link";

export interface LocalFileAccessContext {
  apiUrl: string;
  token: string;
  daemonId: string;
}

// Match the default text-preview budget used by attachment previews.
const MAX_PREVIEW_BYTES = 2 * 1024 * 1024;
const DOCUMENT_EXTENSIONS = new Set([".md", ".markdown", ".txt"]);
const IDENTIFIER = /^[a-zA-Z0-9_-]{1,128}$/;
const TRAVERSAL = /(^|[\\/])\.\.([\\/]|$)/;
function hasControlCharacters(value: string): boolean {
  return Array.from(value).some((character) => character.charCodeAt(0) < 32 || character.charCodeAt(0) === 127);
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

function isRequest(value: unknown): value is LocalFileReadRequest {
  return isRecord(value)
    && typeof value.workspaceId === "string" && IDENTIFIER.test(value.workspaceId)
    && typeof value.projectId === "string" && IDENTIFIER.test(value.projectId)
    && typeof value.href === "string" && value.href.length > 0 && value.href.length <= 8192;
}

function inside(root: string, file: string): boolean {
  const path = relative(root, file);
  return path !== "" && path !== ".." && !path.startsWith(".." + sep) && !isAbsolute(path);
}

function parseDocumentPath(href: string): { path: string } | LocalFileReadResult {
  const raw = href.split("#", 1)[0]!;
  if (raw.includes("?") || hasControlCharacters(raw) || TRAVERSAL.test(raw)) {
    return { ok: false, reason: "unsupported" };
  }
  let path: string;
  try {
    path = decodeURIComponent(raw);
  } catch {
    return { ok: false, reason: "unsupported" };
  }
  if (
    !path || hasControlCharacters(path) || /^[\\/]{2}/.test(path)
    || (path.includes("\\") && sep !== "\\")
    || (/^[a-z][a-z\d+.-]*:/i.test(path) && !/^[a-z]:[\\/]/i.test(path))
    || path.startsWith("~")
  ) return { ok: false, reason: "unsupported" };
  if (TRAVERSAL.test(path)) return { ok: false, reason: "outside_project" };
  if (!DOCUMENT_EXTENSIONS.has(extname(path).toLowerCase())) {
    return { ok: false, reason: "unsupported_type" };
  }
  return { path };
}

async function projectRoot(
  request: LocalFileReadRequest,
  context: LocalFileAccessContext,
): Promise<string | null> {
  const response = await fetch(
    context.apiUrl.replace(/\/+$/, "") + "/api/projects/" + encodeURIComponent(request.projectId) + "/resources",
    {
      headers: { Authorization: "Bearer " + context.token, "X-Workspace-ID": request.workspaceId },
      redirect: "error",
      signal: AbortSignal.timeout(5_000),
    },
  );
  if (!response.ok) return null;
  const body: unknown = await response.json();
  if (!isRecord(body) || !Array.isArray(body.resources)) return null;
  const matches = body.resources.filter((value): value is Record<string, unknown> => {
    if (!isRecord(value) || !isRecord(value.resource_ref)) return false;
    return value.workspace_id === request.workspaceId
      && value.project_id === request.projectId
      && value.resource_type === "local_directory"
      && value.resource_ref.daemon_id === context.daemonId
      && typeof value.resource_ref.local_path === "string"
      && isAbsolute(value.resource_ref.local_path);
  });
  if (matches.length !== 1) return null;
  const ref = matches[0]!.resource_ref as Record<string, unknown>;
  return ref.local_path as string;
}

/** Reads a bounded document for local preview; contents never go to the resource API. */
export async function readLocalFile(
  value: unknown,
  context: LocalFileAccessContext | null,
): Promise<LocalFileReadResult> {
  if (!isRequest(value)) return { ok: false, reason: "unsupported" };
  const parsed = parseDocumentPath(value.href);
  if (!("path" in parsed)) return parsed;
  if (!context) return { ok: false, reason: "unavailable" };

  let root: string | null;
  try {
    root = await projectRoot(value, context);
  } catch {
    return { ok: false, reason: "unavailable" };
  }
  if (!root) return { ok: false, reason: "unavailable" };

  try {
    const canonicalRoot = await realpath(root);
    const file = resolve(root, parsed.path);
    if (!inside(resolve(root), file) && !inside(canonicalRoot, file)) {
      return { ok: false, reason: "outside_project" };
    }
    const canonicalFile = await realpath(file);
    if (!inside(canonicalRoot, canonicalFile)) return { ok: false, reason: "outside_project" };
    const metadata = await stat(canonicalFile);
    if (!DOCUMENT_EXTENSIONS.has(extname(canonicalFile).toLowerCase()) || !metadata.isFile()) {
      return { ok: false, reason: "unsupported_type" };
    }
    if (metadata.size > MAX_PREVIEW_BYTES) return { ok: false, reason: "too_large" };

    const handle = await open(canonicalFile, "r");
    try {
      // Read at most one byte over the limit, including if the file grows.
      const buffer = Buffer.alloc(MAX_PREVIEW_BYTES + 1);
      let sizeBytes = 0;
      while (sizeBytes < buffer.length) {
        const { bytesRead } = await handle.read(buffer, sizeBytes, buffer.length - sizeBytes, null);
        if (bytesRead === 0) break;
        sizeBytes += bytesRead;
      }
      if (sizeBytes > MAX_PREVIEW_BYTES) return { ok: false, reason: "too_large" };
      return {
        ok: true,
        filename: basename(file),
        text: buffer.toString("utf8", 0, sizeBytes),
        contentType: extname(file).toLowerCase() === ".txt" ? "text/plain" : "text/markdown",
        sizeBytes,
      };
    } finally {
      await handle.close();
    }
  } catch (error) {
    return { ok: false, reason: (error as NodeJS.ErrnoException).code === "ENOENT" ? "not_found" : "failed" };
  }
}

export function setupLocalFileReading(
  getContext: () => Promise<LocalFileAccessContext | null>,
  authorizeSender: (window: BrowserWindow) => boolean,
): void {
  ipcMain.handle("file:read-local", async (event, request: unknown): Promise<LocalFileReadResult> => {
    const window = BrowserWindow.fromWebContents(event.sender);
    if (!window || event.senderFrame !== event.sender.mainFrame || !authorizeSender(window)) {
      return { ok: false, reason: "unavailable" };
    }
    try {
      return await readLocalFile(request, await getContext());
    } catch {
      return { ok: false, reason: "unavailable" };
    }
  });
}
