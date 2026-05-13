import { shell, type BrowserWindow } from "electron";

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

// Canonical wrapper around webContents.downloadURL. All renderer-controlled
// URLs that trigger a native download MUST flow through here; direct calls
// to `webContents.downloadURL` elsewhere in the main process are banned by
// the no-restricted-syntax rule in apps/desktop/eslint.config.mjs.
// Reuses the same http/https allowlist as openExternalSafely.
export function downloadURLSafely(win: BrowserWindow, url: string): void {
  if (getHttpProtocol(url) === null) {
    console.warn(`[security] blocked downloadURL: ${describeScheme(url)}`);
    return;
  }
  win.webContents.downloadURL(url);
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
