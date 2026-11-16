// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { DevLongTask, DevLongTaskAttribution, DevRecording } from "@multica/core/diagnostics";
import { longTaskCause, longTaskReport, scriptSource, taskActivity, updateLabel } from "./long-task-report";

const AT = new Date(2026, 10, 16, 19, 39, 16, 120).getTime();

function attribution(overrides: Partial<DevLongTaskAttribution> = {}): DevLongTaskAttribution {
  return { frame: null, scripts: [], updates: [], components: [], ...overrides };
}

function task(overrides: Partial<DevLongTask> = {}): DevLongTask {
  return { at: AT, durationMs: 2100, route: "/acme/issues/MUL-1", attribution: attribution(), ...overrides };
}

function recording(overrides: Partial<DevRecording> = {}): DevRecording {
  return {
    requests: [],
    navigations: [],
    longTasks: [],
    errors: [],
    sync: [],
    events: [],
    frameCounts: [],
    memory: [],
    heapLimit: null,
    ...overrides,
  };
}

const script = {
  invoker: "Response.json.then",
  invokerType: "resolve-promise",
  functionName: "handleComments",
  source: "http://localhost:3000/_next/static/chunks/app.js?v=1:812",
  durationMs: 1900,
  forcedLayoutMs: 0,
};

describe("longTaskCause", () => {
  it("names the component that took a real share of the task, else the longest script", () => {
    const card = { name: "CommentCard", renders: 120, selfMs: 900, changedProps: ["comment"], deeplyEqualProps: false };
    const list = { name: "CommentList", renders: 1, selfMs: 30, changedProps: [], deeplyEqualProps: false };
    expect(longTaskCause(task({ attribution: attribution({ components: [card, list], scripts: [script] }) }))).toBe(
      "<CommentCard> 900ms",
    );
    expect(longTaskCause(task({ attribution: attribution({ components: [list], scripts: [script] }) }))).toBe(
      "Response.json.then → handleComments",
    );
    expect(longTaskCause(task({ attribution: attribution({ components: [list] }) }))).toBe("<CommentList> 30ms");
    expect(longTaskCause(task({ attribution: null }))).toBeNull();
  });
});

describe("scriptSource", () => {
  it("drops origin and query so the bundle path and position remain", () => {
    expect(scriptSource(script.source)).toBe("/_next/static/chunks/app.js:812");
    expect(scriptSource("file:///Applications/Multica.app/Contents/Resources/out/renderer/assets/index-a1.js:99")).toBe(
      "assets/index-a1.js:99",
    );
  });
});

describe("taskActivity", () => {
  it("lists what arrived just before or during the task, not what came long before", () => {
    const rec = recording({
      requests: [
        { requestId: "r1", method: "GET", path: "/api/issues/1/comments", body: null, bodyNote: null, startedAt: AT - 600, durationMs: 400, status: 200, error: null },
        { requestId: "r2", method: "GET", path: "/api/old", body: null, bodyNote: null, startedAt: AT - 9000, durationMs: 100, status: 200, error: null },
      ],
      navigations: [{ at: AT - 1500, route: "/acme/issues/MUL-1", durationMs: null }],
    });
    expect(taskActivity(task(), rec).map((a) => a.text)).toEqual([
      "route → /acme/issues/MUL-1",
      "response GET /api/issues/1/comments → 200 in 400ms",
    ]);
  });
});

describe("longTaskReport", () => {
  const context = {
    build: "web",
    apiUrl: "http://localhost:18080",
    route: "/acme/issues/MUL-1",
    now: AT + 5000,
    blocked: 0.21,
    framesSupported: true,
    userAgent: "Chrome/142",
  };

  it("labels an update that names no component by its method alone", () => {
    expect(updateLabel({ kind: "Cascading Update", lane: "Blocking", component: null, method: "updateSyncExternalStore()" })).toBe(
      "Cascading Update (Blocking) via updateSyncExternalStore()",
    );
  });

  it("sums where the time went and details the longest tasks first", () => {
    const slow = task({
      attribution: attribution({
        scripts: [script],
        updates: [{ kind: "Cascading Update", lane: "Blocking", component: "CommentList", method: "setState" }],
        components: [{ name: "CommentCard", renders: 120, selfMs: 900, changedProps: ["comment", "onReply"], deeplyEqualProps: true }],
      }),
    });
    const quick = task({ at: AT - 3000, durationMs: 60, attribution: attribution({ scripts: [{ ...script, durationMs: 55 }] }) });
    const report = longTaskReport(recording({ longTasks: [quick, slow] }), context);

    expect(report).toContain("2 long tasks, 2.2s blocked in total; blocked in the last 10s: 21%");
    expect(report).toContain("- <CommentCard> 900ms over 1 task");
    expect(report).toContain("- Response.json.then → handleComments at /_next/static/chunks/app.js:812 2.0s, 2×");
    expect(report).toContain("- Cascading Update (Blocking) from CommentList via setState, 1×");
    expect(report).toContain("19:39:16.120    2.1s  <CommentCard> 900ms");
    expect(report).toContain(
      "- <CommentCard> 900ms self, 120×; changed props: comment, onReply; props were deeply equal (new object, same contents)",
    );
    expect(report.indexOf("### 19:39:16.120 · 2.1s")).toBeLessThan(report.indexOf("### 19:39:13.120 · 60ms"));
  });

  it("says so when nothing was recorded", () => {
    expect(longTaskReport(recording(), context)).toContain("No long tasks were recorded.");
  });
});
