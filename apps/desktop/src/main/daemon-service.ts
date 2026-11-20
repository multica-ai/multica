import { createHash } from "crypto";
import { readFile } from "fs/promises";
import { homedir } from "os";
import { join } from "path";

export function backendServiceURL(raw: string): string {
  const url = new URL(raw.trim());
  if (url.protocol === "ws:") url.protocol = "http:";
  if (url.protocol === "wss:") url.protocol = "https:";
  if (!["http:", "https:"].includes(url.protocol) || url.username || url.password) throw new Error("Invalid backend URL");
  // Match Go's escaped-path serialization before hashing the connection key.
  const pathname = decodeURIComponent(url.pathname).replace(/\/+$/, "");
  url.pathname = pathname === "/ws"
    ? "/"
    : encodeURI(pathname).replace(/[!'()*?#]/g, (char) => "%" + char.charCodeAt(0).toString(16).toUpperCase());
  url.search = "";
  url.hash = "";
  return url.toString().replace(/\/+$/, "");
}

export interface ServiceHealth {
  status?: string; pid?: number; profile?: string; owner_id?: string;
  daemon_id?: string; server_url?: string; launched_by?: string;
}
interface ServiceRecord {
  server_url: string; owner_id: string; profile: string;
  daemon_id: string; port: number; pid: number;
}
export function parseServiceRecord(raw: string): ServiceRecord {
  const value: unknown = JSON.parse(raw);
  if (!value || typeof value !== "object") throw new Error("Invalid execution service record");
  const r = value as Record<string, unknown>;
  if (typeof r.server_url !== "string" || typeof r.owner_id !== "string" || !r.owner_id ||
      typeof r.profile !== "string" || typeof r.daemon_id !== "string" ||
      typeof r.port !== "number" || !Number.isInteger(r.port) || r.port < 1 || r.port > 65535 ||
      typeof r.pid !== "number" || !Number.isSafeInteger(r.pid) || r.pid < 1) throw new Error("Invalid execution service identity");
  return r as unknown as ServiceRecord;
}

// Only inspect the backend's public connection record. Never read or mutate
// another launcher's config, credentials or session files.
export async function probeBackendService<T extends ServiceHealth>(
  serverURL: string,
  probe: (port: number) => Promise<T | null>,
  readRecord: (path: string) => Promise<string> = (path) => readFile(path, "utf-8"),
): Promise<T | null> {
  const normalized = backendServiceURL(serverURL);
  const key = createHash("sha256").update(normalized).digest("hex");
  let raw: string;
  try { raw = await readRecord(join(homedir(), ".multica", "services", key, "active.json")); }
  catch (error) {
    if ((error as NodeJS.ErrnoException).code === "ENOENT") return null;
    throw error;
  }
  const record = parseServiceRecord(raw);
  if (record.server_url !== normalized) throw new Error("Execution service backend mismatch");
  const health = await probe(record.port);
  if (!health || !["running", "starting"].includes(health.status ?? "")) return null;
  if (health.pid !== record.pid || health.profile !== record.profile || health.owner_id !== record.owner_id ||
      health.daemon_id !== record.daemon_id || !health.server_url || backendServiceURL(health.server_url) !== normalized) {
    throw new Error("Execution service identity changed; refusing automatic adoption");
  }
  return health;
}

// Unlike Desktop auth profile arguments, an observed service may explicitly
// belong to the default CLI profile. Preserve the empty value as an argument.
export function serviceProfileArgs(profile: string): string[] {
 if (typeof profile !== "string" || profile.replace(/\\/g,"/").split("/").some((part)=>part===".." || part===".") || profile.startsWith("/") || /^[A-Za-z]:/.test(profile)) throw new Error("Invalid execution profile");
 return ["--profile",profile];
}

export function isSharedExecutionService(health: ServiceHealth | null, desktopProfile: string): boolean {
 return !!health && ((health.profile !== undefined && health.profile !== desktopProfile) || (!!health.owner_id && health.launched_by !== "desktop"));
}

export function serviceLogPath(profile:string): string {
 serviceProfileArgs(profile);
 return profile ? join(homedir(),".multica","profiles",profile,"daemon.log") : join(homedir(),".multica","daemon.log");
}

// Keep an existing host-based auth profile when it already belongs to this
// backend. Different deployments on that host must not overwrite its token.
export function desktopAuthProfile(serverURL:string, legacyName:string, legacyBackend?:unknown): string {
 if (typeof legacyBackend!=="string" || !legacyBackend) return legacyName;
 if (backendServiceURL(serverURL)===backendServiceURL(legacyBackend)) return legacyName;
 return legacyName+"-"+createHash("sha256").update(backendServiceURL(serverURL)).digest("hex").slice(0,12);
}
