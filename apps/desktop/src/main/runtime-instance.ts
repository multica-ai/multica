import { randomUUID } from "node:crypto";
import { chmod, lstat, mkdir, readFile, rename, rm, symlink, writeFile } from "node:fs/promises";
import { join, resolve } from "node:path";
import { z } from "zod";
import { readTerminalCliVersion } from "./terminal-cli-version";

export const DESKTOP_RUNTIME_PROFILE = "desktop-runtime";
const runId = z.string().regex(/^[a-f0-9]{32}$/);
const commit = z.string().regex(/^[a-f0-9]{7,40}$/);
const apiOrigin = z.string().url().refine((value) => {
  try { return origin(value) === value; } catch { return false; }
});
const launchSchema = z.object({
  schemaVersion: z.literal(1), instanceId: z.literal("desktop"), ownerId: runId,
  kind: z.enum(["development", "packaged"]),
  appPath: z.string(), cliPath: z.string(), electronPath: z.string(),
  claudeEntryPath: z.string(),
  cliVersion: z.string().min(1), cliCommit: commit,
  profile: z.literal(DESKTOP_RUNTIME_PROFILE), apiUrl: apiOrigin, protocolVersion: z.literal(2),
});
export const runtimeRecordSchema = z.object({
  schemaVersion: z.literal(1), instanceId: z.literal("desktop"), ownerId: runId, runId,
  pid: z.number().int().positive(), port: z.number().int().min(1).max(65535),
  profile: z.literal(DESKTOP_RUNTIME_PROFILE), apiUrl: apiOrigin,
  cliVersion: z.string().min(1), cliCommit: commit,
  apiVersion: z.literal(2), sidecarProtocolVersion: z.literal(2),
  localConversations: z.boolean(), startedAt: z.string().datetime({ offset: true }),
});
export type RuntimeRecord = z.infer<typeof runtimeRecordSchema>;
export type RuntimeLaunch = z.infer<typeof launchSchema>;

const healthSchema = z.object({
  instance_id: z.literal("desktop"), instance_owner_id: runId, instance_run_id: runId,
  pid: z.number().int().positive(), profile: z.literal(DESKTOP_RUNTIME_PROFILE),
  server_url: z.string(), cli_version: z.string(), cli_commit: commit,
  status: z.string(), os: z.string().optional(), uptime: z.string().optional(),
  daemon_id: z.string().optional(), device_name: z.string().optional(),
  active_task_count: z.number().int().nonnegative().optional(),
  local_busy_count: z.number().int().nonnegative().optional(),
  // A daemon that has registered no workspaces (e.g. a verification runtime) reports null.
  agents: z.array(z.string()).optional(), workspaces: z.array(z.unknown()).nullable().optional(),
}).passthrough();
export type RuntimeHealth = z.infer<typeof healthSchema>;

export interface RuntimeSource {
  kind: "development" | "packaged";
  appPath: string;
  cliPath: string;
  electronPath: string;
  /** Per-conversation Claude adapter for the daemon's local conversation service. */
  claudeEntryPath: string;
}

function origin(value: string): string {
  const url = new URL(value);
  if (!["http:", "https:"].includes(url.protocol) || url.username || url.password || url.search || url.hash || url.pathname !== "/") {
    throw new Error("Runtime requires an HTTP or HTTPS API origin");
  }
  return url.origin;
}

export async function readPrivateRegistration(path: string): Promise<string> {
  const [file, directory] = await Promise.all([lstat(path), lstat(resolve(path, ".."))]);
  if (!file.isFile() || file.isSymbolicLink() || file.size > 65536 || !directory.isDirectory() || directory.isSymbolicLink()
    || (process.platform !== "win32" && ((file.mode & 0o077) !== 0 || (directory.mode & 0o077) !== 0))) {
    throw new Error("Runtime registration must be private");
  }
  return readFile(path, "utf8");
}

export async function writePrivateRegistration(path: string, value: unknown): Promise<void> {
  const directory = resolve(path, "..");
  await mkdir(directory, { recursive: true, mode: 0o700 });
  const info = await lstat(directory);
  if (!info.isDirectory() || info.isSymbolicLink()) throw new Error("Invalid runtime registration directory");
  await chmod(directory, 0o700);
  const temporary = join(directory, `.registration-${randomUUID()}`);
  try {
    await writeFile(temporary, JSON.stringify(value) + "\n", { mode: 0o600, flag: "wx" });
    await rename(temporary, path);
  } finally { await rm(temporary, { force: true }); }
}

/** One machine-wide Desktop registration. Only the last Desktop may manage it. */
export class RuntimeInstanceRegistry {
  readonly ownerId = randomUUID().replaceAll("-", "");
  readonly directory: string;
  readonly cliLink: string;

  constructor(home: string, readonly source: RuntimeSource) {
    this.directory = join(home, ".multica", "desktop-runtime");
    this.cliLink = join(this.directory, "cli");
  }

  async launch(): Promise<RuntimeLaunch> {
    return launchSchema.parse(JSON.parse(await readPrivateRegistration(join(this.directory, "launch.json"))));
  }

  async assertOwner(apiUrl?: string): Promise<RuntimeLaunch> {
    const launch = await this.launch();
    if (launch.ownerId !== this.ownerId || (apiUrl && launch.apiUrl !== origin(apiUrl))) {
      throw new Error("Another Desktop has taken over this runtime. Use the most recently started Desktop.");
    }
    return launch;
  }

  async owns(apiUrl: string): Promise<boolean> {
    try { await this.assertOwner(apiUrl); return true; } catch { return false; }
  }

  async register(apiUrl: string): Promise<RuntimeLaunch> {
    const version = await readTerminalCliVersion(this.source.cliPath);
    if (!version?.commit) throw new Error("The bundled CLI build cannot be verified. Rebuild this Desktop instance.");
    const record: RuntimeLaunch = {
      schemaVersion: 1, instanceId: "desktop", ownerId: this.ownerId, profile: DESKTOP_RUNTIME_PROFILE,
      apiUrl: origin(apiUrl), kind: this.source.kind, appPath: resolve(this.source.appPath),
      cliPath: resolve(this.source.cliPath), cliVersion: version.version, cliCommit: version.commit,
      electronPath: resolve(this.source.electronPath), claudeEntryPath: resolve(this.source.claudeEntryPath), protocolVersion: 2,
    };
    await writePrivateRegistration(join(this.directory, "launch.json"), record);
    // The terminal command follows the latest registration, including another worktree.
    if (process.platform !== "win32") {
      const temporary = join(this.directory, `.cli-${randomUUID()}`);
      try {
        await symlink(record.cliPath, temporary);
        await rename(temporary, this.cliLink);
      } finally { await rm(temporary, { force: true }); }
    }
    return record;
  }

  async refresh(apiUrl: string): Promise<RuntimeLaunch> {
    await this.assertOwner(apiUrl);
    return this.register(apiUrl);
  }

  async runtime(): Promise<RuntimeRecord> {
    return runtimeRecordSchema.parse(JSON.parse(await readPrivateRegistration(join(this.directory, "runtime.json"))));
  }

  private async probe(r: RuntimeRecord, request: typeof fetch, timeoutMs: number): Promise<RuntimeHealth | null> {
    try {
      const response = await request(`http://127.0.0.1:${r.port}/health`, { signal: AbortSignal.timeout(timeoutMs), redirect: "error" });
      if (!response.ok) return null;
      const parsed = healthSchema.safeParse(await response.json());
      if (!parsed.success) return null;
      const value = parsed.data;
      if (value.instance_id !== r.instanceId || value.instance_owner_id !== r.ownerId || value.instance_run_id !== r.runId
        || value.pid !== r.pid || value.profile !== r.profile || value.cli_version !== r.cliVersion
        || value.cli_commit !== r.cliCommit || value.server_url !== r.apiUrl) return null;
      return value;
    } catch { return null; }
  }

  async health(apiUrl: string, request: typeof fetch = fetch, timeoutMs = 2000): Promise<RuntimeHealth | null> {
    try {
      await this.assertOwner(apiUrl);
      return await this.probe(await this.runtime(), request, timeoutMs);
    } catch { return null; }
  }

  async endpoint(apiUrl: string, request: typeof fetch = fetch): Promise<{ base: string; token: string }> {
    const launch = await this.assertOwner(apiUrl);
    const r = await this.runtime();
    if (!await this.probe(r, request, 2000)) throw new Error("The registered runtime is offline or its identity changed");
    if (r.ownerId !== this.ownerId || r.apiUrl !== launch.apiUrl) throw new Error("The previous runtime is still being replaced");
    if (r.cliVersion !== launch.cliVersion || !(r.cliCommit.startsWith(launch.cliCommit) || launch.cliCommit.startsWith(r.cliCommit))) {
      throw new Error("Desktop and its running CLI build differ. Restart the runtime when idle.");
    }
    if (!r.localConversations) throw new Error("The registered runtime has no local conversation service");
    const token = (await readPrivateRegistration(join(this.directory, "local-api.token"))).trim();
    if (!/^[a-f0-9]{64}$/.test(token)) throw new Error("Invalid runtime connection credential");
    const current = await this.runtime();
    await this.assertOwner(apiUrl);
    if (current.runId !== r.runId || current.pid !== r.pid || current.port !== r.port) throw new Error("The runtime restarted while connecting; retry the connection");
    return { base: `127.0.0.1:${r.port}`, token };
  }
}
