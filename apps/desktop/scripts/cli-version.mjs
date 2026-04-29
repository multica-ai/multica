import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const repoRoot = resolve(here, "..", "..", "..");

export const CLI_VERSION_FILE = resolve(repoRoot, "release", "cli-version.txt");

const CLI_VERSION_RE = /^v?\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?$/;

export function normalizeCliVersion(raw) {
  if (!raw) return null;
  const trimmed = String(raw).trim();
  if (!CLI_VERSION_RE.test(trimmed)) return null;
  return trimmed.replace(/^v/, "");
}

export function readCliVersionRaw() {
  try {
    const raw = readFileSync(CLI_VERSION_FILE, "utf8").trim();
    return CLI_VERSION_RE.test(raw) ? raw : null;
  } catch {
    return null;
  }
}

export function readCliVersionNormalized() {
  const raw = readCliVersionRaw();
  return raw ? raw.replace(/^v/, "") : null;
}
