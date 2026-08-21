// @vitest-environment node

import { describe, expect, it } from "vitest";
import type { AgentTask } from "../types/agent";
import { resolveWorkdirCopyTarget } from "./workdir";

const terminalStatuses = ["completed", "failed", "cancelled"] as const;

function task(
  status: AgentTask["status"],
  createdAt: string,
  workDir?: string,
  durableWorkDir?: string,
) {
  return {
    status,
    created_at: createdAt,
    work_dir: workDir,
    relative_work_dir: workDir
      ? `relative/${workDir.split("/").pop()}`
      : undefined,
    durable_work_dir: durableWorkDir,
    relative_durable_work_dir: durableWorkDir ? "project" : undefined,
    branch_name: durableWorkDir ? "agent/j/abc12345" : undefined,
  };
}

describe("resolveWorkdirCopyTarget", () => {
  it.each(terminalStatuses)(
    "uses a daemon-confirmed durable directory for a %s task",
    (status) => {
      expect(
        resolveWorkdirCopyTarget([
          task(
            status,
            "2026-08-18T10:00:00Z",
            "/managed/task/worktree",
            "/Users/dev/project",
          ),
        ]),
      ).toEqual({
        path: "/Users/dev/project",
        relativePath: "project",
        branchName: "agent/j/abc12345",
        source: "durable_project_directory",
      });
    },
  );

  it("keeps a running task on its actual workdir", () => {
    expect(
      resolveWorkdirCopyTarget([
        task(
          "running",
          "2026-08-18T10:00:00Z",
          "/managed/task/worktree",
          "/Users/dev/project",
        ),
      ]),
    ).toMatchObject({
      path: "/managed/task/worktree",
      source: "task_workdir",
    });
  });

  it("treats an in-place task workdir as durable without inventing a handoff", () => {
    expect(
      resolveWorkdirCopyTarget([
        task(
          "completed",
          "2026-08-18T10:00:00Z",
          "/Users/dev/project",
        ),
      ]),
    ).toMatchObject({
      path: "/Users/dev/project",
      source: "task_workdir",
    });
  });

  it.each(terminalStatuses)(
    "keeps a terminal %s task on work_dir when no durable handoff was reported",
    (status) => {
      expect(
        resolveWorkdirCopyTarget([
          task(status, "2026-08-18T10:00:00Z", "/preserved/worktree"),
        ]),
      ).toMatchObject({
        path: "/preserved/worktree",
        source: "task_workdir",
      });
    },
  );

  it("uses the newest task with a usable path", () => {
    expect(
      resolveWorkdirCopyTarget([
        task("completed", "2026-08-18T09:00:00Z", "/older"),
        task(
          "completed",
          "2026-08-18T10:00:00Z",
          "/newer-worktree",
          "/newer-project",
        ),
      ]),
    ).toMatchObject({ path: "/newer-project" });
  });

  it("does not trust a durable path on a live task without work_dir", () => {
    expect(
      resolveWorkdirCopyTarget([
        task("running", "2026-08-18T10:00:00Z", undefined, "/project"),
      ]),
    ).toBeUndefined();
  });

  it("returns undefined when no task has a usable path", () => {
    expect(
      resolveWorkdirCopyTarget([
        task("completed", "2026-08-18T10:00:00Z"),
      ]),
    ).toBeUndefined();
  });
});
