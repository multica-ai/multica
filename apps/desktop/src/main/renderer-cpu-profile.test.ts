import { describe, expect, it, vi } from "vitest";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import {
  captureRendererCpuProfile,
  sanitizeCpuProfile,
  sendProfilerCommand,
  type CdpDebugger,
} from "./renderer-cpu-profile";

// A realistic raw V8 profile with content-bearing extras (deoptReason,
// positionTicks, scriptId, a stray top-level key) the sanitizer must drop.
const RAW_PROFILE = {
  nodes: [
    {
      id: 1,
      callFrame: {
        functionName: "parseMarkdown",
        scriptId: "42",
        url: "https://cdn.example/_next/static/chunks/4231.js?token=secret",
        lineNumber: 10,
        columnNumber: 5,
      },
      hitCount: 90,
      children: [2],
      deoptReason: "should be dropped",
      positionTicks: [{ line: 1, ticks: 3 }],
    },
    {
      id: 2,
      callFrame: { functionName: "", scriptId: "7", url: "", lineNumber: -1, columnNumber: -1 },
      hitCount: 5,
    },
  ],
  startTime: 1000,
  endTime: 2200,
  samples: [1, 1, 2],
  timeDeltas: [100, 100, 100],
  extra: "should be dropped",
};

function makeFakeDebugger(opts: { failOn?: string; alreadyAttached?: boolean } = {}) {
  let attached = opts.alreadyAttached ?? false;
  const sent: string[] = [];
  const attach = vi.fn((_protocol?: string) => {
    attached = true;
  });
  const detach = vi.fn(() => {
    attached = false;
  });
  const sendCommand = vi.fn(async (method: string) => {
    sent.push(method);
    if (opts.failOn && method === opts.failOn) throw new Error("cdp failure");
    if (method === "Profiler.stop") return { profile: RAW_PROFILE };
    return {};
  });
  const dbg: CdpDebugger = {
    isAttached: () => attached,
    attach,
    detach,
    sendCommand,
  };
  return { dbg, sent, attach, detach, sendCommand };
}

const noSleep = async () => {};

describe("sendProfilerCommand allowlist", () => {
  it("forwards Profiler.* commands", async () => {
    const { dbg, sent } = makeFakeDebugger();
    await sendProfilerCommand(dbg, "Profiler.start");
    expect(sent).toEqual(["Profiler.start"]);
  });

  it.each([
    "HeapProfiler.takeHeapSnapshot",
    "HeapProfiler.startSampling",
    "Runtime.evaluate",
    "Runtime.getProperties",
    "Runtime.callFunctionOn",
    "Debugger.enable",
    "Page.captureScreenshot",
  ])("rejects forbidden CDP method %s without sending it", async (method) => {
    const { dbg, sent } = makeFakeDebugger();
    await expect(sendProfilerCommand(dbg, method)).rejects.toThrow(/Forbidden CDP method/);
    expect(sent).toEqual([]);
  });
});

describe("captureRendererCpuProfile", () => {
  it("attaches, drives ONLY Profiler.* commands, and returns a sanitized profile", async () => {
    const { dbg, sent, attach, detach } = makeFakeDebugger();

    const profile = await captureRendererCpuProfile(dbg, { delayMs: noSleep });

    // Every CDP command sent during a full capture is in the Profiler.* domain.
    expect(sent.length).toBeGreaterThan(0);
    for (const method of sent) expect(method.startsWith("Profiler.")).toBe(true);
    expect(attach).toHaveBeenCalledTimes(1);
    expect(detach).toHaveBeenCalledTimes(1);

    // Content-bearing fields are gone; only code locations + counts remain.
    expect(profile).toEqual({
      nodes: [
        {
          id: 1,
          callFrame: {
            functionName: "parseMarkdown",
            url: "https://cdn.example/_next/static/chunks/4231.js?token=secret",
            lineNumber: 10,
            columnNumber: 5,
          },
          hitCount: 90,
          children: [2],
        },
        {
          id: 2,
          callFrame: { functionName: "", url: "", lineNumber: -1, columnNumber: -1 },
          hitCount: 5,
        },
      ],
      startTime: 1000,
      endTime: 2200,
      samples: [1, 1, 2],
      timeDeltas: [100, 100, 100],
    });
  });

  it("does not detach a debugger it found already attached", async () => {
    const { dbg, attach, detach } = makeFakeDebugger({ alreadyAttached: true });
    await captureRendererCpuProfile(dbg, { delayMs: noSleep });
    expect(attach).not.toHaveBeenCalled();
    expect(detach).not.toHaveBeenCalled();
  });

  it("is best-effort: a CDP failure yields null and still detaches", async () => {
    const { dbg, detach } = makeFakeDebugger({ failOn: "Profiler.start" });
    const profile = await captureRendererCpuProfile(dbg, { delayMs: noSleep });
    expect(profile).toBeNull();
    expect(detach).toHaveBeenCalledTimes(1);
  });

  it("discards an over-size profile whole rather than truncating", async () => {
    const { dbg } = makeFakeDebugger();
    const profile = await captureRendererCpuProfile(dbg, { delayMs: noSleep, maxBytes: 10 });
    expect(profile).toBeNull();
  });

  it("resolves to null via the hard timeout if capture stalls (never wedges recovery)", async () => {
    const { dbg } = makeFakeDebugger();
    const neverResolve = () => new Promise<void>(() => {});
    const profile = await captureRendererCpuProfile(dbg, {
      delayMs: neverResolve,
      hardTimeoutMs: 20,
    });
    expect(profile).toBeNull();
  });
});

describe("sanitizeCpuProfile", () => {
  it("returns null for non-object / missing nodes", () => {
    expect(sanitizeCpuProfile(null, 1_000)).toBeNull();
    expect(sanitizeCpuProfile({}, 1_000)).toBeNull();
    expect(sanitizeCpuProfile({ nodes: "nope" }, 1_000)).toBeNull();
  });

  it("coerces malformed numeric/string fields to safe defaults", () => {
    const out = sanitizeCpuProfile(
      { nodes: [{ id: "x", callFrame: null, hitCount: undefined }], startTime: NaN, endTime: 5 },
      10_000,
    );
    expect(out).toEqual({
      nodes: [
        { id: 0, callFrame: { functionName: "", url: "", lineNumber: 0, columnNumber: 0 }, hitCount: 0 },
      ],
      startTime: 0,
      endTime: 5,
    });
  });
});

// CI guard (Howard's required fix ①): the profiling module must drive only the
// CDP Profiler.* domain. Rather than grep for forbidden method strings (the
// file documents them in comments), pin the structural invariant: there is
// exactly ONE `.sendCommand(` call in the module, and it lives inside
// `sendProfilerCommand`, which enforces the Profiler.* prefix. No other code
// path can reach CDP, so a future "just read a bit of context" edit can't slip
// a HeapProfiler.* / Runtime.* / Debugger.* command past review.
describe("CDP allowlist is structurally enforced", () => {
  // Vitest runs with cwd = the desktop package root.
  const source = readFileSync(
    resolve(process.cwd(), "src/main/renderer-cpu-profile.ts"),
    "utf8",
  );

  it("calls debugger.sendCommand from exactly one site (the allowlist wrapper)", () => {
    const matches = source.match(/\.sendCommand\(/g) ?? [];
    expect(matches.length).toBe(1);
  });

  it("that single site is guarded by the Profiler.* prefix check", () => {
    const wrapper = source.slice(
      source.indexOf("export function sendProfilerCommand"),
      source.indexOf(".sendCommand("),
    );
    expect(wrapper).toContain("ALLOWED_CDP_METHOD_PREFIX");
  });
});
