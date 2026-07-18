import { describe, expect, it } from "vitest";
import { buildWriteInput, EMPTY_FORM, formFromEval } from "./form";
import type { CerebroEval } from "../types";

const sampleEval: CerebroEval = {
  id: "e1", workspace_id: "w1", key: "customer-service-quality", version: "2.1.0",
  title: "CS quality", description: "keep tone", status: "active",
  owner: { team: "Support" }, objective: "polite replies",
  target: { kind: "agent", locator: "mention://agent/x", ref: "main", extra: "keep-me" },
  datasets: [
    { id: "task-1", situation: "angry customer", expected: "empathy", critical: true },
    { id: "task-2", situation: "refund request", expected: "policy link", critical: false },
  ],
  graders: [{ id: "grader-1", type: "ai_judge", config: { rubric: "be kind", model: "claude" } }],
  thresholds: [
    { metric: "pass_rate", operator: "gte", value: 0.8 },
    { metric: "all_critical_pass", operator: "eq", value: true },
  ],
  runner: { concurrency: 2 }, source: { repository: "repo", commit: "abc", path: "p.json", origin: "keep-me" },
  created_by_id: "u1", created_by_type: "member",
  created_at: "2026-07-18T00:00:00Z", updated_at: "2026-07-18T00:00:00Z",
};

describe("eval form mapping", () => {
  it("round-trips metadata, tasks, grader and threshold through the form", () => {
    const form = formFromEval(sampleEval);
    expect(form.key).toBe("customer-service-quality");
    expect(form.targetKind).toBe("agent");
    expect(form.tasks).toHaveLength(2);
    expect(form.tasks[0]).toMatchObject({ situation: "angry customer", expected: "empathy", critical: true });
    expect(form.graderType).toBe("ai_judge");
    expect(form.graderRubric).toBe("be kind");
    expect(form.passRate).toBe("80");
    expect(form.requireAllCritical).toBe(true);
  });

  it("re-serializes tasks, grader and threshold in the runner's shape", () => {
    const input = buildWriteInput(formFromEval(sampleEval), sampleEval);
    expect(input.datasets).toEqual([
      { id: "task-1", situation: "angry customer", expected: "empathy", critical: true },
      { id: "task-2", situation: "refund request", expected: "policy link", critical: false },
    ]);
    expect(input.graders).toEqual([{ id: "grader-1", type: "ai_judge", config: { rubric: "be kind", model: "claude" } }]);
    expect(input.thresholds).toEqual([
      { metric: "pass_rate", operator: "gte", value: 0.8 },
      { metric: "all_critical_pass", operator: "eq", value: true },
    ]);
  });

  it("preserves runner config, owner, description and status the form cannot show", () => {
    const input = buildWriteInput(formFromEval(sampleEval), sampleEval);
    expect(input.runner).toEqual(sampleEval.runner);
    expect(input.owner).toEqual({ team: "Support" });
    expect(input.description).toBe("keep tone");
    expect(input.status).toBe("active");
    expect(input.target.extra).toBe("keep-me");
    expect(input.source.origin).toBe("keep-me");
  });

  it("drops blank tasks and builds a hard_rule grader with only its own config", () => {
    const form = {
      ...EMPTY_FORM, key: "k", title: "t", objective: "o",
      graderType: "hard_rule", graderMatch: "contains", graderRubric: "ignored",
      tasks: [
        { id: "a", situation: "real", expected: "yes", critical: false },
        { id: "b", situation: "   ", expected: "blank", critical: false },
      ],
    };
    const input = buildWriteInput(form);
    expect(input.datasets).toHaveLength(1);
    expect(input.graders).toEqual([{ id: "grader-1", type: "hard_rule", config: { match: "contains" } }]);
  });

  it("uses fail-closed draft defaults for a fresh create", () => {
    const input = buildWriteInput({ ...EMPTY_FORM, key: "new-eval", title: "New", objective: "obj" });
    expect(input.status).toBe("draft");
    expect(input.datasets).toEqual([]);
    expect(input.owner).toEqual({ team: "Firtal AI" });
    expect(input.thresholds).toEqual([
      { metric: "pass_rate", operator: "gte", value: 1 },
      { metric: "all_critical_pass", operator: "eq", value: true },
    ]);
  });
});
