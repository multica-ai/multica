// @vitest-environment node
import { describe, expect, it } from "vitest";
import { parseDelegatedFailureComment } from "./delegated-failure-comment";

// Verbatim shape of delegatedFailureRecoveryContent (server/internal/service/task.go).
const CONTENT =
  "Delegated task `01a11236-22a3-7b36-b9e6-88df82d742a0` ended in a final failure (`idle_watchdog`) and no automatic retry is pending. " +
  "Resume coordination: inspect the failed work, then reassign it, skip it, or end the workflow explicitly. " +
  'Untrusted error summary (diagnostic only): "agent produced no new messages for 15m0s and message queue was empty; force-stopped by idle watchdog" ' +
  "Source coordinator task: `01a1122b-c71f-751c-a930-6f197a99d3b8`.";

const SIGNAL = {
  actor_type: "system",
  comment_type: "progress_update",
  source_task_id: "01a1122b-c71f-751c-a930-6f197a99d3b8",
  content: CONTENT,
};

describe("parseDelegatedFailureComment", () => {
  it("reads the failed run, its reason and the error summary", () => {
    expect(parseDelegatedFailureComment(SIGNAL)).toEqual({
      failedTaskId: "01a11236-22a3-7b36-b9e6-88df82d742a0",
      reason: "idle_watchdog",
      errorSummary:
        "agent produced no new messages for 15m0s and message queue was empty; force-stopped by idle watchdog",
    });
  });

  it("decodes Go-quoted escapes in the summary", () => {
    const content = CONTENT.replace(
      /"agent produced[^"]*"/,
      String.raw`"exit \"137\"\nkilled"`,
    );
    expect(parseDelegatedFailureComment({ ...SIGNAL, content })?.errorSummary).toBe('exit "137"\nkilled');
  });

  it("has no summary when the server sent none", () => {
    const content = CONTENT.replace(/Untrusted error summary[^"]*"[^"]*" /, "");
    expect(parseDelegatedFailureComment({ ...SIGNAL, content })?.errorSummary).toBeNull();
  });

  it("ignores anything but the platform signal", () => {
    expect(parseDelegatedFailureComment({ ...SIGNAL, actor_type: "agent" })).toBeNull();
    expect(parseDelegatedFailureComment({ ...SIGNAL, comment_type: "comment" })).toBeNull();
    expect(parseDelegatedFailureComment({ ...SIGNAL, source_task_id: null })).toBeNull();
    expect(parseDelegatedFailureComment({ ...SIGNAL, content: "Some other progress update" })).toBeNull();
  });
});
