import { readFile } from "fs/promises";
import { homedir } from "os";
import { join } from "path";

/**
 * Where the supervisor is configured. Deliberately outside the repo: a packaged
 * app has no idea where the checkout lives, and this machine-specific pointer
 * must never be committed.
 */
export function localStackConfigPath(): string {
  return join(homedir(), ".multica", "desktop-local-stack.json");
}

export interface LocalStackConfig {
  repoDir: string;
  composeFile: string;
  backendPort: number;
}

const DEFAULT_COMPOSE_FILE = "docker-compose.selfhost.yml";
// Matches the compose file's own default (`${BACKEND_PORT:-8080}`). Installs
// that moved the backend off 8080 set `backendPort` in the config file.
const DEFAULT_BACKEND_PORT = 8080;

/**
 * Reads the supervisor config. Returns null when the file does not exist, which
 * is the "supervisor disabled" signal — the app then behaves exactly as it does
 * without this feature.
 *
 * A present file that is not a JSON object, or that is missing `repoDir`,
 * throws: silently ignoring those would look identical to the feature being
 * off. `composeFile` and `backendPort` are optional and fall back to the
 * defaults below whenever they are absent *or* of the wrong type, so a typo in
 * either one is not reported.
 */
export async function loadLocalStackConfig(
  path: string,
): Promise<LocalStackConfig | null> {
  let raw: string;
  try {
    raw = await readFile(path, "utf-8");
  } catch (err) {
    if (isMissingFileError(err)) return null;
    throw err;
  }

  const parsed: unknown = JSON.parse(raw);
  if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) {
    throw new Error("local stack config: expected a JSON object");
  }
  const obj = parsed as Record<string, unknown>;

  const repoDir = obj.repoDir;
  if (typeof repoDir !== "string" || repoDir.length === 0) {
    throw new Error("local stack config: repoDir is required");
  }

  const composeFile =
    typeof obj.composeFile === "string" && obj.composeFile.length > 0
      ? obj.composeFile
      : DEFAULT_COMPOSE_FILE;

  const backendPort =
    typeof obj.backendPort === "number" && Number.isInteger(obj.backendPort)
      ? obj.backendPort
      : DEFAULT_BACKEND_PORT;

  return { repoDir, composeFile, backendPort };
}

/**
 * Whether the app is pointed at a backend on this machine. This is the gate for
 * the whole supervisor: a build aimed at SaaS must never touch docker.
 */
export function isLocalApiUrl(apiUrl: string): boolean {
  let host: string;
  try {
    host = new URL(apiUrl).hostname;
  } catch {
    return false;
  }
  // URL normalizes [::1] to the bracket-less form in hostname on some runtimes.
  return host === "localhost" || host === "127.0.0.1" || host === "::1" || host === "[::1]";
}

function isMissingFileError(err: unknown): boolean {
  return Boolean(
    err &&
      typeof err === "object" &&
      "code" in err &&
      (err as NodeJS.ErrnoException).code === "ENOENT",
  );
}
