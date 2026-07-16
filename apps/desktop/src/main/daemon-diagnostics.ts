import { constants } from "fs";
import { open, lstat } from "fs/promises";
import { join } from "path";
import { homedir } from "os";
import { readBoundedDaemonJSON } from "./daemon-json";

const OPERATOR_CREDENTIAL_HEADER = "X-Multica-Shutdown-Credential";
const MAX_CREDENTIAL_BYTES = 256;
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
  status: "running" | "starting";
  os: string;
  pid: number;
  uptime: string;
  daemon_id: string;
  device_name: string;
  server_url: string;
  cli_version: string;
  active_task_count: number;
  agents: string[];
  workspaces: Array<{ id: string; runtimes: string[] }>;
}

function isWorkspace(value: unknown): value is { id: string; runtimes: string[] } {
  if (!value || typeof value !== "object" || Array.isArray(value)) return false;
  const workspace = value as Record<string, unknown>;
  const keys = Object.keys(workspace);
  return (
    keys.length === 2 &&
    keys.includes("id") &&
    keys.includes("runtimes") &&
    typeof workspace.id === "string" &&
    Array.isArray(workspace.runtimes) &&
    workspace.runtimes.every((runtime) => typeof runtime === "string")
  );
}

function isDiagnosticsPayload(value: unknown): value is DiagnosticsPayload {
  if (!value || typeof value !== "object" || Array.isArray(value)) return false;
  const payload = value as Record<string, unknown>;
  return (
    Object.keys(payload).length === DIAGNOSTICS_FIELDS.size &&
    Object.keys(payload).every((key) => DIAGNOSTICS_FIELDS.has(key)) &&
    (payload.status === "running" || payload.status === "starting") &&
    typeof payload.os === "string" &&
    typeof payload.pid === "number" &&
    Number.isSafeInteger(payload.pid) &&
    payload.pid > 0 &&
    typeof payload.uptime === "string" &&
    typeof payload.daemon_id === "string" &&
    typeof payload.device_name === "string" &&
    typeof payload.server_url === "string" &&
    typeof payload.cli_version === "string" &&
    typeof payload.active_task_count === "number" &&
    Number.isSafeInteger(payload.active_task_count) &&
    payload.active_task_count >= 0 &&
    Array.isArray(payload.agents) &&
    payload.agents.every((agent) => typeof agent === "string") &&
    Array.isArray(payload.workspaces) &&
    payload.workspaces.every(isWorkspace)
  );
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
    const payload = await readBoundedDaemonJSON(res, controller.signal);
    return isDiagnosticsPayload(payload) ? payload : null;
  } catch {
    return null;
  } finally {
    clearTimeout(timeout);
  }
}
