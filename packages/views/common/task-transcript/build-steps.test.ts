// @vitest-environment node
import { describe, expect, it } from "vitest";
import {
  buildLanes,
  buildSteps,
  groupSteps,
  laneSegmentPosition,
  rowCalls,
  shouldShowTimeline,
  timelineTicks,
  toolKindTotals,
  type TraceCallStep,
} from "./build-steps";
import type { TimelineItem } from "./build-timeline";

const T0 = "2026-08-15T10:00:00.000Z";
function at(seconds: number): string {
  return new Date(Date.parse(T0) + seconds * 1000).toISOString();
}

let seq = 0;
function call(tool: string, seconds: number, input?: Record<string, unknown>): TimelineItem {
  return { seq: ++seq, type: "tool_use", tool, input, created_at: at(seconds) };
}
function result(tool: string, seconds: number, output = "ok"): TimelineItem {
  return { seq: ++seq, type: "tool_result", tool, output, created_at: at(seconds) };
}
function text(seconds: number, content = "done"): TimelineItem {
  return { seq: ++seq, type: "text", content, created_at: at(seconds) };
}

describe("buildSteps", () => {
  it("folds a call and its result into one step", () => {
    const steps = buildSteps([call("Bash", 0), result("Bash", 4)]);

    expect(steps).toHaveLength(1);
    const step = steps[0] as TraceCallStep;
    expect(step.kind).toBe("call");
    expect(step.tool).toBe("Bash");
    expect(step.call).toBeDefined();
    expect(step.result).toBeDefined();
    expect(step.durationMs).toBe(4000);
  });

  it("pairs same-tool calls FIFO when two are in flight", () => {
    const first = call("Bash", 0, { command: "one" });
    const second = call("Bash", 1, { command: "two" });
    const steps = buildSteps([
      first,
      second,
      result("Bash", 5, "first done"),
      result("Bash", 9, "second done"),
    ]) as TraceCallStep[];

    expect(steps).toHaveLength(2);
    expect(steps[0]!.call?.input).toEqual({ command: "one" });
    expect(steps[0]!.result?.output).toBe("first done");
    expect(steps[1]!.result?.output).toBe("second done");
  });

  it("does not pair across different tools", () => {
    const steps = buildSteps([call("Read", 0), result("Bash", 1)]) as TraceCallStep[];

    expect(steps).toHaveLength(2);
    expect(steps[0]!.result).toBeUndefined();
    expect(steps[1]!.call).toBeUndefined();
    expect(steps[1]!.result).toBeDefined();
  });

  it("keeps a call with no result yet — a running call is not a dropped one", () => {
    const steps = buildSteps([call("Bash", 0)]) as TraceCallStep[];

    expect(steps).toHaveLength(1);
    expect(steps[0]!.result).toBeUndefined();
    expect(steps[0]!.durationMs).toBeUndefined();
  });

  it("keeps prose, thinking and errors as their own steps", () => {
    const steps = buildSteps([
      { seq: 1, type: "thinking", content: "hmm", created_at: at(0) },
      { seq: 2, type: "text", content: "here is the plan", created_at: at(1) },
      { seq: 3, type: "error", content: "boom", created_at: at(2) },
    ]);

    expect(steps.map((s) => s.kind)).toEqual(["thinking", "text", "error"]);
  });

  it("leaves duration undefined rather than guessing when a timestamp is missing", () => {
    const steps = buildSteps([
      { seq: 1, type: "tool_use", tool: "Bash" },
      { seq: 2, type: "tool_result", tool: "Bash", output: "ok" },
    ]) as TraceCallStep[];

    expect(steps[0]!.durationMs).toBeUndefined();
  });
});

describe("groupSteps", () => {
  it("folds three or more consecutive same-tool calls", () => {
    const steps = buildSteps([
      call("Read", 0),
      result("Read", 1),
      call("Read", 2),
      result("Read", 3),
      call("Read", 4),
      result("Read", 5),
    ]);

    const rows = groupSteps(steps);

    expect(rows).toHaveLength(1);
    expect(rows[0]!.kind).toBe("group");
    expect(rowCalls(rows[0]!)).toHaveLength(3);
    expect(rows[0]!.kind === "group" && rows[0]!.durationMs).toBe(5000);
  });

  it("leaves two consecutive calls alone — two commands are two things to read", () => {
    const steps = buildSteps([
      call("Bash", 0),
      result("Bash", 1),
      call("Bash", 2),
      result("Bash", 3),
    ]);

    expect(groupSteps(steps).map((r) => r.kind)).toEqual(["call", "call"]);
  });

  it("never folds shell commands — five commands are five things that happened", () => {
    const items: TimelineItem[] = [];
    for (let i = 0; i < 5; i++) {
      items.push(
        { seq: ++seq, type: "tool_use", tool: "Bash", input: { command: `step ${i}` }, created_at: at(i * 10) },
        { seq: ++seq, type: "tool_result", tool: "Bash", output: "ok", created_at: at(i * 10 + 1) },
      );
    }

    const rows = groupSteps(buildSteps(items));

    expect(rows).toHaveLength(5);
    expect(rows.every((row) => row.kind === "call")).toBe(true);
  });

  it("does not group across a different tool or across prose", () => {
    const steps = buildSteps([
      call("Read", 0),
      result("Read", 1),
      call("Read", 2),
      result("Read", 3),
      text(4),
      call("Read", 5),
      result("Read", 6),
    ]);

    expect(groupSteps(steps).map((r) => r.kind)).toEqual(["call", "call", "text", "call"]);
  });
});

describe("buildLanes", () => {
  it("splits tool spans from the model gaps that surround them", () => {
    // 0-10 model, 10-40 tool, 40-60 model.
    const steps = buildSteps([call("Bash", 10), result("Bash", 40)]);

    const lanes = buildLanes(steps, at(0), at(60))!;

    expect(lanes.tool).toEqual([{ startMs: 10_000, durationMs: 30_000, kind: "tool" }]);
    expect(lanes.model).toEqual([
      { startMs: 0, durationMs: 10_000, kind: "think" },
      { startMs: 40_000, durationMs: 20_000, kind: "think" },
    ]);
    expect(lanes.toolMs).toBe(30_000);
    expect(lanes.modelMs).toBe(30_000);
    expect(lanes.totalMs).toBe(60_000);
  });

  it("marks the gap a report landed in", () => {
    const steps = buildSteps([call("Bash", 0), result("Bash", 10), text(20)]);

    const lanes = buildLanes(steps, at(0), at(30))!;

    expect(lanes.model).toEqual([{ startMs: 10_000, durationMs: 20_000, kind: "report" }]);
  });

  it("marks the gap an error landed in, outranking a report", () => {
    const steps = buildSteps([
      call("Bash", 0),
      result("Bash", 5),
      text(10),
      { seq: 99, type: "error", content: "boom", created_at: at(12) },
    ]);

    const lanes = buildLanes(steps, at(0), at(20))!;

    expect(lanes.model.at(-1)!.kind).toBe("error");
  });

  it("merges overlapping tool spans so parallel calls are not double-counted", () => {
    const steps = buildSteps([
      call("Bash", 0),
      call("Bash", 5),
      result("Bash", 20),
      result("Bash", 30),
    ]);

    const lanes = buildLanes(steps, at(0), at(30))!;

    expect(lanes.tool).toHaveLength(1);
    expect(lanes.toolMs).toBe(30_000);
    expect(lanes.modelMs).toBe(0);
  });

  it("returns null when no event carries a timestamp", () => {
    const steps = buildSteps([
      { seq: 1, type: "tool_use", tool: "Bash" },
      { seq: 2, type: "tool_result", tool: "Bash", output: "ok" },
    ]);

    expect(buildLanes(steps, undefined, undefined)).toBeNull();
  });

  it("returns null for a zero-length run rather than dividing by it", () => {
    const steps = buildSteps([call("Bash", 0), result("Bash", 0)]);

    expect(buildLanes(steps, at(0), at(0))).toBeNull();
  });
});

describe("timelineTicks", () => {
  it("keeps tick density constant as the track is zoomed", () => {
    const total = 30 * 60_000;

    // Same run, four times the width: four times the labels, so the on-screen
    // spacing between them does not thin out.
    expect(timelineTicks(total, 6).length).toBeLessThanOrEqual(7);
    expect(timelineTicks(total, 24).length).toBeGreaterThan(timelineTicks(total, 6).length);
  });

  it("returns nothing for a run with no length", () => {
    expect(timelineTicks(0)).toEqual([]);
  });

  it("omits a round tick that would crowd the exact run end", () => {
    expect(timelineTicks(2 * 60_000 + 2_000, 6)).toEqual([
      0,
      30_000,
      60_000,
      90_000,
      122_000,
    ]);
  });
});

describe("laneSegmentPosition", () => {
  const total = 39 * 60_000;

  it("gives a sub-pixel call a visible, clickable minimum width", () => {
    // 40ms of a 39-minute run is well under a pixel at any sane track width,
    // so the floor is what the reader actually sees.
    const { width } = laneSegmentPosition({ startMs: 0, durationMs: 40, kind: "tool" }, total);
    const [, truePct] = width.match(/^max\(3px, ([\d.e-]+)%\)$/) ?? [];
    expect(Number(truePct)).toBeCloseTo(0.0017, 4);
  });

  it("keeps a widened sliver inside the axis instead of past its end", () => {
    // The last thing a run does ends at the run's end, so the final segment is
    // routinely a sliver at ~100%. Left unclamped, its 3px minimum lands beyond
    // the track, and those 3px of scrollable overflow are what gave the
    // timeline a scrollbar it could not settle on (#6278). Capping the offset
    // by the bar's own width is what keeps `left + width` on the axis.
    const last = laneSegmentPosition(
      { startMs: total - 300, durationMs: 300, kind: "think" },
      total,
    );
    expect(last.left).toBe(`min(${((total - 300) / total) * 100}%, 100% - ${last.width})`);
  });

  it("leaves a segment wider than the minimum where it belongs", () => {
    // min() picks the raw offset here: 25% + 50% is comfortably inside 100%.
    const mid = laneSegmentPosition(
      { startMs: total / 4, durationMs: total / 2, kind: "tool" },
      total,
    );
    expect(mid).toEqual({ left: "min(25%, 100% - max(3px, 50%))", width: "max(3px, 50%)" });
  });
});

describe("toolKindTotals", () => {
  it("splits tool time by what the calls were doing", () => {
    const steps = buildSteps([
      { seq: ++seq, type: "tool_use", tool: "Bash", input: { command: "pnpm test" }, created_at: at(0) },
      { seq: ++seq, type: "tool_result", tool: "Bash", output: "ok", created_at: at(60) },
      { seq: ++seq, type: "tool_use", tool: "Edit", input: { file_path: "a.ts", old_string: "a", new_string: "b" }, created_at: at(60) },
      { seq: ++seq, type: "tool_result", tool: "Edit", output: "ok", created_at: at(62) },
      { seq: ++seq, type: "tool_use", tool: "Read", input: { file_path: "b.ts" }, created_at: at(62) },
      { seq: ++seq, type: "tool_result", tool: "Read", output: "ok", created_at: at(63) },
    ]);

    // The lopsidedness is the point: one kind dominates, which is why this is
    // a tooltip and not three more lanes.
    expect(toolKindTotals(steps)).toEqual({
      command: 60_000,
      write: 2_000,
      read: 1_000,
      other: 0,
    });
  });
});

describe("shouldShowTimeline", () => {
  function longRun(stepCount: number, seconds: number) {
    const items: TimelineItem[] = [];
    for (let i = 0; i < stepCount; i++) {
      const start = (seconds / stepCount) * i;
      items.push(call("Bash", start), result("Bash", start + 1));
    }
    return buildSteps(items);
  }

  it("shows for a long run with many steps", () => {
    const steps = longRun(10, 600);
    expect(shouldShowTimeline(steps, buildLanes(steps, at(0), at(600)))).toBe(true);
  });

  it("hides for a short run — a handful of bars says less than the row durations", () => {
    const steps = longRun(3, 600);
    expect(shouldShowTimeline(steps, buildLanes(steps, at(0), at(600)))).toBe(false);
  });

  it("hides for a fast run even with many steps", () => {
    const steps = longRun(10, 20);
    expect(shouldShowTimeline(steps, buildLanes(steps, at(0), at(20)))).toBe(false);
  });

  it("hides when lanes could not be built", () => {
    expect(shouldShowTimeline(longRun(10, 600), null)).toBe(false);
  });
});
