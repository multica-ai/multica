import { describe, expect, it } from "vitest";
import { parseWithFallback } from "@multica/core/api";
import { evalRunsListSchema, evalsListSchema } from "./schemas";
import type { EvalRun } from "./types";

const emptyRuns: { runs: EvalRun[] } = { runs: [] };

describe("eval API schemas", () => {
  it("fails malformed responses closed to an empty catalog", () => {
    const result = parseWithFallback({ evals: null }, evalsListSchema, { evals: [] }, { endpoint: "test" });
    expect(result.evals).toEqual([]);
  });

  it("parses a run whether or not the backend sends issue_key", () => {
    const withKey = parseWithFallback(
      { runs: [{ id: "r1", workspace_id: "w1", eval_id: "e1", eval_version: "1.0.0", status: "passed", issue_key: "MUL-12", created_at: "2026-07-18T06:00:00Z" }] },
      evalRunsListSchema, emptyRuns, { endpoint: "test" },
    );
    expect(withKey.runs[0]?.issue_key).toBe("MUL-12");

    // An older backend omits issue_key entirely — must still parse, not fall back.
    const withoutKey = parseWithFallback(
      { runs: [{ id: "r2", workspace_id: "w1", eval_id: "e1", eval_version: "1.0.0", status: "failed", created_at: "2026-07-18T06:00:00Z" }] },
      evalRunsListSchema, emptyRuns, { endpoint: "test" },
    );
    expect(withoutKey.runs).toHaveLength(1);
    expect(withoutKey.runs[0]?.issue_key).toBeUndefined();
  });
});
