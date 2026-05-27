import { writeFile } from "node:fs/promises";
import path from "node:path";
import { dialog, shell, type BrowserWindow } from "electron";

// True when the URL parses and uses http/https — the only schemes we let
// reach `shell.openExternal`. Scheme comparison is safe because the WHATWG
// URL parser lowercases the protocol field.
export function isSafeExternalHttpUrl(url: string): boolean {
  return getHttpProtocol(url) !== null;
}

// Canonical wrapper around shell.openExternal. All renderer-controlled URLs
// that eventually reach the OS shell MUST flow through here; direct calls
// to `shell.openExternal` elsewhere in the main process are banned by the
// no-restricted-syntax rule in apps/desktop/eslint.config.mjs.
export function openExternalSafely(url: string): Promise<void> | void {
  if (getHttpProtocol(url) === null) {
    console.warn(`[security] blocked openExternal: ${describeScheme(url)}`);
    return;
  }
  return shell.openExternal(url);
}

// Canonical wrapper around renderer-controlled file downloads. All such URLs
// MUST flow through here; direct calls to `webContents.downloadURL` elsewhere
// in the main process are banned by the no-restricted-syntax rule in
// apps/desktop/eslint.config.mjs.
// Reuses the same http/https allowlist as openExternalSafely.
export async function downloadURLSafely(
  win: BrowserWindow,
  url: string,
  options?: { filename?: string; headers?: Record<string, string> },
): Promise<void> {
  if (getHttpProtocol(url) === null) {
    console.warn(`[security] blocked downloadURL: ${describeScheme(url)}`);
    throw new Error("Blocked unsafe download URL");
  }

  const { canceled, filePath } = await dialog.showSaveDialog(win, {
    defaultPath: defaultDownloadFilename(url, options?.filename),
    properties: ["showOverwriteConfirmation"],
  });
  if (canceled || !filePath) return;

  const res = await fetch(url, {
    headers: sanitizeDownloadHeaders(options?.headers),
  });
  if (!res.ok) {
    throw new Error(`Download failed: ${res.status} ${res.statusText}`);
  }

  const body = Buffer.from(await res.arrayBuffer());
  await writeFile(filePath, body);
}

function getHttpProtocol(url: string): "http:" | "https:" | null {
  try {
    const { protocol } = new URL(url);
    if (protocol === "http:" || protocol === "https:") return protocol;
    return null;
  } catch {
    return null;
  }
}

function describeScheme(url: string): string {
  try {
    return `scheme=${new URL(url).protocol}`;
  } catch {
    return "invalid URL";
  }
}

function defaultDownloadFilename(url: string, filename?: string): string {
  const fromOption = filename?.trim();
  const fromURL = safeBasename(new URL(url).pathname);
  const raw = fromOption || fromURL || "attachment";
  const withoutControlChars = Array.from(raw)
    .filter((ch) => ch.charCodeAt(0) >= 32)
    .join("");
  return withoutControlChars
    .replace(/[\\/:*?"<>|]/g, "_")
    .replace(/^\.+$/, "attachment")
    .slice(0, 255) || "attachment";
}

function safeBasename(pathname: string): string {
  try {
    return path.basename(decodeURIComponent(pathname));
  } catch {
    return path.basename(pathname);
  }
}

function sanitizeDownloadHeaders(
  headers?: Record<string, string>,
): Record<string, string> | undefined {
  if (!headers) return undefined;
  const allowed: Record<string, string> = {};
  for (const [name, value] of Object.entries(headers)) {
    const lower = name.toLowerCase();
    if (
      lower === "authorization" ||
      lower === "x-workspace-slug" ||
      lower === "x-csrf-token"
    ) {
      allowed[name] = value;
    }
  }
  return Object.keys(allowed).length > 0 ? allowed : undefined;
}
