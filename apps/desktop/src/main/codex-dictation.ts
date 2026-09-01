import { execFile } from "node:child_process";
import { open } from "node:fs/promises";
import { homedir } from "node:os";
import { isAbsolute, join } from "node:path";
import { BrowserWindow, ipcMain, type IpcMainInvokeEvent } from "electron";
import type { DictationResult } from "@multica/core/types/dictation";
import { CODEX_DICTATION_CHANNEL, CODEX_DICTATION_WORLD } from "../shared/dictation";
import { isTrustedRendererURL } from "./navigation-guard";
import { bundledCliPath } from "./bundled-cli";

export const CODEX_DICTATION_SHORTCUT = "Ctrl+Alt+Shift+D";
const MAX_KEYBINDINGS_BYTES = 64 * 1024;
const trustedWindows = new WeakMap<BrowserWindow, string>();

function isDictationChord(key: unknown): boolean {
  if (typeof key !== "string") return false;
  const parts = key.toLowerCase().split("+").map((part) => part.trim());
  const normalized = parts.map((part) => part === "control" ? "ctrl" : part);
  return normalized.length === 4 &&
    [...new Set(normalized)].sort().join("+") === "alt+ctrl+d+shift";
}

/** The first bridge deliberately supports one safe, explicit chord, not an
 * arbitrary keyboard injector. Duplicate/conditional/conflicting bindings fail
 * closed instead of guessing which command the external app registered. */
export function hasCodexDictationBinding(raw: string): boolean {
  if (Buffer.byteLength(raw, "utf8") > MAX_KEYBINDINGS_BYTES) return false;
  try {
    const data: unknown = JSON.parse(raw);
    if (!Array.isArray(data)) return false;
    const entries = data.filter((entry): entry is Record<string, unknown> =>
      !!entry && typeof entry === "object" && !Array.isArray(entry),
    );
    const toggles = entries.filter((entry) => entry.command === "globalDictationToggle");
    const binding = toggles[0];
    return toggles.length === 1 && !!binding && binding.when === undefined &&
      isDictationChord(binding.key) &&
      !entries.some((entry) => entry !== binding && isDictationChord(entry.key));
  } catch {
    return false;
  }
}

async function isShortcutConfigured(): Promise<boolean> {
  const codexDir = process.env.CODEX_HOME || join(homedir(), ".codex");
  if (!isAbsolute(codexDir)) return false;
  // This is the only OpenAI file we read. Never inspect auth.json, sessions,
  // global state, keychains, browser cookies, or network traffic.
  let file;
  try {
    file = await open(join(codexDir, "keybindings.json"), "r");
    const stat = await file.stat();
    if (!stat.isFile() || stat.size > MAX_KEYBINDINGS_BYTES) return false;
    const buffer = Buffer.alloc(MAX_KEYBINDINGS_BYTES + 1);
    const { bytesRead } = await file.read(buffer, 0, buffer.length, 0);
    if (bytesRead > MAX_KEYBINDINGS_BYTES) return false;
    return hasCodexDictationBinding(buffer.subarray(0, bytesRead).toString("utf8"));
  } catch {
    return false;
  } finally {
    await file?.close();
  }
}

function trustedFocusedWindow(event: IpcMainInvokeEvent): BrowserWindow | null {
  const frame = event.senderFrame;
  if (!frame || event.sender.isDestroyed() || frame !== event.sender.mainFrame) return null;
  const window = BrowserWindow.fromWebContents(event.sender);
  if (!window || window.isDestroyed() || !window.isFocused()) return null;
  const trustedURL = trustedWindows.get(window);
  if (!trustedURL || !isTrustedRendererURL(frame.url, trustedURL)) return null;
  return window;
}

async function hasInputGesture(event: IpcMainInvokeEvent): Promise<boolean> {
  let timer: ReturnType<typeof setTimeout> | undefined;
  try {
    return await Promise.race([
      event.sender.executeJavaScriptInIsolatedWorld(CODEX_DICTATION_WORLD, [{
        code: "globalThis.multicaDictationActivation?.consume() === true",
      }], false).then((value: unknown) => value === true),
      new Promise<boolean>((resolve) => { timer = setTimeout(() => resolve(false), 1000); }),
    ]);
  } finally {
    if (timer) clearTimeout(timer);
  }
}

async function sendDictationChord(window: BrowserWindow): Promise<DictationResult> {
  const handle = window.getNativeWindowHandle();
  const hwnd = handle.length === 8 ? handle.readBigUInt64LE() :
    handle.length === 4 ? BigInt(handle.readUInt32LE()) : 0n;
  if (hwnd === 0n) return { ok: false, reason: "not_focused" };
  return new Promise((resolve) => {
    execFile(
      bundledCliPath(),
      ["--desktop-dictation-v1", "toggle", hwnd.toString()],
      { windowsHide: true, shell: false, timeout: 5000, maxBuffer: 4096, encoding: "utf8" },
      (error, stdout) => {
        if (error) { resolve({ ok: false, reason: "unavailable" }); return; }
        switch (stdout.trim()) {
          case "sent":
            resolve({ ok: true, shortcut: CODEX_DICTATION_SHORTCUT });
            break;
          case "app_not_running":
          case "not_focused":
          case "busy":
          case "cleanup_failed":
            resolve({ ok: false, reason: stdout.trim() as "app_not_running" | "not_focused" | "busy" | "cleanup_failed" });
            break;
          default:
            resolve({ ok: false, reason: "unavailable" });
        }
      },
    );
  });
}

/** Register only windows created by our main/issue renderer loader. */
export function registerCodexDictationWindow(window: BrowserWindow, rendererURL: string): void {
  const registered = trustedWindows.has(window);
  trustedWindows.set(window, rendererURL);
  if (registered || process.platform !== "win32") return;
  // A registered OS hotkey consumes this before Chromium sees it. If Codex
  // failed to register/consume it, reserve the exact chord rather than letting
  // AltGr layouts turn it into draft text. Other shortcuts are unchanged.
  window.webContents.on("before-input-event", (event, input) => {
    if (input.control && input.alt && input.shift && !input.meta &&
      (input.code === "KeyD" || input.key.toLowerCase() === "d")) event.preventDefault();
  });
}

export function setupCodexDictation(): void {
  let pending = false;
  ipcMain.handle(CODEX_DICTATION_CHANNEL, async (event, ...args): Promise<DictationResult> => {
    if (process.platform !== "win32" || args.length !== 0) return { ok: false, reason: "unavailable" };
    if (pending) return { ok: false, reason: "busy" };
    const window = trustedFocusedWindow(event);
    if (!window) return { ok: false, reason: "not_focused" };
    pending = true;
    try {
      // Consume one mic activation in the isolated preload. Page-wide user
      // activation and page-owned DOM wrappers cannot authorize this call.
      if (!await hasInputGesture(event)) return { ok: false, reason: "unavailable" };
      if (!await isShortcutConfigured()) return { ok: false, reason: "not_configured" };
      if (trustedFocusedWindow(event) !== window) return { ok: false, reason: "not_focused" };
      return await sendDictationChord(window);
    } catch {
      // Never forward process output, local paths, or config contents to UI/logs.
      return { ok: false, reason: "unavailable" };
    } finally {
      pending = false;
    }
  });
}
