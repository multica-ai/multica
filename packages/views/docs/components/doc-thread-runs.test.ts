// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { DocTaskRun } from "@multica/core/types";
import { docThreadRuns } from "./doc-thread-runs";

function run(id: string, agentId: string, docThreadId: string, status: string): DocTaskRun {
  return { id, agentId, docThreadId, status, failureReason: "", error: null, createdAt: "", completedAt: null, canCancel: true };
}

describe("docThreadRuns", () => {
  it("shows each agent's latest run on the discussion while it works or after it failed", () => {
    // Newest first, as the server lists them.
    const runs = [
      run("t5", "writer", "c1", "running"),
      run("t4", "reviewer", "c1", "failed"),
      run("t3", "other", "c2", "queued"),
      run("t2", "writer", "c1", "failed"),
      run("t1", "editor", "c1", "completed"),
    ];
    expect(docThreadRuns(runs, "c1").map((r) => r.id)).toEqual(["t4", "t5"]);
    expect(docThreadRuns(runs, "c2").map((r) => r.id)).toEqual(["t3"]);
  });

  it("keeps a working run visible when a follow-up queues behind it", () => {
    const runs = [run("t2", "writer", "c1", "queued"), run("t1", "writer", "c1", "running")];
    expect(docThreadRuns(runs, "c1").map((r) => r.id)).toEqual(["t1", "t2"]);
  });

  it("keeps a retry waiting for the runtime visible, in place of the failure it retries", () => {
    const runs = [run("t2", "writer", "c1", "deferred"), run("t1", "writer", "c1", "failed")];
    expect(docThreadRuns(runs, "c1").map((r) => r.id)).toEqual(["t2"]);
  });

  it("drops a failure once a newer run of the same agent finished", () => {
    const runs = [run("t2", "writer", "c1", "completed"), run("t1", "writer", "c1", "failed")];
    expect(docThreadRuns(runs, "c1")).toEqual([]);
  });
});
