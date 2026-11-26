import { afterEach, describe, expect, it } from "vitest";
import {
  attributeLongTask,
  changedPropNames,
  recordFrameEntry,
  recordMeasureEntry,
  resetLongTaskAttribution,
} from "./long-task-attribution";

const components = { devtools: { track: "Components \u269b", color: "primary" } };

function render(name: string, startTime: number, duration: number, properties: unknown[] | null = null) {
  recordMeasureEntry({
    name: `\u200b${name}`,
    startTime,
    duration,
    detail: { devtools: { ...components.devtools, properties } },
  });
}

afterEach(() => resetLongTaskAttribution());

describe("attributeLongTask", () => {
  it("lists the frame's scripts that ran during the task, longest first", () => {
    recordFrameEntry({
      startTime: 1000,
      duration: 260,
      blockingDuration: 210,
      renderStart: 1220,
      styleAndLayoutStart: 1240,
      scripts: [
        { startTime: 1000, duration: 20, invoker: "TimerHandler:setTimeout", invokerType: "user-callback", sourceURL: "", sourceCharPosition: -1 },
        {
          startTime: 1020,
          duration: 190,
          invoker: "MessagePort.onmessage",
          invokerType: "event-listener",
          sourceFunctionName: "performWorkUntilDeadline",
          sourceURL: "http://localhost:3000/_next/static/chunks/react-dom.js",
          sourceCharPosition: 4521,
          forcedStyleAndLayoutDuration: 12,
        },
      ],
    });
    recordFrameEntry({ startTime: 5000, duration: 80, scripts: [{ startTime: 5000, duration: 80, invoker: "later" }] });

    const attribution = attributeLongTask(1000, 220);
    expect(attribution.frame).toEqual({ durationMs: 260, blockingMs: 210, renderMs: 40, styleLayoutMs: 20 });
    expect(attribution.scripts).toEqual([
      {
        invoker: "MessagePort.onmessage",
        invokerType: "event-listener",
        functionName: "performWorkUntilDeadline",
        source: "http://localhost:3000/_next/static/chunks/react-dom.js:4521",
        durationMs: 190,
        forcedLayoutMs: 12,
      },
      { invoker: "TimerHandler:setTimeout", invokerType: "user-callback", functionName: null, source: null, durationMs: 20, forcedLayoutMs: 0 },
    ]);
  });

  it("charges each component its own render time and keeps the props that changed", () => {
    render("IssueDetail", 100, 150, [
      ["Changed Props", ""],
      ["\u2013\u00a0issue", "{…}"],
      ["+\u00a0issue", "{…}"],
      ["+\u00a0\u00a0\u00a0title", '"new"'],
    ]);
    render("CommentList", 110, 120);
    render("CommentCard", 115, 40);
    render("CommentCard", 160, 50);
    render("Sidebar", 400, 30);

    const { components: rendered } = attributeLongTask(90, 200);
    expect(rendered.map((c) => [c.name, c.renders, c.selfMs])).toEqual([
      ["CommentCard", 2, 90],
      ["IssueDetail", 1, 30],
      ["CommentList", 1, 30],
    ]);
    expect(rendered.find((c) => c.name === "IssueDetail")?.changedProps).toEqual(["issue"]);
  });

  it("lists a render that crosses the task's edges as a lead without charging it time", () => {
    render("NavigationProvider", 0, 300);
    render("NavigationProvider", 260, 40);
    render("IssueRow", 210, 30);

    const attribution = attributeLongTask(200, 70);
    expect(attribution.components.map((c) => [c.name, c.selfMs])).toEqual([["IssueRow", 30]]);
    expect(attribution.spanningComponents).toEqual([{ name: "NavigationProvider", renders: 2 }]);
  });

  it("flags props React found deeply equal", () => {
    render("Row", 0, 120, [["Changed Props", "This component received deeply equal props. It might benefit from useMemo."]]);
    expect(attributeLongTask(0, 120).components[0]?.deeplyEqualProps).toBe(true);
  });

  it("names the component and method behind an update whose render began in the task", () => {
    const scheduler = (name: string, startTime: number, duration: number, properties: unknown[]) =>
      recordMeasureEntry({
        name,
        startTime,
        duration,
        detail: { devtools: { track: "Blocking", trackGroup: "Scheduler \u269b", properties } },
      });
    scheduler("Cascading Update", 80, 30, [["Component name", "CommentList"], ["Method name", "setState"]]);
    scheduler("Update", 400, 10, [["Component name", "Sidebar"]]);

    expect(attributeLongTask(100, 100).updates).toEqual([
      { kind: "Cascading Update", lane: "Blocking", component: "CommentList", method: "setState" },
    ]);
  });

  it("ignores measures that are not React's", () => {
    recordMeasureEntry({ name: "app:boot", startTime: 0, duration: 100, detail: null });
    recordMeasureEntry({ name: "Reconnect", startTime: 0, duration: 100, detail: components });
    expect(attributeLongTask(0, 100)).toEqual({ frame: null, scripts: [], updates: [], components: [], spanningComponents: [] });
  });
});

describe("changedPropNames", () => {
  it("keeps top-level keys that were added or removed, not nested or unchanged ones", () => {
    expect(
      changedPropNames([
        ["Changed Props", ""],
        ["\u2013\u00a0onClick", "() => {}"],
        ["+\u00a0onClick", "() => {}"],
        ["+\u00a0\u00a0\u00a0nested", "1"],
        ["\u2007\u00a0same", "1"],
        ["+\u00a0items", "Array(3)"],
      ]),
    ).toEqual(["onClick", "items"]);
  });
});
