// @vitest-environment node
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { chmod, mkdir, mkdtemp, readFile, readlink, rm, stat, symlink, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { RuntimeInstanceRegistry, readPrivateRegistration, runtimeRecordSchema, writePrivateRegistration, type RuntimeRecord, type RuntimeSource } from "./runtime-instance";

const version = vi.hoisted(() => vi.fn(async () => ({ version: "0.7.0", commit: "bcdfa010e" })));
vi.mock("./terminal-cli-version", () => ({ readTerminalCliVersion: version }));
let home: string;
let source: RuntimeSource;
let registry: RuntimeInstanceRegistry;
const apiUrl = "https://api.example.test";
const token = "a".repeat(64);

async function runtime(): Promise<RuntimeRecord> {
  const raw = await readFile(new URL("../../../../server/internal/runtimeinstance/testdata/runtime.json", import.meta.url), "utf8");
  return { ...runtimeRecordSchema.parse(JSON.parse(raw)), ownerId: registry.ownerId };
}
function health(r: RuntimeRecord) {
  return { instance_id: r.instanceId, instance_owner_id: r.ownerId, instance_run_id: r.runId, pid: r.pid, profile: r.profile,
    server_url: r.apiUrl, cli_version: r.cliVersion, cli_commit: r.cliCommit, status: "running", local_busy_count: 0 };
}
async function publish(r: RuntimeRecord) {
  await writePrivateRegistration(join(registry.directory, "runtime.json"), r);
  await writeFile(join(registry.directory, "local-api.token"), token + "\n", { mode: 0o600 });
}
beforeEach(async () => {
  version.mockResolvedValue({ version: "0.7.0", commit: "bcdfa010e" });
  home = await mkdtemp(join(tmpdir(), "multica-shared-runtime-"));
  source = { kind: "development", appPath: join(home, "worktree-a"), cliPath: join(home, "worktree-a/cli"), electronPath: process.execPath, claudeEntryPath: join(home, "worktree-a/claude-host.js") };
  registry = new RuntimeInstanceRegistry(home, source);
  await registry.register(apiUrl);
});
afterEach(async () => { await rm(home, { recursive: true, force: true }); });

describe("single Desktop runtime registration", () => {
  it("records the Claude adapter entry the daemon runs", async () => {
    expect(await registry.launch()).toMatchObject({ claudeEntryPath: join(home, "worktree-a/claude-host.js") });
  });

  it("atomically replaces the launch source and terminal pointer when another Desktop starts", async () => {
    const otherSource = { ...source, kind: "packaged" as const, appPath: join(home, "Multica.app"), cliPath: join(home, "Multica.app/cli") };
    const next = new RuntimeInstanceRegistry(home, otherSource);
    await next.register(apiUrl);
    expect(next.directory).toBe(registry.directory);
    expect(await next.launch()).toMatchObject({ instanceId: "desktop", ownerId: next.ownerId, kind: "packaged", cliPath: otherSource.cliPath, profile: "desktop-runtime" });
    if (process.platform !== "win32") expect(await readlink(next.cliLink)).toBe(otherSource.cliPath);
    await expect(registry.refresh(apiUrl)).rejects.toThrow("taken over");
    await expect(registry.assertOwner()).rejects.toThrow("taken over");
    expect((await next.launch()).ownerId).toBe(next.ownerId);
  });

  it("accepts a runtime that reports no registered workspaces as null", async () => {
    const r = await runtime(); await publish(r);
    const request = vi.fn(async () => Response.json({ ...health(r), workspaces: null }));
    expect(await registry.endpoint(apiUrl, request)).toEqual({ base: `127.0.0.1:${r.port}`, token });
  });

  it("uses the shared Go wire fixture and returns only a verified private endpoint", async () => {
    const r = await runtime(); await publish(r);
    const request = vi.fn(async () => Response.json(health(r)));
    expect(await registry.endpoint(apiUrl, request)).toEqual({ base: `127.0.0.1:${r.port}`, token });
    expect(request).toHaveBeenCalledWith(`http://127.0.0.1:${r.port}/health`, expect.objectContaining({ redirect: "error" }));
    const launchText = await readPrivateRegistration(join(registry.directory, "launch.json"));
    expect(launchText).not.toContain(token);
    if (process.platform !== "win32") {
      expect((await stat(registry.directory)).mode & 0o777).toBe(0o700);
      expect((await stat(join(registry.directory, "launch.json"))).mode & 0o777).toBe(0o600);
    }
  });

  it.each(["instance_run_id", "instance_owner_id", "pid", "profile", "cli_commit", "server_url"])("rejects a stale or foreign health response (%s)", async (field) => {
    const r = await runtime(); await publish(r);
    const request = vi.fn(async () => Response.json({ ...health(r), [field]: field === "pid" ? 999 : "wrong" }));
    expect(await registry.health(apiUrl, request)).toBeNull();
    await expect(registry.endpoint(apiUrl, request)).rejects.toThrow("identity changed");
  });

  it("can identify the previous process for graceful replacement but never routes conversations to it", async () => {
    const r = { ...await runtime(), ownerId: "3".repeat(32) }; await publish(r);
    const request = vi.fn(async () => Response.json(health(r)));
    expect(await registry.health(apiUrl, request)).toMatchObject({ instance_owner_id: r.ownerId });
    await expect(registry.endpoint(apiUrl, request)).rejects.toThrow("previous runtime");
    const newer = new RuntimeInstanceRegistry(home, source); await newer.register(apiUrl);
    expect(await registry.health(apiUrl, request)).toBeNull();
    await expect(registry.endpoint(apiUrl, request)).rejects.toThrow("taken over");
  });

  it("rejects an incompatible build and a restart that happens during discovery", async () => {
    const r = await runtime(); await publish(r);
    const request = vi.fn(async () => Response.json(health(r)));
    await registry.refresh(apiUrl);
    const wrong = { ...r, cliVersion: "0.6.0" }; await publish(wrong);
    await expect(registry.endpoint(apiUrl, vi.fn(async () => Response.json(health(wrong))))).rejects.toThrow("build differ");
    await publish(r);
    request.mockImplementation(async () => {
      await publish({ ...r, runId: "9".repeat(32) });
      return Response.json(health(r));
    });
    await expect(registry.endpoint(apiUrl, request)).rejects.toThrow("restarted while connecting");
  });

  it("rejects unsupported protocol, invalid tokens, public files, and symlink registrations", async () => {
    const r = await runtime(); await publish(r);
    const path = join(registry.directory, "runtime.json");
    await writePrivateRegistration(path, { ...r, sidecarProtocolVersion: 99 });
    await expect(registry.runtime()).rejects.toThrow();
    await publish(r);
    await writeFile(join(registry.directory, "local-api.token"), "invalid");
    await expect(registry.endpoint(apiUrl, vi.fn(async () => Response.json(health(r))))).rejects.toThrow("credential");
    if (process.platform !== "win32") {
      await chmod(path, 0o644);
      await expect(registry.runtime()).rejects.toThrow("private");
      await rm(path); await symlink(source.claudeEntryPath, path);
      await expect(registry.runtime()).rejects.toThrow("private");
    }
  });

  it("rejects credential-bearing origins before publishing anything", async () => {
    const previous = await registry.launch();
    await expect(registry.register("https://user:password@example.test")).rejects.toThrow("API origin");
    expect(await registry.launch()).toEqual(previous);
    const directory = join(home, "not-private"); await mkdir(directory, { mode: 0o755 });
    await writePrivateRegistration(join(directory, "record.json"), { ok: true });
    expect(JSON.parse(await readPrivateRegistration(join(directory, "record.json")))).toEqual({ ok: true });
  });
});
