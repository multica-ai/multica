import { describe, expect, it } from "vitest";
import { AgentTaskListSchema, TaskMessageListSchema } from "./schemas";

describe("mobile task audit schemas", () => {
  it("redacts nested runtime fields from task results", () => {
    const tasks = AgentTaskListSchema.parse([
      {
        id: "task-1",
        agent_id: "agent-1",
        runtime_id: "runtime-private",
        issue_id: "issue-1",
        status: "completed",
        priority: 0,
        dispatched_at: null,
        started_at: null,
        completed_at: "2026-06-20T00:00:00Z",
        result: {
          summary: "finished",
          runtime_id: "runtime-private",
          runtimeId: "runtime-private",
          runtime_detail_url: "/api/runtimes/runtime-private",
          workDir: "/Users/alice/.multica/workspaces/ws/task/workdir",
          steps: [
            { name: "safe" },
            {
              name: "unsafe",
              daemon_operation_params: { runtime_id: "runtime-private" },
              daemonOperationParams: { runtimeId: "runtime-private" },
            },
          ],
        },
        error: null,
        created_at: "2026-06-20T00:00:00Z",
        work_dir: "/Users/alice/.multica/workspaces/ws/task/workdir",
        prior_work_dir: "/Users/alice/.multica/workspaces/ws/old/workdir",
        relative_work_dir: "ws/task/workdir",
      },
    ]);

    expect(tasks[0]?.runtime_id).toBe("");
    expect(tasks[0]?.execution_mode).toBe("normal");
    const rawTask = tasks[0] as unknown as Record<string, unknown>;
    expect("work_dir" in rawTask).toBe(false);
    expect("prior_work_dir" in rawTask).toBe(false);
    expect(tasks[0]?.relative_work_dir).toBe("ws/task/workdir");
    const result = tasks[0]?.result as Record<string, unknown>;
    expect(result.summary).toBe("finished");
    expect("runtime_id" in result).toBe(false);
    expect("runtimeId" in result).toBe(false);
    expect("runtime_detail_url" in result).toBe(false);
    expect("workDir" in result).toBe(false);
    const steps = result.steps as Array<Record<string, unknown>>;
    expect(steps[0]?.name).toBe("safe");
    expect(steps[1]?.name).toBe("unsafe");
    expect("daemon_operation_params" in steps[1]!).toBe(false);
    expect("daemonOperationParams" in steps[1]!).toBe(false);
  });

  it("preserves task execution mode when present", () => {
    const tasks = AgentTaskListSchema.parse([
      {
        id: "task-goal",
        agent_id: "agent-1",
        runtime_id: "runtime-private",
        issue_id: "issue-1",
        execution_mode: "goal",
        status: "queued",
        priority: 0,
        dispatched_at: null,
        started_at: null,
        completed_at: null,
        result: null,
        error: null,
        created_at: "2026-06-20T00:00:00Z",
      },
    ]);

    expect(tasks[0]?.execution_mode).toBe("goal");
  });

  it("redacts runtime invocation fields from task message inputs", () => {
    const messages = TaskMessageListSchema.parse([
      {
        task_id: "task-1",
        issue_id: "issue-1",
        seq: 1,
        type: "tool_use",
        input: {
          path: "README.md",
          runtime_id: "runtime-private",
          runtimeId: "runtime-private",
          connection_credentials: { token: "secret" },
          daemonOperationParams: { runtimeId: "runtime-private" },
          nested: {
            keep: "safe",
            runtime_id: "runtime-private",
          },
        },
      },
    ]);

    const input = messages[0]?.input ?? {};
    expect(input.path).toBe("README.md");
    expect("runtime_id" in input).toBe(false);
    expect("runtimeId" in input).toBe(false);
    expect("connection_credentials" in input).toBe(false);
    expect("daemonOperationParams" in input).toBe(false);
    const nested = input.nested as Record<string, unknown>;
    expect(nested.keep).toBe("safe");
    expect("runtime_id" in nested).toBe(false);
  });
});
