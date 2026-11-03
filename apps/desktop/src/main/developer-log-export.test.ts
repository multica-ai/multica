// @vitest-environment node
import { createHash } from "node:crypto";
import { mkdtemp, mkdir, readFile, readdir, rm, stat, utimes, writeFile } from "node:fs/promises";
import { dirname, join } from "node:path";
import { tmpdir } from "node:os";
import { describe, expect, it, vi } from "vitest";
import type { LocalTimingRecord } from "@multica/tutti-agent-gui-host/local-timing";
import { LocalTimingLog, type LocalTimingSnapshot } from "./local-timing-log";
import { WebDiagnosticLog, snapshotDaemonLog } from "./developer-runtime-logs";
import { DeveloperLogExporter, buildDeveloperLogBundle, createDeveloperLogTool } from "./developer-log-export";

const scope = { userId: "user", workspaceId: "workspace" };
const signal = () => new AbortController().signal;
const runtime = { appVersion: "test-version", platform: "darwin", arch: "arm64", electronVersion: "test", chromeVersion: "test", nodeVersion: "test", packaged: false };
async function fixture() {
  const root = await mkdtemp(join(tmpdir(), "multica-export-"));
  const log = new LocalTimingLog(root);
  const selected = join(root, "selected.json");
  const daemonTimingPath = join(root, "daemon-timing.jsonl");
  const webLog = new WebDiagnosticLog(root, "https://test.invalid", scope.userId);
  const daemonPath = join(root, "daemon.log");
  let active = true;
  const options = {
    log, exportsRoot: join(root, "exports"), runtime, daemonTimingPath: () => daemonTimingPath,
    assertScope: () => { if (!active) throw new Error("Account is no longer active"); },
    conversations: vi.fn(async () => ["conversation", "second"]),
    runtimeLogs: vi.fn(async (_scope: typeof scope, since: number, until: number) => ({ web: await webLog.snapshot(since, until), daemon: await snapshotDaemonLog(daemonPath) })),
    chooseSavePath: vi.fn(async (): Promise<string | null> => selected),
  };
  const now = Date.now();
  const record = { version: 1, source: "main", stage: "provider.first_text", event: "mark", elapsedMs: 42, at: new Date(now).toISOString(), conversationId: "conversation", turnId: "turn", requestId: "request", provider: "codex", message: "NEVER_EXPORT", env: { token: "NEVER_EXPORT" } };
  for (const value of [record, { ...record, conversationId: "second" }, { ...record, conversationId: "foreign" }, { ...record, at: new Date(now - 2 * 86400000).toISOString() }]) log.write(value);
  return { root, log, webLog, daemonPath, daemonTimingPath, selected, options, exporter: new DeveloperLogExporter(options), deactivate: () => { active = false; }, cleanup: async () => { await log.flush(); await webLog.flush(); await rm(root, { recursive: true, force: true }); } };
}

describe("developer log export", () => {
  it("shares a sanitized, scoped snapshot between native save and MCP exports", async () => {
    const f = await fixture();
    try {
      const saved = await f.exporter.save(scope);
      const ui = JSON.parse(await readFile(f.selected, "utf8"));
      expect(saved).toEqual({ status: "saved", filename: "selected.json", recordCount: 2 });
      expect(ui.runtime.appVersion).toBe("test-version");
      expect(ui.records.map((record: { conversationId: string }) => record.conversationId)).toEqual(["conversation", "second"]);
      const exported = await f.exporter.export(scope, { sinceMinutes: 60, conversationId: "conversation" }, signal());
      const contents = await readFile(exported.path, "utf8");
      expect(exported.recordCount).toBe(1);
      expect(JSON.parse(contents).records[0]).toMatchObject({ turnId: "turn", stage: "provider.first_text" });
      expect(contents).not.toMatch(/NEVER_EXPORT|foreign|message|env/);
      expect(exported.stages).toEqual([{ stage: "provider.first_text", background: false, records: 1, waits: 0 }]);
      if (process.platform !== "win32") {
        expect((await stat(exported.path)).mode & 0o777).toBe(0o600);
        expect((await stat(f.selected)).mode & 0o777).toBe(0o600);
      }
    } finally { await f.cleanup(); }
  });
  it("separates background timings and exports duration percentiles through both entry points", async () => {
    const f = await fixture();
    try {
      for (const background of [false, true]) for (const durationMs of [10, 20, 100]) f.log.write({
        version: 1, source: "main", conversationId: "conversation", turnId: "turn", at: new Date().toISOString(),
        stage: "context.dispose", event: "end", elapsedMs: 100, durationMs, background,
      });
      const exported = await f.exporter.export(scope, { sinceMinutes: 60 }, signal());
      await f.exporter.save(scope);
      const saved = JSON.parse(await readFile(f.selected, "utf8"));
      expect(exported.stages).toEqual(saved.summary.stages);
      expect(exported.stages.filter((stage) => stage.stage === "context.dispose")).toEqual([false, true].map((background) => ({
        stage: "context.dispose", background, records: 3, waits: 0, samples: 3, p50DurationMs: 20, p90DurationMs: 100, p95DurationMs: 100, maxDurationMs: 100,
      })));
      const bundle = JSON.parse(await readFile(exported.path, "utf8"));
      expect(bundle.records.filter((record: { background?: boolean }) => record.background)).toHaveLength(3);
    } finally { await f.cleanup(); }
  });
  it("includes Web and daemon logs in the same UI and MCP bundle with honest source coverage", async () => {
    const f = await fixture();
    try {
      f.webLog.console({ level: "error", message: "fetch failed token=NEVER_EXPORT" });
      await writeFile(f.daemonPath, "12:34:56.789 ERR connection failed token=NEVER_EXPORT\n");
      await f.exporter.save(scope);
      const ui = JSON.parse(await readFile(f.selected, "utf8"));
      const result = await createDeveloperLogTool(f.exporter, scope, "conversation").call({ request_id: "combined" }, signal()) as { path: string };
      const mcp = JSON.parse(await readFile(result.path, "utf8"));
      for (const bundle of [ui, mcp]) {
        expect(bundle.schemaVersion).toBe(3);
        expect(bundle.sources.web).toMatchObject({ status: "available", scope: "account", timeFilterApplied: true, records: [{ level: "error", message: "fetch failed token=[REDACTED]" }] });
        expect(bundle.sources.daemon).toMatchObject({ status: "available", scope: "desktop-profile", timeFilterApplied: false, text: expect.stringContaining("connection failed") });
        expect(bundle.summary).toMatchObject({ webRecordCount: 1, daemonLineCount: 1 });
        expect(bundle.summary.recordCount).toBe(bundle.summary.timingRecordCount + 2);
        expect(JSON.stringify(bundle)).not.toContain("NEVER_EXPORT");
      }
      expect(mcp.records).toHaveLength(1);
      expect(ui.records).toHaveLength(2);
      await rm(f.daemonPath);
      const absent = await f.exporter.export(scope, { sinceMinutes: 60 }, signal());
      expect(absent.sources.daemon.status).toBe("missing");
    } finally { await f.cleanup(); }
  });
  it("still saves Web and timing records when the daemon source cannot be read", async () => {
    const f = await fixture();
    try {
      f.webLog.console({ level: "info", message: "Web ready" });
      await mkdir(f.daemonPath);
      await f.exporter.save(scope);
      const bundle = JSON.parse(await readFile(f.selected, "utf8"));
      expect(bundle.sources.web.status).toBe("available");
      expect(bundle.sources.daemon.status).toBe("unreadable");
      expect(bundle.coverage.incomplete).toBe(true);
      expect(bundle.records).toHaveLength(2);
    } finally { await f.cleanup(); }
  });
  it("rejects account changes while collecting runtime sources before saving", async () => {
    const f = await fixture();
    try {
      const readSources = f.options.runtimeLogs.getMockImplementation()!;
      f.options.runtimeLogs.mockImplementationOnce(async (...args) => {
        const sources = await readSources(...args);
        f.deactivate();
        return sources;
      });
      await expect(f.exporter.save(scope)).rejects.toThrow("Account is no longer active");
      expect(f.options.chooseSavePath).not.toHaveBeenCalled();
      expect(await readdir(f.root)).not.toContain("selected.json");
    } finally { await f.cleanup(); }
  });
  it("does not save when cancelled or when the account changes in the save dialog", async () => {
    const f = await fixture();
    try {
      f.options.chooseSavePath.mockResolvedValueOnce(null);
      expect(await f.exporter.save(scope)).toEqual({ status: "cancelled" });
      expect(await readdir(f.root)).not.toContain("selected.json");
      f.options.chooseSavePath.mockImplementationOnce(async () => { f.deactivate(); return f.selected; });
      await expect(f.exporter.save(scope)).rejects.toThrow("Account is no longer active");
      expect(await readdir(f.root)).not.toContain("selected.json");
    } finally { await f.cleanup(); }
  });
  it("rejects foreign conversations, out-of-range filters and cancelled calls before writing", async () => {
    const f = await fixture();
    try {
      await expect(f.exporter.export(scope, { sinceMinutes: 60, conversationId: "foreign" }, signal())).rejects.toThrow("unavailable");
      await expect(f.exporter.export(scope, { sinceMinutes: 10081 }, signal())).rejects.toThrow();
      await expect(f.exporter.export(scope, { sinceMinutes: 60 }, AbortSignal.abort())).rejects.toThrow();
      expect(await readdir(f.root)).not.toContain("exports");
    } finally { await f.cleanup(); }
  });
  it("keeps the save destination intact and reports a failure when saving cannot finish", async () => {
    const f = await fixture();
    try {
      f.options.chooseSavePath.mockResolvedValue(join(f.root, "missing", "out.json"));
      await expect(f.exporter.save(scope)).rejects.toThrow("Could not save developer logs");
      expect((await readdir(f.root)).some((name) => name.endsWith(".tmp"))).toBe(false);
    } finally { await f.cleanup(); }
  });
  it("returns an honest empty snapshot and bounds automatically generated files", async () => {
    const f = await fixture();
    try {
      const empty = await f.exporter.export(scope, { sinceMinutes: 60, conversationId: "second" }, signal());
      const old = new Date(Date.now() - 8 * 86400000);
      await utimes(empty.path, old, old);
      await writeFile(join(f.options.exportsRoot, "keep-me.txt"), "unrelated");
      f.options.conversations.mockResolvedValue([]);
      for (let index = 0; index < 22; index++) {
        const result = await f.exporter.export(scope, { sinceMinutes: 60 }, signal());
        expect(result.recordCount).toBe(0);
      }
      const files = await readdir(f.options.exportsRoot);
      expect(files.filter((name) => name.endsWith(".json"))).toHaveLength(20);
      expect(files).toContain("keep-me.txt");
      await expect(stat(empty.path)).rejects.toMatchObject({ code: "ENOENT" });
    } finally { await f.cleanup(); }
  });
  it("defaults MCP to the current conversation, deduplicates retries and permits explicit workspace export", async () => {
    const f = await fixture();
    try {
      const tool = createDeveloperLogTool(f.exporter, scope, "conversation");
      const input = { request_id: "same-export" };
      const [first, retry] = await Promise.all([tool.call(input, signal()), tool.call(input, signal())]);
      expect(first).toEqual(retry);
      expect(first).toMatchObject({ recordCount: 1, filter: { conversationId: "conversation" } });
      expect(await readdir(f.options.exportsRoot)).toHaveLength(1);
      await expect(tool.call({ request_id: "same-export", scope: "workspace" }, signal())).rejects.toThrow("another log export");
      expect(await tool.call({ request_id: "all", scope: "workspace" }, signal())).toMatchObject({ recordCount: 2, filter: { conversationId: null } });
      await expect(tool.call({ request_id: "path", path: "/tmp/arbitrary.json" }, signal())).rejects.toThrow();
    } finally { await f.cleanup(); }
  });
  it("merges the daemon's timing file into one per-turn waterfall in both exports", async () => {
    const f = await fixture();
    try {
      const base = Date.now() - 1000;
      const at = (ms: number) => new Date(base + ms).toISOString();
      const daemon = (ms: number, stage: string, extra: object = {}) => JSON.stringify({ version: 2, source: "daemon", at: at(ms), event: "mark", elapsedMs: ms, conversationId: "conversation", requestId: "r", turnId: "turn-r", provider: "claude", origin: "local", clientKind: "desktop", turnKind: "user", stage, ...extra });
      await writeFile(f.daemonTimingPath, [daemon(10, "daemon.request_received"), daemon(90, "provider.first_text"), daemon(95, "ws.first_output_write"), daemon(200, "turn.finished", { outcome: "completed" }), daemon(15, "turn.persist", { conversationId: "foreign", turnId: "foreign", requestId: "foreign" })].join("\n") + "\n");
      f.log.write({ version: 2, source: "gui", at: at(0), stage: "client.send", event: "mark", elapsedMs: 0, conversationId: "conversation", requestId: "r" });
      f.log.write({ version: 2, source: "gui", at: at(120), stage: "client.first_event_received", event: "mark", elapsedMs: 0, conversationId: "conversation", turnId: "turn-r" });
      const exported = await f.exporter.export(scope, { sinceMinutes: 60, conversationId: "conversation" }, signal());
      await f.exporter.save(scope);
      for (const bundle of [JSON.parse(await readFile(exported.path, "utf8")), JSON.parse(await readFile(f.selected, "utf8"))]) {
        expect(bundle.sources.daemonTiming).toEqual({ status: "available", incomplete: false, droppedWrites: 0 });
        const turn = bundle.waterfalls.turns.find((entry: { turnId?: string }) => entry.turnId === "turn-r");
        expect(turn).toMatchObject({ provider: "claude", turnKind: "user", outcome: "completed", totals: { clientToFirstEventMs: 120, daemonToFirstOutputMs: 80, transportMs: 35 } });
        expect(turn.segments.map((segment: { stage: string }) => segment.stage)).toEqual(["client.send", "daemon.request_received", "provider.first_text", "ws.first_output_write", "client.first_event_received", "turn.finished"]);
        expect(JSON.stringify(bundle)).not.toContain("foreign");
      }
      expect(exported).toMatchObject({ daemonTimingRecordCount: 4, waterfallCount: 2, sources: { daemonTiming: { status: "available" } } });
    } finally { await f.cleanup(); }
  });
  it("reads one phone-to-desktop turn end to end on the executor, with a wrong phone clock and hashed devices", async () => {
    const f = await fixture();
    try {
      const base = Date.now() - 2000;
      const at = (ms: number) => new Date(base + ms).toISOString();
      const hours = 10 * 3_600_000;
      const peer = "12D3KooWPhonePeerIdentifier";
      const line = (value: object) => JSON.stringify({ version: 2, event: "mark", elapsedMs: 0, ...value });
      const dir = dirname(f.daemonTimingPath);
      await writeFile(f.daemonTimingPath, [
        line({ source: "daemon", at: at(100), stage: "daemon.request_received", conversationId: "conversation", requestId: "r", origin: "p2p", clientKind: "mobile", connection: "relayed" }),
        line({ source: "daemon", at: at(600), stage: "provider.first_text", conversationId: "conversation", requestId: "r", turnId: "turn-r", provider: "codex" }),
        line({ source: "daemon", at: at(650), stage: "ws.first_output_write", conversationId: "conversation", requestId: "r", turnId: "turn-r", clientKind: "desktop", connection: "loopback" }),
        line({ source: "daemon", at: at(700), stage: "ws.first_output_write", conversationId: "conversation", requestId: "r", turnId: "turn-r", clientKind: "mobile", connection: "relayed" }),
      ].join("\n") + "\n");
      await writeFile(join(dir, "device-timing.jsonl"), [
        line({ source: "daemon", at: at(90), stage: "link.request_received", requestId: "r", peer, role: "executor", connection: "relayed" }),
        line({ source: "daemon", at: at(10), stage: "connection.relayed", peer, addrType: "relay" }),
        line({ source: "daemon", at: at(20), stage: "proxy.request", requestId: "someone-elses", peer }),
      ].join("\n") + "\n");
      const received = at(1500);
      const phone = (ms: number, value: object) => line({ source: "mobile", at: new Date(Date.parse(at(ms)) - hours).toISOString(), receivedAt: received, reporter: peer, clientKind: "mobile", ...value });
      await writeFile(join(dir, "timing-reports.jsonl"), [
        phone(0, { stage: "client.send", conversationId: "conversation", requestId: "r" }),
        phone(800, { stage: "client.first_event_received", conversationId: "conversation", turnId: "turn-r" }),
        phone(5, { stage: "peer.connect", event: "end", durationMs: 40, addrType: "lan", peer }),
        phone(1, { stage: "client.send", conversationId: "foreign", requestId: "foreign" }),
      ].join("\n") + "\n");

      const exported = await f.exporter.export(scope, { sinceMinutes: 60, conversationId: "conversation" }, signal());
      const bundle = JSON.parse(await readFile(exported.path, "utf8"));
      const turn = bundle.waterfalls.turns.find((entry: { turnId?: string }) => entry.turnId === "turn-r");
      // (800 - 0) on the phone minus (700 - 100) on the computer, with the phone ten hours off.
      expect(turn.totals.transportMs).toBe(200);
      expect(turn).toMatchObject({ origin: "p2p", clientKind: "mobile", connection: "relayed", startedAt: at(90) });
      expect(turn.segments.map((segment: { stage: string }) => segment.stage)).toEqual(expect.arrayContaining(["client.send", "link.request_received", "daemon.request_received", "client.first_event_received"]));
      const text = JSON.stringify(bundle);
      expect(text).not.toContain(peer);
      expect(text).not.toContain("foreign");
      expect(text).not.toContain("someone-elses");
      expect(bundle.deviceTimeline.map((record: { stage: string }) => record.stage)).toEqual(["connection.relayed", "peer.connect"]);
      expect(bundle.deviceTimeline[1].reporter).toMatch(/^[0-9a-f]{16}$/);
      expect(exported).toMatchObject({ sources: { linkTiming: { status: "available" } } });
    } finally { await f.cleanup(); }
  });
  describe("peer identities", () => {
    const peer = "12D3KooWLnZUpcaBwbz9uD1XsyyHnbXUrJRmxnsMiRnuCmvPix67";
    const at = new Date().toISOString();
    const record = { version: 2, source: "daemon", event: "mark", elapsedMs: 0, stage: "connection.direct", at, peer, reporter: peer } as LocalTimingRecord;
    const build = (daemonText = "", webMessage = "") => buildDeveloperLogBundle({
      runtime, workspaceId: null, since: 0, until: Date.now(), conversationId: null,
      timing: { records: [record], incomplete: false, droppedWrites: 0 } as unknown as LocalTimingSnapshot,
      sources: {
        web: { status: "available", truncated: false, incomplete: false, maxBytes: 1, records: [{ at, level: "info", message: webMessage }] },
        daemon: { status: "available", scope: "desktop-profile", timeFilterApplied: false, truncated: false, incomplete: false, maxBytes: 1, text: daemonText },
      },
    });

    it("are recognizable within one export and not across exports", () => {
      const first = build().records[0]!;
      const second = build().records[0]!;
      expect(first.peer).toMatch(/^[0-9a-f]{16}$/);
      expect(first.reporter).toBe(first.peer);
      expect(second.peer).not.toBe(first.peer);
      expect(first.peer).not.toBe(createHash("sha256").update(peer).digest("hex").slice(0, 16));
    });
    it("in log text read as the same hash as the records", () => {
      const bundle = build(`dial ${peer} failed\n`, `lost ${peer}`);
      const hash = bundle.records[0]!.peer!;
      expect(JSON.stringify(bundle)).not.toContain(peer);
      expect(bundle.sources.daemon.text).toBe(`dial peer:${hash} failed\n`);
      expect(bundle.sources.web.records[0]!.message).toBe(`lost peer:${hash}`);
    });
  });

  it("keeps a report out of a time window by when it arrived, not by its own clock", async () => {
    const f = await fixture();
    try {
      const dir = dirname(f.daemonTimingPath);
      const report = (at: string, receivedAt: string) => JSON.stringify({ version: 2, source: "mobile", event: "mark", elapsedMs: 0, stage: "client.send", conversationId: "conversation", requestId: receivedAt.slice(0, 4), at, receivedAt });
      const now = new Date().toISOString();
      await writeFile(join(dir, "timing-reports.jsonl"), [report("2001-01-01T00:00:00.000Z", now), report(now, "2001-01-01T00:00:00.000Z")].join("\n") + "\n");
      const exported = await f.exporter.export(scope, { sinceMinutes: 60, conversationId: "conversation" }, signal());
      const bundle = JSON.parse(await readFile(exported.path, "utf8"));
      const reports = bundle.records.filter((record: { source: string }) => record.source === "mobile");
      expect(reports.map((record: { at: string }) => record.at)).toEqual(["2001-01-01T00:00:00.000Z"]);
    } finally { await f.cleanup(); }
  });
  it("reports the export incomplete when the daemon or desktop recorded dropped writes", async () => {
    const f = await fixture();
    try {
      await writeFile(f.daemonTimingPath, JSON.stringify({ version: 2, source: "daemon", at: new Date().toISOString(), stage: "timing.dropped", event: "mark", elapsedMs: 0, count: 7 }) + "\n");
      const exported = await f.exporter.export(scope, { sinceMinutes: 60 }, signal());
      expect(exported.incomplete).toBe(true);
      expect(JSON.parse(await readFile(exported.path, "utf8")).coverage).toMatchObject({ incomplete: true, droppedTimingWrites: 7 });
    } finally { await f.cleanup(); }
  });
});
