import { describe, expect, it } from "vitest";
import type { TaskMessagePayload } from "@multica/core/types";
import { relativeTime, stringsFor } from "../strings";
import { sortByPhase } from "./issues";
import { runSteps, stepLabel } from "./steps";
import { isActiveTask, runPhase } from "./tasks";

const message = (seq: number, type: TaskMessagePayload["type"], extra: Partial<TaskMessagePayload> = {}): TaskMessagePayload => ({ task_id: "t", issue_id: "i", seq, type, ...extra });

describe("stepLabel", () => {
  it("names the tool and the argument that says what it did", () => {
    expect(stepLabel({ tool: "Read", input: { file_path: "/repo/apps/mobile/src/ui/theme/colors.ts" } })).toBe("Read colors.ts");
    expect(stepLabel({ tool: "Bash", input: { command: "pnpm test\necho done" } })).toBe("Bash pnpm test");
    expect(stepLabel({ tool: "Task", input: {} })).toBe("Task");
  });

  it("shortens long arguments", () => {
    expect(stepLabel({ tool: "Bash", input: { command: "x".repeat(100) } })).toHaveLength("Bash ".length + 64);
  });
});

describe("runSteps", () => {
  const messages = [
    message(1, "text", { content: "Looking" }),
    message(2, "tool_use", { tool: "Read", call_id: "c1", input: { path: "a.ts" } }),
    message(3, "tool_result", { call_id: "c1" }),
    message(4, "tool_use", { tool: "Bash", call_id: "c2", input: { command: "pnpm lint" } }),
  ];

  it("keeps the last call open while the run is live", () => {
    expect(runSteps(messages, true).map((step) => [step.label, step.done])).toEqual([
      ["Read a.ts", true],
      ["Bash pnpm lint", false],
    ]);
  });

  it("closes every call once the run is over", () => {
    expect(runSteps(messages, false).every((step) => step.done)).toBe(true);
  });
});

describe("task phases", () => {
  it("groups statuses into what the UI shows", () => {
    expect(runPhase("waiting_local_directory")).toBe("queued");
    expect(runPhase("running")).toBe("running");
    expect(runPhase("cancelled")).toBe("done");
    expect(isActiveTask({ status: "dispatched" })).toBe(true);
    expect(isActiveTask({ status: "failed" })).toBe(false);
  });
});

describe("sortByPhase", () => {
  it("puts work in flight first and keeps order within a phase", () => {
    const issues = [
      { id: "a", c: "done" },
      { id: "b", c: "unstarted" },
      { id: "c", c: "started" },
      { id: "d", c: "unstarted" },
      { id: "e", c: "closed" },
    ] as const;
    expect(sortByPhase(issues, (issue) => issue.c).map((issue) => issue.id)).toEqual(["c", "b", "d", "a", "e"]);
  });
});

describe("relativeTime", () => {
  const now = Date.parse("2026-11-14T12:00:00Z");
  it("uses short relative units, then a date", () => {
    const en = stringsFor("en");
    expect(relativeTime("2026-11-14T11:59:40Z", en, now)).toBe("now");
    expect(relativeTime("2026-11-14T09:00:00Z", en, now)).toBe("3h");
    expect(relativeTime("2026-11-13T09:00:00Z", en, now)).toBe("Yesterday");
    expect(relativeTime("2026-10-01T09:00:00Z", en, now)).toBe("10/1");
    expect(relativeTime("2026-11-14T11:30:00Z", stringsFor("zh-Hans"), now)).toBe("30 分钟");
  });
});
