import { constants } from "fs";
import { open, lstat } from "fs/promises";
import { join } from "path";
import { homedir } from "os";

const OPERATOR_CREDENTIAL_HEADER = "X-Multica-Shutdown-Credential";
const MAX_CREDENTIAL_BYTES = 256;
const MAX_DIAGNOSTICS_BYTES = 1 << 20;
const DIAGNOSTICS_FIELDS = new Set([
  "status",
  "os",
  "pid",
  "uptime",
  "daemon_id",
  "device_name",
  "server_url",
  "cli_version",
  "active_task_count",
  "agents",
  "workspaces",
]);

export interface DiagnosticsPayload {
  status?: string;
  os?: string;
  pid?: number;
  uptime?: string;
  daemon_id?: string;
  device_name?: string;
  server_url?: string;
  cli_version?: string;
  active_task_count?: number;
  agents?: string[];
  workspaces?: unknown[];
}

function isDiagnosticsPayload(value: unknown): value is DiagnosticsPayload {
  if (!value || typeof value !== "object" || Array.isArray(value)) return false;
  const payload = value as Record<string, unknown>;
  return (
    Object.keys(payload).every((key) => DIAGNOSTICS_FIELDS.has(key)) &&
    (payload.status === "running" || payload.status === "starting") &&
    (payload.os === undefined || typeof payload.os === "string") &&
    (payload.pid === undefined || typeof payload.pid === "number") &&
    (payload.uptime === undefined || typeof payload.uptime === "string") &&
    (payload.daemon_id === undefined || typeof payload.daemon_id === "string") &&
    (payload.device_name === undefined || typeof payload.device_name === "string") &&
    (payload.server_url === undefined || typeof payload.server_url === "string") &&
    (payload.cli_version === undefined || typeof payload.cli_version === "string") &&
    (payload.active_task_count === undefined ||
      (typeof payload.active_task_count === "number" &&
        Number.isSafeInteger(payload.active_task_count) &&
        payload.active_task_count >= 0)) &&
    (payload.agents === undefined ||
      (Array.isArray(payload.agents) &&
        payload.agents.every((agent) => typeof agent === "string"))) &&
    (payload.workspaces === undefined || Array.isArray(payload.workspaces))
  );
}

function rejectDuplicateJSONKeys(source: string): void {
  let offset = 0;

  function skipWhitespace(): void {
    while (/\s/.test(source[offset] ?? "")) offset += 1;
  }

  function parseString(): string {
    const start = offset;
    if (source[offset++] !== '"') throw new SyntaxError("expected string");
    while (offset < source.length) {
      const char = source[offset++];
      if (char === '"') return JSON.parse(source.slice(start, offset)) as string;
      if (char === "\\") {
        offset += 1;
      } else if (char < " ") {
        throw new SyntaxError("invalid control character");
      }
    }
    throw new SyntaxError("unterminated string");
  }

  function parseValue(): void {
    skipWhitespace();
    const char = source[offset];
    if (char === "{") {
      parseObject();
      return;
    }
    if (char === "[") {
      parseArray();
      return;
    }
    if (char === '"') {
      parseString();
      return;
    }
    const match = source
      .slice(offset)
      .match(/^(?:true|false|null|-?(?:0|[1-9]\d*)(?:\.\d+)?(?:[eE][+-]?\d+)?)/);
    if (!match) throw new SyntaxError("invalid JSON value");
    offset += match[0].length;
  }

  function parseObject(): void {
    offset += 1;
    const keys = new Set<string>();
    skipWhitespace();
    if (source[offset] === "}") {
      offset += 1;
      return;
    }
    while (true) {
      skipWhitespace();
      const key = parseString();
      if (keys.has(key)) throw new SyntaxError("duplicate JSON key");
      keys.add(key);
      skipWhitespace();
      if (source[offset++] !== ":") throw new SyntaxError("expected colon");
      parseValue();
      skipWhitespace();
      const separator = source[offset++];
      if (separator === "}") return;
      if (separator !== ",") throw new SyntaxError("expected object separator");
    }
  }

  function parseArray(): void {
    offset += 1;
    skipWhitespace();
    if (source[offset] === "]") {
      offset += 1;
      return;
    }
    while (true) {
      parseValue();
      skipWhitespace();
      const separator = source[offset++];
      if (separator === "]") return;
      if (separator !== ",") throw new SyntaxError("expected array separator");
    }
  }

  parseValue();
  skipWhitespace();
  if (offset !== source.length) throw new SyntaxError("trailing JSON content");
}

async function readBoundedJSON(response: Response): Promise<unknown> {
  const declaredLength = response.headers.get("content-length");
  if (declaredLength !== null) {
    const parsedLength = Number(declaredLength);
    if (!Number.isSafeInteger(parsedLength) || parsedLength < 0 || parsedLength > MAX_DIAGNOSTICS_BYTES) {
      throw new Error("invalid diagnostics content length");
    }
  }
  if (!response.body) throw new Error("missing diagnostics response body");

  const reader = response.body.getReader();
  const chunks: Uint8Array[] = [];
  let total = 0;
  try {
    while (true) {
      const { done, value } = await reader.read();
      if (done) break;
      total += value.byteLength;
      if (total > MAX_DIAGNOSTICS_BYTES) {
        await reader.cancel();
        throw new Error("diagnostics response exceeds 1 MiB");
      }
      chunks.push(value);
    }
  } finally {
    reader.releaseLock();
  }

  const bytes = new Uint8Array(total);
  let position = 0;
  for (const chunk of chunks) {
    bytes.set(chunk, position);
    position += chunk.byteLength;
  }
  const source = new TextDecoder("utf-8", { fatal: true }).decode(bytes);
  rejectDuplicateJSONKeys(source);
  return JSON.parse(source) as unknown;
}

export function profileOperatorCredentialPath(profile: string): string {
  const dir = profile
    ? join(homedir(), ".multica", "profiles", profile)
    : join(homedir(), ".multica");
  return join(dir, "daemon.shutdown-token");
}

async function readOperatorCredential(profile: string): Promise<string | null> {
  const path = profileOperatorCredentialPath(profile);
  let before;
  try {
    before = await lstat(path);
  } catch {
    return null;
  }
  if (!before.isFile() || before.isSymbolicLink()) return null;
  if (process.platform !== "win32" && (before.mode & 0o077) !== 0) return null;
  if (before.size <= 0 || before.size > MAX_CREDENTIAL_BYTES) return null;

  let file;
  try {
    const flags =
      process.platform === "win32"
        ? constants.O_RDONLY
        : constants.O_RDONLY | constants.O_NOFOLLOW;
    file = await open(path, flags);
    const after = await file.stat();
    if (!after.isFile()) return null;
    if (before.dev !== after.dev || before.ino !== after.ino) return null;
    if (process.platform !== "win32" && (after.mode & 0o077) !== 0) return null;
    if (after.size <= 0 || after.size > MAX_CREDENTIAL_BYTES) return null;
    const raw = await file.readFile({ encoding: "utf-8" });
    if (Buffer.byteLength(raw) > MAX_CREDENTIAL_BYTES) return null;
    const credential = raw.trim();
    if (!/^[A-Za-z0-9_-]+$/.test(credential)) return null;
    const decoded = Buffer.from(credential, "base64url");
    if (decoded.length !== 32 || decoded.toString("base64url") !== credential) {
      return null;
    }
    return credential;
  } catch {
    return null;
  } finally {
    await file?.close().catch(() => {});
  }
}

export async function fetchDaemonDiagnostics(
  profile: string,
  port: number,
): Promise<DiagnosticsPayload | null> {
  const credential = await readOperatorCredential(profile);
  if (!credential) return null;

  const controller = new AbortController();
  const timeout = setTimeout(() => controller.abort(), 2_000);
  try {
    const res = await fetch(`http://127.0.0.1:${port}/diagnostics`, {
      headers: { [OPERATOR_CREDENTIAL_HEADER]: credential },
      redirect: "error",
      signal: controller.signal,
    });
    if (!res.ok) return null;
    const payload = await readBoundedJSON(res);
    return isDiagnosticsPayload(payload) ? payload : null;
  } catch {
    return null;
  } finally {
    clearTimeout(timeout);
  }
}
