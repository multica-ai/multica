// @vitest-environment node
import { mkdtemp, readFile, rm, stat, writeFile } from "node:fs/promises";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { describe, expect, it } from "vitest";
import { createLocalTiming, LOCAL_TIMING_PREFIX, type LocalTimingRecord } from "@multica/tutti-agent-gui-host/local-timing";
import { LocalTimingLog, snapshotDaemonTiming } from "./local-timing-log";

describe("local timing log", () => {
  it("persists and rotates validated records without copying unrelated console data", async () => {
    const root = await mkdtemp(join(tmpdir(), "multica-timing-"));
    try {
      const log = new LocalTimingLog(root, 700);
      const records: LocalTimingRecord[] = [];
      const timing = createLocalTiming({ source: "renderer", conversationId: "c", requestId: "request" }, (record) => records.push(record));
      timing.mark("gui.submit");
      log.console("unrelated SECRET"); log.console(LOCAL_TIMING_PREFIX + "not-json SECRET");
      for (let i = 0; i < 7; i++) log.console(LOCAL_TIMING_PREFIX + JSON.stringify({ ...records[0], elapsedMs: i, content: "SECRET" }));
      await log.flush();
      const current = await readFile(log.path, "utf8");
      const previous = await readFile(log.path + ".1", "utf8");
      expect(current + previous).not.toContain("SECRET");
      expect(current.trim().split("\n").map((line) => JSON.parse(line)).at(-1).elapsedMs).toBe(6);
      expect(Buffer.byteLength(current)).toBeLessThanOrEqual(700);
      expect(Buffer.byteLength(previous)).toBeLessThanOrEqual(700);
      if (process.platform !== "win32") expect((await stat(log.path)).mode & 0o777).toBe(0o600);
    } finally { await rm(root, { recursive: true, force: true }); }
  });
  it("remains best effort when its destination cannot be written", async () => {
    const root = await mkdtemp(join(tmpdir(), "multica-timing-failure-"));
    try {
      const file = join(root, "file"); await writeFile(file, "occupied");
      const log = new LocalTimingLog(file);
      createLocalTiming({ source: "main", conversationId: "c" }, log.write).mark("turn.execute");
      await expect(log.flush()).resolves.toBeUndefined();
    } finally { await rm(root, { recursive: true, force: true }); }
  });
  it("reads both retained files, validates again and reports malformed lines", async () => {
    const root = await mkdtemp(join(tmpdir(), "multica-timing-snapshot-"));
    try {
      const log = new LocalTimingLog(root);
      const row = { version: 1, source: "gui", conversationId: "c", at: new Date().toISOString(), stage: "gui.first_output_applied", event: "mark", elapsedMs: 1 };
      await writeFile(log.path + ".1", JSON.stringify({ ...row, content: "SECRET" }) + "\ninvalid\n");
      log.write({ ...row, source: "main", conversationId: "foreign" });
      log.write({ ...row, source: "main" });
      const snapshot = await log.snapshot(new Set(["c"]), Date.now() - 60000, Date.now() + 1000);
      expect(snapshot.records).toHaveLength(2);
      expect(snapshot.incomplete).toBe(true);
      expect(JSON.stringify(snapshot.records)).not.toMatch(/SECRET|foreign/);
    } finally { await rm(root, { recursive: true, force: true }); }
  });
  it("reports dropped writes as incomplete, in this process and to a separate reader", async () => {
    const root = await mkdtemp(join(tmpdir(), "multica-timing-dropped-"));
    try {
      const log = new LocalTimingLog(root);
      const row = { version: 2, source: "main", conversationId: "c", at: new Date().toISOString(), stage: "turn.persist", event: "mark", elapsedMs: 1 };
      for (let i = 0; i < 1003; i++) log.write(row);
      const range = [new Set(["c"]), Date.now() - 60000, Date.now() + 60000] as const;
      const live = await log.snapshot(...range);
      expect(live).toMatchObject({ incomplete: true, droppedWrites: 3 });
      expect(live.records).toHaveLength(1000);
      expect(live.records.every((record) => record.stage === "turn.persist")).toBe(true);
      const elsewhere = await new LocalTimingLog(root).snapshot(...range);
      expect(elsewhere).toMatchObject({ incomplete: true, droppedWrites: 3 });
    } finally { await rm(root, { recursive: true, force: true }); }
  });
  it("counts a failed write as dropped", async () => {
    const root = await mkdtemp(join(tmpdir(), "multica-timing-failed-"));
    try {
      const file = join(root, "file"); await writeFile(file, "occupied");
      const log = new LocalTimingLog(file);
      log.write({ version: 2, source: "main", conversationId: "c", at: new Date().toISOString(), stage: "turn.persist", event: "mark", elapsedMs: 1 });
      await log.flush();
      expect(await log.snapshot(null, 0, Date.now() + 1000)).toMatchObject({ incomplete: true, droppedWrites: 1 });
    } finally { await rm(root, { recursive: true, force: true }); }
  });
  it("scopes the daemon's file, keeps device-level records to device-wide reads and reports its drops", async () => {
    const root = await mkdtemp(join(tmpdir(), "multica-daemon-timing-"));
    try {
      const path = join(root, "daemon-timing.jsonl");
      const at = new Date().toISOString();
      const line = (extra: object) => JSON.stringify({ version: 2, source: "daemon", at, event: "mark", elapsedMs: 1, stage: "turn.persist", ...extra });
      const range = [Date.now() - 60000, Date.now() + 60000] as const;
      expect(await snapshotDaemonTiming(null, null, ...range)).toMatchObject({ status: "unavailable", incomplete: false });
      expect(await snapshotDaemonTiming(path, null, ...range)).toMatchObject({ status: "missing", incomplete: false });
      await writeFile(path, [line({ conversationId: "c" }), line({ conversationId: "foreign" }), line({ stage: "link.start" }), line({ stage: "timing.dropped", count: 4 })].join("\n") + "\n");
      const scoped = await snapshotDaemonTiming(path, new Set(["c"]), ...range);
      expect(scoped).toMatchObject({ status: "available", incomplete: true, droppedWrites: 4 });
      expect(scoped.records.map((record) => record.conversationId)).toEqual(["c"]);
      expect((await snapshotDaemonTiming(path, null, ...range)).records.map((record) => record.stage)).toEqual(["turn.persist", "turn.persist", "link.start"]);
    } finally { await rm(root, { recursive: true, force: true }); }
  });
});
