import { describe, expect, it } from "vitest";
import type { AgentRuntime, AgentTask } from "../types";
import {
  TAKE_OVER_PROVIDERS,
  canTakeOverLocally,
  findResumableTask,
  isTakeOverProvider,
} from "./take-over";

function makeTask(overrides: Partial<AgentTask> = {}): AgentTask {
  return {
    id: "t-1",
    agent_id: "a-1",
    runtime_id: "rt-1",
    issue_id: "i-1",
    status: "completed",
    priority: 0,
    dispatched_at: null,
    started_at: null,
    completed_at: "2026-04-29T12:00:00Z",
    result: { session_id: "sess-1", work_dir: "/tmp/work" },
    error: null,
    created_at: "2026-04-29T11:55:00Z",
    ...overrides,
  };
}

function makeRuntime(overrides: Partial<AgentRuntime> = {}): AgentRuntime {
  return {
    id: "rt-1",
    workspace_id: "ws-1",
    daemon_id: null,
    name: "claude-runtime",
    runtime_mode: "local",
    provider: "claude",
    launch_header: "",
    status: "online",
    device_info: "",
    metadata: {},
    owner_id: null,
    last_seen_at: null,
    created_at: "2026-04-01T00:00:00Z",
    updated_at: "2026-04-01T00:00:00Z",
    ...overrides,
  };
}

describe("isTakeOverProvider", () => {
  it("matches each canonical provider name (case-insensitive)", () => {
    for (const p of TAKE_OVER_PROVIDERS) {
      expect(isTakeOverProvider(p)).toBe(true);
      expect(isTakeOverProvider(p.toUpperCase())).toBe(true);
    }
  });

  it("rejects empty / unknown / null", () => {
    expect(isTakeOverProvider(null)).toBe(false);
    expect(isTakeOverProvider(undefined)).toBe(false);
    expect(isTakeOverProvider("")).toBe(false);
    expect(isTakeOverProvider("openai")).toBe(false);
  });
});

describe("findResumableTask", () => {
  it("returns the newest completed task with a session id and supported provider", () => {
    const tasks: AgentTask[] = [
      makeTask({
        id: "old",
        completed_at: "2026-04-29T10:00:00Z",
        result: { session_id: "old-sess", work_dir: "/old" },
      }),
      makeTask({
        id: "new",
        completed_at: "2026-04-29T12:00:00Z",
        result: { session_id: "new-sess", work_dir: "/new" },
      }),
    ];
    const runtimes = [makeRuntime()];

    const found = findResumableTask(tasks, runtimes);
    expect(found?.task.id).toBe("new");
    expect(found?.provider).toBe("claude");
  });

  it("skips non-completed tasks", () => {
    const tasks: AgentTask[] = [
      makeTask({ id: "running", status: "running" }),
      makeTask({
        id: "old-done",
        completed_at: "2026-04-29T08:00:00Z",
        result: { session_id: "sess", work_dir: "/x" },
      }),
    ];
    expect(findResumableTask(tasks, [makeRuntime()])?.task.id).toBe("old-done");
  });

  it("skips completed tasks without a session_id", () => {
    const tasks: AgentTask[] = [
      makeTask({ id: "no-session", result: { work_dir: "/x" } }),
    ];
    expect(findResumableTask(tasks, [makeRuntime()])).toBeNull();
  });

  it("skips tasks whose runtime provider is not supported", () => {
    const tasks = [makeTask()];
    const runtimes = [makeRuntime({ provider: "openai" })];
    expect(findResumableTask(tasks, runtimes)).toBeNull();
  });

  it("skips tasks whose runtime is missing from the workspace runtimes list", () => {
    const tasks = [makeTask({ runtime_id: "ghost" })];
    expect(findResumableTask(tasks, [makeRuntime()])).toBeNull();
  });

  it("falls back to created_at when completed_at is missing", () => {
    const tasks: AgentTask[] = [
      makeTask({
        id: "older-completed",
        completed_at: null,
        created_at: "2026-04-29T09:00:00Z",
      }),
      makeTask({
        id: "newer-completed",
        completed_at: null,
        created_at: "2026-04-29T11:00:00Z",
      }),
    ];
    expect(findResumableTask(tasks, [makeRuntime()])?.task.id).toBe(
      "newer-completed",
    );
  });
});

describe("canTakeOverLocally", () => {
  const tasks = [makeTask()];
  const runtimes = [makeRuntime()];

  it("returns true on desktop with a resumable run", () => {
    expect(canTakeOverLocally({ tasks, runtimes, clientType: "desktop" })).toBe(
      true,
    );
  });

  it("returns false on web even with a resumable run", () => {
    expect(canTakeOverLocally({ tasks, runtimes, clientType: "web" })).toBe(
      false,
    );
  });

  it("returns false on desktop when there is no resumable run", () => {
    expect(
      canTakeOverLocally({ tasks: [], runtimes, clientType: "desktop" }),
    ).toBe(false);
  });
});
