// @vitest-environment node
import { describe, expect, it } from "vitest";
import {
  FALLBACK_GOAL,
  FALLBACK_GOAL_DETAIL,
  GoalDetailSchema,
  GoalResponseSchema,
  GoalsResponseSchema,
  MetricReadingsResponseSchema,
  MetricsResponseSchema,
} from "./goals-schemas";

const goalRow = {
  id: "g-1",
  workspace_id: "ws-1",
  scope_type: "company",
  scope_id: null,
  parent_goal_id: null,
  title: "Grow revenue",
  why: "",
  period_start: "2026-10-01",
  period_end: "2026-12-31",
  owner_type: "member",
  owner_id: "u-1",
  status: "active",
  confidence: "at_risk",
  derived_confidence: "at_risk",
  confidence_override: null,
  confidence_note: "",
  confidence_changed_at: "2026-11-02T10:00:00Z",
  progress: 0.4,
  readings_missing: 0,
  source_proposal_id: null,
  key_results: [
    {
      id: "kr-1",
      goal_id: "g-1",
      title: "Revenue",
      metric_key: "mrr",
      scope_type: "company",
      scope_id: null,
      start_value: 0,
      target_value: 1000,
      weight: 2,
      current_value: 400,
      observed_at: "2026-11-02T00:00:00Z",
      progress: 0.4,
      series: [{ observed_at: "2026-11-01T00:00:00Z", value: 300 }, "garbage"],
    },
  ],
  created_by_type: "member",
  created_by_id: "u-1",
  created_at: "2026-10-01T00:00:00Z",
  updated_at: "2026-11-02T00:00:00Z",
};

describe("goal schemas", () => {
  it("reads a goal with its key results and drops a malformed series point alone", () => {
    const [goal] = GoalsResponseSchema.parse({ goals: [goalRow] });
    expect(goal?.confidence).toBe("at_risk");
    expect(goal?.key_results[0]?.series).toEqual([{ observed_at: "2026-11-01T00:00:00Z", value: 300 }]);
  });

  it("degrades an unknown status and confidence to unknown instead of failing the list", () => {
    const [goal] = GoalsResponseSchema.parse({ goals: [{ ...goalRow, status: "paused", confidence: "panicking" }] });
    expect(goal?.status).toBe("unknown");
    expect(goal?.confidence).toBe("unknown");
  });

  it("drops a goal that is not an object and keeps the rest of the list", () => {
    const goals = GoalsResponseSchema.parse({ goals: ["nope", goalRow] });
    expect(goals).toHaveLength(1);
  });

  it("answers a missing list with none, and a non-list response with none", () => {
    expect(GoalsResponseSchema.parse({})).toEqual([]);
    expect(GoalsResponseSchema.parse({ goals: "oops" })).toEqual([]);
  });

  it("reads a goal detail with its child goals, and an unreadable goal as the empty fallback", () => {
    const detail = GoalDetailSchema.parse({ goal: goalRow, child_goals: [{ id: "c", title: "Child", status: "draft", scope_type: "floor", scope_id: "f" }] });
    expect(detail.child_goals[0]?.title).toBe("Child");
    const broken = GoalDetailSchema.parse({ goal: "not a goal", child_goals: [] });
    expect(broken.goal).toEqual(FALLBACK_GOAL);
    expect(FALLBACK_GOAL_DETAIL.child_goals).toEqual([]);
  });

  it("reads a goal response under goal and falls back when it is absent", () => {
    expect(GoalResponseSchema.parse({ goal: goalRow }).title).toBe("Grow revenue");
    expect(GoalResponseSchema.parse({}).id).toBe(FALLBACK_GOAL.id);
  });

  it("reads metrics, defaulting an unknown source and direction", () => {
    const [metric] = MetricsResponseSchema.parse({
      metrics: [{ key: "runs.count", name: "Runs", unit: "runs", source: "builtin", direction: "sideways" }],
    });
    expect(metric).toMatchObject({ key: "runs.count", source: "builtin", direction: "increase" });
    expect(MetricsResponseSchema.parse({ metrics: [{ key: "x", source: "made-up" }] })[0]?.source).toBe("unknown");
  });

  it("reads readings and drops a reading whose value is not a number", () => {
    const readings = MetricReadingsResponseSchema.parse({
      readings: [
        { id: "r1", metric_key: "mrr", scope_type: "company", value: "12.5", observed_at: "2026-11-02T00:00:00Z" },
        { id: "r2", metric_key: "mrr", scope_type: "company", value: "not-a-number" },
      ],
    });
    expect(readings.map((r) => r.id)).toEqual(["r1"]);
    expect(readings[0]?.value).toBe(12.5);
  });
});
