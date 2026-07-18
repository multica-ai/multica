import { describe, expect, it } from "vitest";
import { describeGrader, describeThreshold, nextTransitions, statusInput, versionsForKey } from "./lifecycle";
import type { CerebroEval, EvalStatus } from "../types";

function makeEval(overrides: Partial<CerebroEval> = {}): CerebroEval {
  return {
    id: "e1", workspace_id: "w1", key: "customer-service-quality", version: "1.0.0",
    title: "Customer-service reply quality", description: "", status: "draft",
    owner: { team: "Firtal AI" }, objective: "Answer accurately and on policy.",
    target: { kind: "agent", locator: "mention://agent/x", ref: "main" },
    datasets: [{ id: "t1", situation: "Angry refund", expected: "offer refund", critical: true }],
    graders: [{ id: "grader-1", type: "ai_judge", config: { model: "gpt-judge" } }],
    thresholds: [
      { metric: "pass_rate", operator: "gte", value: 0.8 },
      { metric: "all_critical_pass", operator: "eq", value: true },
    ],
    runner: { engine: "v2" }, source: { repository: "https://github.com/firtal-group/firtal-evals", commit: "abc", path: "evals/x/eval.json" },
    created_by_id: "u1", created_by_type: "member",
    created_at: "2026-07-18T06:00:00Z", updated_at: "2026-07-18T06:00:00Z",
    ...overrides,
  };
}

describe("nextTransitions", () => {
  it("moves a draft only to active", () => {
    expect(nextTransitions("draft").map((t) => t.to)).toEqual(["active"]);
  });
  it("lets an active eval pause or retire, and confirms retire", () => {
    const t = nextTransitions("active");
    expect(t.map((x) => x.to)).toEqual(["paused", "retired"]);
    expect(t.find((x) => x.to === "retired")?.confirm).toBe(true);
    expect(t.find((x) => x.to === "paused")?.confirm).toBeUndefined();
  });
  it("lets a paused eval resume or retire", () => {
    expect(nextTransitions("paused").map((t) => t.to)).toEqual(["active", "retired"]);
  });
  it("gives a retired eval no further moves", () => {
    expect(nextTransitions("retired")).toEqual([]);
  });
});

describe("statusInput", () => {
  it("flips only the status and carries every other field through", () => {
    const input = statusInput(makeEval({ status: "draft" }), "active");
    expect(input.status).toBe("active");
    // Tasks, grader, threshold and runner survive the round-trip unchanged.
    expect(input.datasets).toHaveLength(1);
    expect((input.datasets[0] as { situation: string }).situation).toBe("Angry refund");
    expect((input.graders[0] as { type: string }).type).toBe("ai_judge");
    expect(input.thresholds).toHaveLength(2);
    expect(input.runner).toEqual({ engine: "v2" });
    expect(input.key).toBe("customer-service-quality");
    expect(input.version).toBe("1.0.0");
  });

  it("can retire an active eval", () => {
    expect(statusInput(makeEval({ status: "active" }), "retired").status).toBe("retired");
  });
});

describe("versionsForKey", () => {
  it("returns only same-key evals, newest first", () => {
    const evals = [
      makeEval({ id: "a", key: "k1", version: "1.0.0", created_at: "2026-07-01T00:00:00Z" }),
      makeEval({ id: "b", key: "k1", version: "2.0.0", created_at: "2026-07-10T00:00:00Z" }),
      makeEval({ id: "c", key: "k2", version: "1.0.0", created_at: "2026-07-20T00:00:00Z" }),
    ];
    expect(versionsForKey(evals, "k1").map((e) => e.id)).toEqual(["b", "a"]);
  });
});

describe("describeGrader / describeThreshold", () => {
  it("summarizes an AI judge with its model", () => {
    expect(describeGrader(makeEval())).toBe("AI judge · gpt-judge");
  });
  it("summarizes a hard-rule grader with its match", () => {
    const item = makeEval({ graders: [{ id: "grader-1", type: "hard_rule", config: { match: "icontains" } }] });
    expect(describeGrader(item)).toBe("Hard rule · icontains");
  });
  it("summarizes the pass rule as a percent plus the critical rule", () => {
    expect(describeThreshold(makeEval())).toBe("Pass rate ≥ 80% · every critical task must pass");
  });
  it("drops the critical clause when it is off", () => {
    const item = makeEval({ thresholds: [{ metric: "pass_rate", operator: "gte", value: 0.5 }, { metric: "all_critical_pass", operator: "eq", value: false }] });
    expect(describeThreshold(item)).toBe("Pass rate ≥ 50%");
  });
});

// Type guard: every EvalStatus is handled by nextTransitions without a default.
const ALL_STATUSES: EvalStatus[] = ["draft", "active", "paused", "retired"];
describe("nextTransitions covers every status", () => {
  it("never throws for a known status", () => {
    for (const status of ALL_STATUSES) expect(() => nextTransitions(status)).not.toThrow();
  });
});
