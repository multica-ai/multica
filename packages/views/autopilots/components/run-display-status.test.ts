// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { AutopilotRun } from "@multica/core/types";
import { runDisplayStatus } from "./run-display-status";

describe("runDisplayStatus", () => {
  it.each([
    [{ status: "running", task_status: "queued" }, "queued"],
    [{ status: "running", task_status: "dispatched" }, "running"],
    [{ status: "running", task_status: "running" }, "running"],
    // Older server: no task status, keep what the run says.
    [{ status: "running", task_status: null }, "running"],
    // Only a running run can be queued; a finished run keeps its outcome.
    [{ status: "completed", task_status: "queued" }, "completed"],
    [{ status: "issue_created", task_status: null }, "issue_created"],
    [{ status: "failed", task_status: "failed" }, "failed"],
    [{ status: "skipped", task_status: null }, "skipped"],
    // Unknown status from a newer server.
    [{ status: "paused_by_future", task_status: null }, "issue_created"],
  ])("%j -> %s", (run, want) => {
    expect(runDisplayStatus(run as Pick<AutopilotRun, "status" | "task_status">)).toBe(want);
  });
});
