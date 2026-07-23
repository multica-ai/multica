import { describe, expect, it } from "vitest";
import { buildWriteInput, defaultSourcePath, EMPTY_FORM, formFromEval, slugify, TARGET_KINDS, withDerivedIdentity } from "./form";
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

// The identity fields used to be the first thing the create form asked for —
// nine required inputs, including a code-repo file path, before the eval's own
// content. They are now derived from the title until the user overrides them.
describe("derived identity", () => {
  it("slugifies a title into a catalog key the key field accepts", () => {
    expect(slugify("Customer-service reply quality")).toBe("customer-service-reply-quality");
    expect(slugify("  Refunds & returns!  ")).toBe("refunds-returns");
    expect(slugify("")).toBe("");
    // The derived key must satisfy the pattern the Key input validates against.
    expect(slugify("Answer Quality 2")).toMatch(/^[a-z0-9]+(?:-[a-z0-9]+)*$/);
  });

  it("files an in-app eval under its key by default", () => {
    expect(defaultSourcePath("answer-quality")).toBe("evals/answer-quality/eval.json");
    expect(defaultSourcePath("")).toBe("");
  });

  it("follows the title while the identity fields are untouched", () => {
    const form = { ...EMPTY_FORM, title: "Answer quality" };
    const derived = withDerivedIdentity(form, false, false);
    expect(derived.key).toBe("answer-quality");
    expect(derived.sourcePath).toBe("evals/answer-quality/eval.json");
  });

  it("never overwrites a key or source path the user typed", () => {
    const form = { ...EMPTY_FORM, title: "Answer quality", key: "my-own-key", sourcePath: "custom/path.json" };
    const derived = withDerivedIdentity(form, true, true);
    expect(derived.key).toBe("my-own-key");
    expect(derived.sourcePath).toBe("custom/path.json");
  });

  it("still derives the source path from a hand-typed key", () => {
    const form = { ...EMPTY_FORM, title: "Answer quality", key: "my-own-key" };
    const derived = withDerivedIdentity(form, true, false);
    expect(derived.sourcePath).toBe("evals/my-own-key/eval.json");
  });

  it("offers only target kinds the runner can resolve", () => {
    // Target type was free text, so a typo saved an eval that could never run.
    expect(TARGET_KINDS).toContain("agent");
    expect(TARGET_KINDS).toContain("workflow");
    expect(TARGET_KINDS).toContain(EMPTY_FORM.targetKind);
  });
});
