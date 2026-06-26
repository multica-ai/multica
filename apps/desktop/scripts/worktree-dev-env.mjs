// Per-worktree dev isolation for `pnpm dev:desktop`.
//
// Two `pnpm dev:desktop` instances from two different git worktrees collide on
// the renderer Vite port (5173) and the single-instance lock / userData dir
// (keyed by the app name "Multica Canary"). The env hooks to override both
// already exist — electron.vite.config.ts reads DESKTOP_RENDERER_PORT and
// src/main/index.ts reads DESKTOP_APP_SUFFIX — but nothing derives unique
// values per worktree. This module does, mirroring the offset scheme that
// scripts/init-worktree-env.sh already uses for backend/frontend ports.
//
// Backend targeting is deliberately NOT touched here: which backend the desktop
// connects to stays driven by apps/desktop/.env* (VITE_API_URL / VITE_WS_URL),
// exactly as documented. This module only adds the two knobs needed for two
// Electron processes to coexist.

import { statSync } from "node:fs";
import { basename, join } from "node:path";

// Worktree renderer ports start at 5174 so they never reuse 5173 — the primary
// checkout's default — even when a worktree's offset is 0 (e.g. POSIX cksum of
// "/tmp/multica-3494" is 1189739000, and 1189739000 % 1000 === 0). Range 5174–6173.
const RENDERER_PORT_BASE = 5174;
const OFFSET_MODULO = 1000;

// POSIX cksum (CRC-32), kept byte-compatible with `cksum(1)` so the offset
// matches scripts/init-worktree-env.sh — a worktree's backend (18080+offset),
// frontend (13000+offset) and desktop renderer (5174+offset) ports all share
// one offset. Verified against coreutils: cksum of "/tmp/foo" → 427878967.
function cksumTable() {
  const table = new Uint32Array(256);
  const POLY = 0x04c11db7;
  for (let i = 0; i < 256; i++) {
    let crc = i << 24;
    for (let bit = 0; bit < 8; bit++) {
      crc = crc & 0x80000000 ? (crc << 1) ^ POLY : crc << 1;
    }
    table[i] = crc >>> 0;
  }
  return table;
}

const TABLE = cksumTable();

export function cksum(buf) {
  let crc = 0;
  for (const byte of buf) {
    crc = (((crc << 8) >>> 0) ^ TABLE[((crc >>> 24) ^ byte) & 0xff]) >>> 0;
  }
  // POSIX appends the byte length, least-significant byte first.
  let len = buf.length;
  while (len > 0) {
    crc = (((crc << 8) >>> 0) ^ TABLE[((crc >>> 24) ^ (len & 0xff)) & 0xff]) >>> 0;
    len = Math.floor(len / 256);
  }
  return (~crc) >>> 0;
}

export function offsetForPath(path) {
  return cksum(Buffer.from(path)) % OFFSET_MODULO;
}

export function rendererPortForPath(path) {
  return RENDERER_PORT_BASE + offsetForPath(path);
}

// Worktree → a readable, unique, filesystem-safe suffix "<folder>-<offset>".
// The dev app then shows e.g. "Multica Canary mul-3724-194" in Cmd+Tab and gets
// its own userData / single-instance lock under that name. The offset is what
// makes the lock unique: the folder name alone collides for worktrees that share
// a basename at different paths (e.g. /a/multica vs /b/multica) or whose names
// slug to the same fallback — those would share one lock and the second Electron
// would still be blocked.
export function appSuffixForPath(path) {
  const slug =
    basename(path)
      .toLowerCase()
      .replace(/[^a-z0-9]+/g, "-")
      .replace(/^-+|-+$/g, "") || "worktree";
  return `${slug}-${offsetForPath(path)}`;
}

// A linked git worktree has a `.git` FILE (a "gitdir:" pointer); the primary
// checkout has a `.git` DIRECTORY. We only auto-isolate linked worktrees, so
// the primary checkout keeps the unchanged 5173 / "Multica Canary" defaults.
export function isLinkedWorktree(root) {
  try {
    return statSync(join(root, ".git")).isFile();
  } catch {
    return false;
  }
}

// scripts live at <root>/apps/desktop/scripts
export function repoRootFromScriptDir(scriptDir) {
  return join(scriptDir, "..", "..", "..");
}

// Populate DESKTOP_RENDERER_PORT / DESKTOP_APP_SUFFIX on `env` for a worktree
// checkout, without overriding values the caller set explicitly. Returns `env`.
export function applyWorktreeDevEnv(env, { root, log = false } = {}) {
  const hasPort = Boolean(env.DESKTOP_RENDERER_PORT);
  const hasSuffix = Boolean(env.DESKTOP_APP_SUFFIX);
  if (hasPort && hasSuffix) return env; // explicit overrides win outright
  if (!isLinkedWorktree(root)) return env; // primary checkout → keep defaults

  if (!hasPort) env.DESKTOP_RENDERER_PORT = String(rendererPortForPath(root));
  if (!hasSuffix) env.DESKTOP_APP_SUFFIX = appSuffixForPath(root);

  if (log) {
    console.log(
      `[dev:desktop] worktree isolation → renderer port ${env.DESKTOP_RENDERER_PORT}, ` +
        `app "Multica Canary ${env.DESKTOP_APP_SUFFIX}"`,
    );
  }
  return env;
}
