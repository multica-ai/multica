import { describe, expect, it } from "vitest";
import type { AgentTask } from "@multica/core/types";
import { buildRunChat, deriveElapsedMs, isTerminalRun } from "./build-run-chat";

function task(overrides: Partial<AgentTask> = {}): AgentTask {
  return {
    id: "task-1",
    agent_id: "agent-1",
    runtime_id: "runtime-1",
    issue_id: "issue-1",
    status: "completed",
    priority: 0,
    dispatched_at: "2026-06-16T09:00:00.000Z",
    started_at: "2026-06-16T09:00:01.000Z",
    completed_at: "2026-06-16T09:00:39.000Z",
    result: null,
    error: null,
    created_at: "2026-06-16T09:00:00.000Z",
    trigger_summary: "Fix the login redirect",
    ...overrides,
  };
}

describe("buildRunChat", () => {
  it("terminal run yields a user bubble then exactly one assistant message", () => {
    const messages = buildRunChat(task());

    expect(messages).toHaveLength(2);
    expect(messages[0]?.role).toBe("user");
    expect(messages[0]?.content).toBe("Fix the login redirect");
    // user bubble must not carry a task_id, or it would be treated as a run.
    expect(messages[0]?.task_id).toBeNull();

    expect(messages[1]?.role).toBe("assistant");
    expect(messages[1]?.task_id).toBe("task-1");
    // exactly one assistant message — multiple would duplicate the whole run.
    expect(messages.filter((m) => m.role === "assistant")).toHaveLength(1);
  });

  it("in-flight run yields the user bubble only (live stream comes via pendingTask)", () => {
    for (const status of ["queued", "dispatched", "waiting_local_directory", "running"] as const) {
      const messages = buildRunChat(task({ status, completed_at: null }));
      expect(messages).toHaveLength(1);
      expect(messages[0]?.role).toBe("user");
      expect(messages.some((m) => m.role === "assistant")).toBe(false);
    }
  });

  it("derives elapsed_ms from started_at -> completed_at", () => {
    const messages = buildRunChat(task());
    expect(messages[1]?.elapsed_ms).toBe(38_000);
  });

  it("strips mention markdown from the trigger bubble", () => {
    const messages = buildRunChat(
      task({ trigger_summary: "[@Implementer](mention://agent/abc) please fix it" }),
    );
    expect(messages[0]?.content).toBe("@Implementer please fix it");
  });

  it("carries the failure reason and raw error for a failed run", () => {
    const messages = buildRunChat(
      task({ status: "failed", failure_reason: "agent_error", error: "boom" }),
    );
    const assistant = messages.find((m) => m.role === "assistant");
    expect(assistant?.failure_reason).toBe("agent_error");
    expect(assistant?.content).toBe("boom");
  });

  it("redacts secrets in the raw error before it reaches the failure bubble", () => {
    const messages = buildRunChat(
      task({
        status: "failed",
        failure_reason: "agent_error",
        error:
          "psql failed: postgres://admin:s3cr3t@db.internal:5432/app (token ghp_0123456789abcdef0123456789abcdef0123)",
      }),
    );
    const assistant = messages.find((m) => m.role === "assistant");
    expect(assistant?.content).not.toContain("s3cr3t");
    expect(assistant?.content).not.toContain(
      "ghp_0123456789abcdef0123456789abcdef0123",
    );
    expect(assistant?.content).toContain("[REDACTED CONNECTION STRING]");
    expect(assistant?.content).toContain("[REDACTED GITHUB TOKEN]");
  });

  it("a cancelled run is terminal and emits an assistant message with no failure reason", () => {
    const messages = buildRunChat(task({ status: "cancelled" }));
    const assistant = messages.find((m) => m.role === "assistant");
    expect(assistant).toBeTruthy();
    expect(assistant?.failure_reason).toBeNull();
  });

  it("omits the trigger bubble when there is no trigger summary (empty run)", () => {
    const messages = buildRunChat(task({ trigger_summary: undefined }));
    // terminal with no trigger → assistant message only.
    expect(messages).toHaveLength(1);
    expect(messages[0]?.role).toBe("assistant");
  });

  it("an in-flight run with no trigger yields no messages (empty live state)", () => {
    const messages = buildRunChat(
      task({ status: "running", completed_at: null, trigger_summary: undefined }),
    );
    expect(messages).toHaveLength(0);
  });

  it("includeTrigger:false suppresses the user bubble", () => {
    const messages = buildRunChat(task(), { includeTrigger: false });
    expect(messages.every((m) => m.role !== "user")).toBe(true);
  });
});

describe("deriveElapsedMs", () => {
  it("returns null when completed_at is missing", () => {
    expect(deriveElapsedMs(task({ completed_at: null }))).toBeNull();
  });

  it("returns null for non-positive durations", () => {
    expect(
      deriveElapsedMs(
        task({
          started_at: "2026-06-16T09:00:39.000Z",
          completed_at: "2026-06-16T09:00:39.000Z",
        }),
      ),
    ).toBeNull();
  });

  it("falls back to dispatched_at then created_at for the start anchor", () => {
    expect(
      deriveElapsedMs(
        task({
          started_at: null,
          dispatched_at: "2026-06-16T09:00:00.000Z",
          completed_at: "2026-06-16T09:00:10.000Z",
        }),
      ),
    ).toBe(10_000);
  });
});

describe("isTerminalRun", () => {
  it("classifies completed/failed/cancelled as terminal and others as active", () => {
    expect(isTerminalRun(task({ status: "completed" }))).toBe(true);
    expect(isTerminalRun(task({ status: "failed" }))).toBe(true);
    expect(isTerminalRun(task({ status: "cancelled" }))).toBe(true);
    expect(isTerminalRun(task({ status: "running" }))).toBe(false);
    expect(isTerminalRun(task({ status: "queued" }))).toBe(false);
  });
});
