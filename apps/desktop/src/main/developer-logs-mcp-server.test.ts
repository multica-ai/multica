// @vitest-environment node
import { mkdir, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { InMemoryTransport } from "@modelcontextprotocol/sdk/inMemory.js";
import { afterEach, describe, expect, it } from "vitest";
import { createLocalTiming } from "@multica/tutti-agent-gui-host/local-timing";
import { createDeveloperLogsMcpServer, writeDeveloperLogsManifest } from "./developer-logs-mcp-server";
import { LocalTimingLog } from "./local-timing-log";
import { WebDiagnosticLog } from "./developer-runtime-logs";

const cleanups: (() => Promise<unknown>)[] = [];
afterEach(async () => { for (const cleanup of cleanups.splice(0).reverse()) await cleanup(); });
const runtime = { appVersion: "test", platform: "test", arch: "test", electronVersion: "test", chromeVersion: "test", nodeVersion: "test", packaged: false };

async function fixture() {
  const root = await mkdtemp(join(tmpdir(), "multica-logs-mcp-"));
  cleanups.push(() => rm(root, { recursive: true, force: true }));
  const diagnostics = join(root, "diagnostics");
  const client = new Client({ name: "test", version: "1" });
  const [clientSide, serverSide] = InMemoryTransport.createLinkedPair();
  await createDeveloperLogsMcpServer(diagnostics).connect(serverSide);
  await client.connect(clientSide);
  cleanups.push(() => client.close());
  const call = async () => {
    const result = await client.callTool({ name: "export_developer_logs", arguments: { since_minutes: 60 } });
    const text = (result.content as { text: string }[])[0]!.text;
    return { isError: result.isError === true, text };
  };
  return { root, diagnostics, client, call };
}

describe("standalone developer logs MCP", () => {
  it("asks for the desktop when it has not published diagnostics", async () => {
    const f = await fixture();
    expect((await f.client.listTools()).tools.map((tool) => tool.name)).toEqual(["export_developer_logs"]);
    expect(await f.call()).toEqual({ isError: true, text: expect.stringContaining("Open Multica Desktop") });
  });

  it("exports device-wide timings, the published account's Web log and the daemon tail", async () => {
    const f = await fixture();
    const timing = new LocalTimingLog(f.diagnostics);
    createLocalTiming({ source: "main", conversationId: "first", provider: "codex" }, timing.write).mark("provider.first_text");
    createLocalTiming({ source: "main", conversationId: "second", provider: "claude" }, timing.write).mark("provider.first_text");
    await timing.flush();
    const web = new WebDiagnosticLog(f.diagnostics, "https://api.test", "user");
    web.console({ level: "error", message: "renderer failed token=secret" });
    await web.flush();
    const daemonLog = join(f.root, "daemon.log");
    await writeFile(daemonLog, "task started\nAuthorization: Bearer abc\n");
    await writeDeveloperLogsManifest(f.diagnostics, { version: 1, apiUrl: "https://api.test", userId: "user", daemonLogPath: daemonLog, runtime });

    const result = await f.call();
    expect(result.isError).toBe(false);
    const summary = JSON.parse(result.text);
    expect(summary).toMatchObject({ timingRecordCount: 2, conversationCount: 2, webRecordCount: 1, filter: { conversationId: null, timingScope: "device" }, sources: { web: { status: "available" }, daemon: { status: "available" } } });
    expect(summary.path.startsWith(join(f.diagnostics, "exports"))).toBe(true);
    const bundle = await readFile(summary.path, "utf8");
    expect(JSON.parse(bundle)).toMatchObject({ workspaceId: null, runtime });
    expect(bundle).not.toContain("secret");
    expect(bundle).not.toContain("Bearer abc");
  });

  it("omits the Web log while signed out", async () => {
    const f = await fixture();
    await writeDeveloperLogsManifest(f.diagnostics, { version: 1, apiUrl: "https://api.test", userId: null, daemonLogPath: null, runtime });
    expect(JSON.parse((await f.call()).text)).toMatchObject({ webRecordCount: 0, sources: { web: { status: "unavailable" }, daemon: { status: "unavailable" } } });
  });
  it("merges the daemon timing file the manifest names and reports its dropped writes", async () => {
    const f = await fixture();
    const daemonTiming = join(f.root, "daemon-timing.jsonl");
    const record = { version: 2, source: "daemon", at: new Date().toISOString(), event: "mark", elapsedMs: 1 };
    await writeFile(daemonTiming, [{ ...record, stage: "provider.first_text", conversationId: "first", turnId: "t", requestId: "r", provider: "codex" }, { ...record, stage: "timing.dropped", count: 2 }, { ...record, stage: "link.start" }].map((line) => JSON.stringify(line)).join("\n") + "\n");
    await writeDeveloperLogsManifest(f.diagnostics, { version: 1, apiUrl: "https://api.test", userId: null, daemonLogPath: null, daemonTimingPath: daemonTiming, runtime });
    const summary = JSON.parse((await f.call()).text);
    expect(summary).toMatchObject({ daemonTimingRecordCount: 2, incomplete: true, waterfallCount: 1, sources: { daemonTiming: { status: "available" } } });
    const bundle = JSON.parse(await readFile(summary.path, "utf8"));
    expect(bundle.coverage.droppedTimingWrites).toBe(2);
    expect(bundle.waterfalls.turns[0]).toMatchObject({ turnId: "t", provider: "codex" });
  });

  it("reads a manifest an older Desktop published without the daemon timing path", async () => {
    const f = await fixture();
    await mkdir(f.diagnostics, { recursive: true });
    await writeFile(join(f.diagnostics, "developer-logs-mcp.json"), JSON.stringify({ version: 1, apiUrl: "https://api.test", userId: null, daemonLogPath: null, runtime }));
    expect(JSON.parse((await f.call()).text)).toMatchObject({ daemonTimingRecordCount: 0, sources: { daemonTiming: { status: "unavailable" } } });
  });
});
