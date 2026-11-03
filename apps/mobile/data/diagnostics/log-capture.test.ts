// @vitest-environment node
import { describe, expect, it, vi } from "vitest";
import { LogCapture, formatLogArguments, type LogSink } from "./log-capture";

function setup() {
  const written: { source: string; lines: string }[] = [];
  const sink: LogSink = { appendLogLines: (source, lines) => { written.push({ source, lines }); } };
  const scheduled: (() => void)[] = [];
  const capture = new LogCapture(sink, ((flush: () => void) => { scheduled.push(flush); return 0; }) as never);
  return { written, sink, scheduled, capture };
}

describe("LogCapture", () => {
  it("batches records as JSON lines on a delay", () => {
    const { capture, written, scheduled } = setup();
    capture.log("info", ["[api] → GET /api/search?q=term", { rid: "r1" }]);
    capture.log("warning", ["[ws] socket closed"]);
    expect(written).toHaveLength(0);
    expect(scheduled).toHaveLength(1);
    scheduled[0]!();
    expect(written).toHaveLength(1);
    const lines = written[0]!.lines.split("\n").map((line) => JSON.parse(line));
    expect(lines[0]).toMatchObject({ source: "app", level: "info", message: '[api] → GET /api/search?q=term {"rid":"r1"}' });
    expect(lines[1].level).toBe("warning");
    expect(Number.isNaN(Date.parse(lines[0].at))).toBe(false);
  });

  it("writes errors at once, since one may be the process's last act", () => {
    const { capture, written } = setup();
    capture.log("error", [new Error("boom")]);
    expect(written[0]!.lines).toContain("boom");
  });

  it("labels records with the current workspace and omits it when there is none", () => {
    const { capture, written } = setup();
    capture.log("error", ["before"]);
    capture.setWorkspace("ws-1");
    capture.log("error", ["inside"]);
    capture.setWorkspace(null);
    capture.log("error", ["after"]);
    const records = written.map((entry) => JSON.parse(entry.lines));
    expect(records.map((record) => record.workspaceId)).toEqual([undefined, "ws-1", undefined]);
  });

  it("skips debug noise and truncates an oversized message", () => {
    const { capture, written } = setup();
    capture.log("debug", ["[ws] event"]);
    capture.log("error", ["x".repeat(20_000)]);
    expect(written).toHaveLength(1);
    const { message } = JSON.parse(written[0]!.lines);
    expect(message.length).toBeLessThan(17_000);
    expect(message).toContain("[truncated]");
  });

  it("nests a mark, whose own source says who wrote it", () => {
    const { capture, written } = setup();
    capture.mark({ at: "2026-11-03T00:00:00.000Z" });
    capture.flush();
    expect(written[0]!.source).toBe("marks");
    expect(JSON.parse(written[0]!.lines)).toEqual({ at: "2026-11-03T00:00:00.000Z", source: "marks", record: { at: "2026-11-03T00:00:00.000Z" } });
  });

  it("keeps a failed write for the next flush and never throws into the caller", () => {
    const { capture, sink, written } = setup();
    const append = vi.spyOn(sink, "appendLogLines").mockImplementationOnce(() => { throw new Error("native failed"); });
    expect(() => capture.log("error", ["kept"])).not.toThrow();
    append.mockRestore();
    capture.flush();
    expect(written.at(-1)!.lines).toContain("kept");
  });

  it("forgets what is pending when discarded, for a sign-out about to clear the log", () => {
    const { capture, written } = setup();
    capture.log("info", ["before sign-out"]);
    capture.discard();
    capture.flush();
    expect(written).toHaveLength(0);
  });

  it("does nothing without a native log", () => {
    const capture = new LogCapture(null);
    expect(() => { capture.log("error", ["x"]); capture.mark({ at: "now" }); capture.flush(); }).not.toThrow();
  });
});

describe("formatLogArguments", () => {
  it("formats errors, including nested ones, and survives a value it cannot serialize", () => {
    const cycle: Record<string, unknown> = {};
    cycle.self = cycle;
    expect(formatLogArguments(["a", 1, new TypeError("bad")])).toContain("TypeError: bad");
    expect(formatLogArguments([{ error: new Error("nested") }])).toContain("nested");
    expect(formatLogArguments([cycle])).toBe("[object Object]");
    expect(formatLogArguments([undefined])).toBe("undefined");
  });
});
