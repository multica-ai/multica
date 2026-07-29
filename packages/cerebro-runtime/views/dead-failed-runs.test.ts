// FIR-3901 — the inbox reducer over dead failed runs.

import { describe, expect, it } from "vitest";
import {
  buildInboxFailedRunHints,
  parseFailedRunsPayload,
  type DeadFailedRun,
} from "./dead-failed-runs";

function run(over: Partial<DeadFailedRun> = {}): DeadFailedRun {
  return {
    task_id: "t1",
    issue_id: "i1",
    attempt: 1,
    max_attempts: 2,
    resume_possible: true,
    ...over,
  };
}

describe("buildInboxFailedRunHints", () => {
  it("returns one hint per issue", () => {
    const map = buildInboxFailedRunHints([
      run({ task_id: "a", issue_id: "i1" }),
      run({ task_id: "b", issue_id: "i2" }),
    ]);
    expect(map.size).toBe(2);
    expect(map.get("i1")).toBeDefined();
    expect(map.get("i2")).toBeDefined();
  });

  it("keeps the first (newest) run when an issue has several failures", () => {
    // The server orders newest-first, so the first entry must win.
    const map = buildInboxFailedRunHints([
      run({ task_id: "newest", issue_id: "i1", resume_possible: true }),
      run({ task_id: "older", issue_id: "i1", resume_possible: false }),
    ]);
    expect(map.get("i1")?.resumePossible).toBe(true);
  });

  it("says the run can be continued only when the server allows it", () => {
    const map = buildInboxFailedRunHints([
      run({ issue_id: "yes", resume_possible: true }),
      run({ issue_id: "no", resume_possible: false }),
    ]);
    expect(map.get("yes")?.title).toBe("Run failed — can be continued");
    expect(map.get("no")?.title).toBe("Run failed");
    expect(map.get("no")?.resumePossible).toBe(false);
  });

  it("treats a missing resume_possible as not resumable", () => {
    // Enum/shape drift from an older or newer server must not render a
    // Resume button that would silently start a blank run.
    const map = buildInboxFailedRunHints([
      { task_id: "t", issue_id: "i1", attempt: 1, max_attempts: 2 } as DeadFailedRun,
    ]);
    expect(map.get("i1")?.resumePossible).toBe(false);
  });

  it("ignores rows without an issue id", () => {
    const map = buildInboxFailedRunHints([
      run({ issue_id: "" }),
      { task_id: "x", attempt: 1, max_attempts: 2, resume_possible: true } as DeadFailedRun,
    ]);
    expect(map.size).toBe(0);
  });

  it("survives an empty list", () => {
    expect(buildInboxFailedRunHints([]).size).toBe(0);
  });

  // FIR-4073 — a paused machine is not a failure: grey pip, own wording, and
  // never resumable by the user (the platform is what picks the run back up).
  it("marks a run on a paused machine as paused, not failed", () => {
    const map = buildInboxFailedRunHints([
      run({ issue_id: "i1", runtime_paused: true, unpause_at: "2026-07-29T16:00:00Z" }),
    ]);
    expect(map.get("i1")?.paused).toBe(true);
    expect(map.get("i1")?.title).toBe("Waiting — the machine is paused");
    expect(map.get("i1")?.resumePossible).toBe(false);
  });

  it("says a pause needs a human when there is no unpause time", () => {
    // No unpause_at means the auto-pause circuit breaker gave up.
    const map = buildInboxFailedRunHints([run({ issue_id: "i1", runtime_paused: true })]);
    expect(map.get("i1")?.title).toBe("Paused — needs a human to resume");
  });

  it("leaves a normal failure red", () => {
    const map = buildInboxFailedRunHints([run({ issue_id: "i1", runtime_paused: false })]);
    expect(map.get("i1")?.paused).toBe(false);
    expect(map.get("i1")?.title).toBe("Run failed — can be continued");
  });
});

describe("parseFailedRunsPayload", () => {
  const endpoint = "GET /test";

  it("returns the runs of a well-formed body", () => {
    const runs = parseFailedRunsPayload({ runs: [run()] }, endpoint);
    expect(runs).toHaveLength(1);
    expect(runs[0]?.task_id).toBe("t1");
  });

  it("keeps fields the schema does not name", () => {
    // A newer server may add fields; they must survive to the UI, not be stripped.
    const runs = parseFailedRunsPayload(
      { runs: [{ ...run(), blocked_reason: "runtime offline" }] },
      endpoint,
    );
    expect(runs[0]?.blocked_reason).toBe("runtime offline");
  });

  it("degrades to an empty list when runs is not an array", () => {
    expect(parseFailedRunsPayload({ runs: "nope" }, endpoint)).toEqual([]);
  });

  it("degrades to an empty list when a run is missing required fields", () => {
    expect(parseFailedRunsPayload({ runs: [{ task_id: "t" }] }, endpoint)).toEqual([]);
  });

  it("degrades to an empty list on null, undefined, and a non-object body", () => {
    expect(parseFailedRunsPayload(null, endpoint)).toEqual([]);
    expect(parseFailedRunsPayload(undefined, endpoint)).toEqual([]);
    expect(parseFailedRunsPayload("boom", endpoint)).toEqual([]);
  });

  it("returns an empty list when runs is absent", () => {
    expect(parseFailedRunsPayload({}, endpoint)).toEqual([]);
  });
});
