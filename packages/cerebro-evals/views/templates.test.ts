import { describe, expect, it } from "vitest";
import { EVAL_TEMPLATES, duplicateForm } from "./templates";
import { buildWriteInput } from "./form";
import type { CerebroEval } from "../types";

const KEY_PATTERN = /^[a-z0-9]+(?:-[a-z0-9]+)*$/;

describe("EVAL_TEMPLATES", () => {
  it("offers five starting points with unique ids", () => {
    expect(EVAL_TEMPLATES).toHaveLength(5);
    expect(new Set(EVAL_TEMPLATES.map((t) => t.id)).size).toBe(5);
  });

  it("each template builds a valid, writable draft", () => {
    for (const template of EVAL_TEMPLATES) {
      const form = template.build();
      expect(form.key).toMatch(KEY_PATTERN);
      expect(form.title.length).toBeGreaterThan(0);
      expect(form.tasks.length).toBeGreaterThan(0);

      const input = buildWriteInput(form);
      expect(input.datasets.length).toBeGreaterThan(0);
      // Threshold is always the pass_rate + all_critical_pass pair the runner reads.
      expect(input.thresholds).toHaveLength(2);
      expect(input.status).toBe("draft");
      const passRate = input.thresholds.find((t) => (t as { metric: string }).metric === "pass_rate") as { value: number };
      expect(passRate.value).toBeGreaterThanOrEqual(0);
      expect(passRate.value).toBeLessThanOrEqual(1);
    }
  });

  it("builds distinct task ids each time so adopted tasks never collide", () => {
    const form = EVAL_TEMPLATES[0]!.build();
    const ids = form.tasks.map((t) => t.id);
    expect(new Set(ids).size).toBe(ids.length);
  });

  it("the structured-output template uses a deterministic hard rule", () => {
    const form = EVAL_TEMPLATES.find((t) => t.id === "structured-output-format")!.build();
    expect(form.graderType).toBe("hard_rule");
    const input = buildWriteInput(form);
    expect((input.graders[0] as { type: string }).type).toBe("hard_rule");
  });
});

describe("duplicateForm", () => {
  const original: CerebroEval = {
    id: "e1", workspace_id: "w1", key: "customer-service-quality", version: "2.1.0",
    title: "CS quality", description: "keep tone", status: "active",
    owner: { team: "Support" }, objective: "polite replies",
    target: { kind: "agent", locator: "mention://agent/x", ref: "main" },
    datasets: [
      { id: "task-1", situation: "angry customer", expected: "empathy", critical: true },
      { id: "task-2", situation: "refund request", expected: "policy link", critical: false },
    ],
    graders: [{ id: "grader-1", type: "ai_judge", config: { rubric: "be kind" } }],
    thresholds: [{ metric: "pass_rate", operator: "gte", value: 0.8 }],
    runner: {}, source: { repository: "repo", path: "evals/x/eval.json" },
    created_by_id: "u1", created_by_type: "member", created_at: "", updated_at: "",
  };

  it("clones tasks and grader but gives a distinct key/title and reset version", () => {
    const form = duplicateForm(original);
    expect(form.key).toBe("customer-service-quality-copy");
    expect(form.title).toBe("CS quality (copy)");
    expect(form.version).toBe("1.0.0");
    expect(form.tasks).toHaveLength(2);
    expect(form.tasks[0]?.situation).toBe("angry customer");
    expect(form.graderType).toBe("ai_judge");
    // Target carries over so the clone is runnable; the user can retarget it.
    expect(form.targetKind).toBe("agent");
  });

  it("produces a key that still passes the create-form pattern", () => {
    expect(duplicateForm(original).key).toMatch(/^[a-z0-9]+(?:-[a-z0-9]+)*$/);
  });
});
