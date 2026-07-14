import { describe, expect, it } from "vitest";
import {
  parseCommentTriggerOutcomes,
  unhandledCommentTriggerOutcomes,
} from "./comment-trigger-outcomes";

// MUL-4525 §2: the create/edit comment response's trigger_outcomes drive the
// "posted, but N not triggered" warning, so parsing must be defensive (drop
// malformed entries, tolerate older servers) and count only real blocks.
describe("comment trigger outcomes", () => {
  it("parses valid outcomes and drops malformed entries individually", () => {
    const raw = [
      { target_type: "agent", target_id: "a1", status: "queued", reason_code: "queued" },
      { target_type: "squad", target_id: "s1", status: "blocked", reason_code: "invocation_not_allowed" },
      { status: "blocked" }, // missing target_id → dropped
      "not-an-object", // → dropped
    ];
    const parsed = parseCommentTriggerOutcomes(raw);
    expect(parsed.map((o) => o.target_id)).toEqual(["a1", "s1"]);
  });

  it("returns [] for a missing / non-array field (older server)", () => {
    expect(parseCommentTriggerOutcomes(undefined)).toEqual([]);
    expect(parseCommentTriggerOutcomes(null)).toEqual([]);
    expect(parseCommentTriggerOutcomes("nope")).toEqual([]);
  });

  it("treats only queued/coalesced/deferred as handled; blocked warns", () => {
    const raw = [
      { target_type: "agent", target_id: "a1", status: "queued", reason_code: "queued" },
      { target_type: "agent", target_id: "a2", status: "coalesced", reason_code: "coalesced" },
      { target_type: "agent", target_id: "a3", status: "deferred", reason_code: "deferred" },
      { target_type: "squad", target_id: "s1", status: "blocked", reason_code: "invocation_not_allowed" },
    ];
    const unhandled = unhandledCommentTriggerOutcomes(raw);
    expect(unhandled).toHaveLength(1);
    expect(unhandled[0]?.target_id).toBe("s1");
  });

  it("warns on an unknown / future / empty status instead of assuming success", () => {
    const raw = [
      { target_type: "agent", target_id: "ok", status: "queued", reason_code: "queued" },
      { target_type: "agent", target_id: "future", status: "throttled", reason_code: "x" },
      { target_type: "squad", target_id: "empty", status: "", reason_code: "" },
    ];
    const unhandled = unhandledCommentTriggerOutcomes(raw);
    expect(unhandled.map((o) => o.target_id).sort()).toEqual(["empty", "future"]);
  });
});
