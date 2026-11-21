import { describe, expect, it } from "vitest";
import type { AgentTask } from "@multica/core/types";
import {
  matchesCallerFilter,
  resolveAgentCallerId,
  taskCallerUserId,
} from "./activity-tab";

const NOW = Date.UTC(2026, 5, 15, 12, 0, 0);

function task(overrides: Partial<AgentTask> = {}): AgentTask {
  return {
    id: "task-1",
    agent_id: "agent-1",
    runtime_id: "runtime-1",
    status: "completed",
    issue_id: "",
    started_at: new Date(NOW - 10 * 60 * 1000).toISOString(),
    completed_at: new Date(NOW - 5 * 60 * 1000).toISOString(),
    ...overrides,
  } as AgentTask;
}

describe("resolveAgentCallerId", () => {
  it("defaults to the signed-in user when no override was picked", () => {
    expect(resolveAgentCallerId(undefined, "user-1")).toBe("user-1");
    expect(resolveAgentCallerId(undefined, null)).toBeNull();
  });

  it("maps the explicit anyone value to no filter and passes members through", () => {
    expect(resolveAgentCallerId("all", "user-1")).toBeNull();
    expect(resolveAgentCallerId("user-2", "user-1")).toBe("user-2");
  });
});

describe("taskCallerUserId", () => {
  it("prefers the attribution originator over the flat column", () => {
    expect(
      taskCallerUserId(
        task({
          attribution: {
            source: "direct_human",
            precise: true,
            originator: { id: "via-attribution", name: "Ada" },
          },
          originator_user_id: "via-column",
        }),
      ),
    ).toBe("via-attribution");
  });

  it("falls back to the flat originator column", () => {
    expect(taskCallerUserId(task({ originator_user_id: "via-column" }))).toBe(
      "via-column",
    );
  });

  it("is null when neither source names a caller", () => {
    expect(taskCallerUserId(task())).toBeNull();
  });
});

describe("matchesCallerFilter", () => {
  it("keeps every task when no caller is set", () => {
    expect(matchesCallerFilter(task(), null)).toBe(true);
  });

  it("matches only tasks attributed to the caller", () => {
    const t = task({ originator_user_id: "user-1" });
    expect(matchesCallerFilter(t, "user-1")).toBe(true);
    expect(matchesCallerFilter(t, "user-2")).toBe(false);
  });
});

