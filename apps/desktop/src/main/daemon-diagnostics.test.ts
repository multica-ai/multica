import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { chmod, mkdir, rm, symlink, writeFile } from "fs/promises";
import { dirname, join } from "path";
import {
  fetchDaemonDiagnostics,
  profileOperatorCredentialPath,
} from "./daemon-diagnostics";

const credential = Buffer.alloc(32, 7).toString("base64url");
let originalHome: string | undefined;
let home: string;

beforeEach(async () => {
  originalHome = process.env.HOME;
  home = join(process.cwd(), `.daemon-diagnostics-test-${process.pid}`);
  process.env.HOME = home;
  await rm(home, { recursive: true, force: true });
  vi.restoreAllMocks();
});

afterEach(async () => {
  process.env.HOME = originalHome;
  await rm(home, { recursive: true, force: true });
  vi.restoreAllMocks();
});

async function writeCredential(profile: string, value = credential) {
  const path = profileOperatorCredentialPath(profile);
  await mkdir(dirname(path), { recursive: true });
  await writeFile(path, `${value}\n`, { mode: 0o600 });
  if (process.platform !== "win32") await chmod(path, 0o600);
  return path;
}

describe("daemon diagnostics", () => {
  it("uses the exact default and named profile credential paths", () => {
    expect(profileOperatorCredentialPath("")).toBe(
      join(home, ".multica", "daemon.shutdown-token"),
    );
    expect(profileOperatorCredentialPath("desktop-localhost-8082")).toBe(
      join(
        home,
        ".multica",
        "profiles",
        "desktop-localhost-8082",
        "daemon.shutdown-token",
      ),
    );
  });

  it("authenticates diagnostics without exposing the credential in the URL", async () => {
    await writeCredential("test-profile");
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(
        JSON.stringify({
          status: "running",
          cli_version: "v9.9.9",
          active_task_count: 2,
        }),
        { status: 200, headers: { "Content-Type": "application/json" } },
      ),
    );
    vi.stubGlobal("fetch", fetchMock);

    const result = await fetchDaemonDiagnostics("test-profile", 19514);
    expect(result?.cli_version).toBe("v9.9.9");
    expect(fetchMock).toHaveBeenCalledOnce();
    const [url, options] = fetchMock.mock.calls[0];
    expect(url).toBe("http://127.0.0.1:19514/diagnostics");
    expect(url).not.toContain(credential);
    expect(options.redirect).toBe("error");
    expect(options.headers).toEqual({
      "X-Multica-Shutdown-Credential": credential,
    });
  });

  it("fails closed for invalid credential files", async () => {
    const fetchMock = vi.fn();
    vi.stubGlobal("fetch", fetchMock);

    await writeCredential("malformed", "not-base64url!");
    expect(await fetchDaemonDiagnostics("malformed", 19514)).toBeNull();

    if (process.platform !== "win32") {
      const broad = await writeCredential("broad");
      await chmod(broad, 0o644);
      expect(await fetchDaemonDiagnostics("broad", 19514)).toBeNull();

      const target = await writeCredential("target");
      const link = profileOperatorCredentialPath("link");
      await mkdir(dirname(link), { recursive: true });
      await symlink(target, link);
      expect(await fetchDaemonDiagnostics("link", 19514)).toBeNull();
    }

    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("fails closed for authentication, redirects, and malformed JSON", async () => {
    await writeCredential("test-profile");
    for (const response of [
      new Response("unauthorized", { status: 401 }),
      new Response("redirect", { status: 307 }),
      new Response("not json", { status: 200 }),
    ]) {
      vi.stubGlobal("fetch", vi.fn().mockResolvedValue(response));
      expect(await fetchDaemonDiagnostics("test-profile", 19514)).toBeNull();
    }
  });

  it("rejects oversized diagnostics responses", async () => {
    await writeCredential("test-profile");
    for (const response of [
      new Response("{}", {
        status: 200,
        headers: { "Content-Length": String((1 << 20) + 1) },
      }),
      new Response(`{"status":"running","padding":"${"x".repeat(1 << 20)}"}`, {
        status: 200,
      }),
    ]) {
      vi.stubGlobal("fetch", vi.fn().mockResolvedValue(response));
      expect(await fetchDaemonDiagnostics("test-profile", 19514)).toBeNull();
    }
  });

  it("rejects duplicate keys, trailing content, and unknown fields", async () => {
    await writeCredential("test-profile");
    for (const source of [
      '{"status":"running","status":"starting"}',
      '{"status":"running","workspaces":[{"id":1,"id":2}]}',
      '{"status":"running"} true',
      '{"status":"running","unexpected":true}',
      '{"status":"running","pid":1e400}',
    ]) {
      vi.stubGlobal(
        "fetch",
        vi.fn().mockResolvedValue(new Response(source, { status: 200 })),
      );
      expect(await fetchDaemonDiagnostics("test-profile", 19514)).toBeNull();
    }
  });

  it("fails closed for malformed diagnostics fields", async () => {
    await writeCredential("test-profile");
    for (const payload of [
      {},
      { status: "stopped" },
      { status: "running", active_task_count: -1 },
      { status: "running", active_task_count: 0.5 },
      { status: "running", pid: -1 },
      { status: "running", pid: 0.5 },
      { status: "running", agents: ["codex", 7] },
      { status: "running", workspaces: "workspace-id" },
      { status: "running", workspaces: [{ id: 7, runtimes: [] }] },
      { status: "running", workspaces: [{ id: "id", runtimes: [7] }] },
      { status: "running", workspaces: [{ id: "id", runtimes: [], extra: true }] },
    ]) {
      vi.stubGlobal(
        "fetch",
        vi.fn().mockResolvedValue(
          new Response(JSON.stringify(payload), {
            status: 200,
            headers: { "Content-Type": "application/json" },
          }),
        ),
      );
      expect(await fetchDaemonDiagnostics("test-profile", 19514)).toBeNull();
    }
  });
});
