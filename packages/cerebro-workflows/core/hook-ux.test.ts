import { describe, expect, it } from "vitest";
import { createHookDraft, HOOK_EVENT_OPTIONS } from "./hook-types";
import { validateHook } from "./hook-validation";
import {
  ACTION_CONFIGURATION,
  HOOK_TEMPLATES,
  decisionSummary,
  describeHook,
  failModeSummary,
  fieldDefinition,
  filterSummary,
  scopeSummary,
  stepSummary,
  triggerSummary,
} from "./hook-ux";

describe("Hooks UX contract", () => {
  it("offers every supported event with plain-language copy and advanced metadata", () => {
    expect(HOOK_EVENT_OPTIONS.map((option) => option.value)).toEqual(expect.arrayContaining([
      "on.task.failure",
      "on.wakeup.fire_failure",
      "before.session.start",
      "on.error",
    ]));
    expect(HOOK_EVENT_OPTIONS.every((option) => option.description.length > 0)).toBe(true);
  });

  it("defines typed configuration for every visible action", () => {
    const required = [
      "member.notify", "wakeup.create", "wakeup.cancel", "approval.require",
      "metric.increment", "artifact.create_or_update", "task.retry", "task.cancel",
      "audit.record", "session.handoff", "judge.gate",
    ];
    expect(required.every((type) => (ACTION_CONFIGURATION[type]?.fields.length ?? 0) > 0)).toBe(true);
    expect(ACTION_CONFIGURATION["wakeup.create"]?.fields.find((field) => field.key === "fire_at")?.input).toBe("datetime-local");
    expect(ACTION_CONFIGURATION["task.retry"]?.fields.find((field) => field.key === "task_id")?.required).toBe(true);
  });

  it("exposes eval.run and eval.gate actions targeting an eval (FIR-3496)", () => {
    expect(ACTION_CONFIGURATION["eval.run"]?.fields[0]).toMatchObject({ key: "eval_id", target: "eval", required: true });
    expect(ACTION_CONFIGURATION["eval.gate"]?.fields[0]).toMatchObject({ key: "eval_id", target: "eval", required: true });
  });

  it("uses human labels and typed value metadata for filter fields", () => {
    expect(fieldDefinition("issue.status")).toMatchObject({ label: "Issue status", input: "select" });
    expect(fieldDefinition("attempt")).toMatchObject({ label: "Attempt number", input: "number" });
    expect(fieldDefinition("continuation")).toMatchObject({ label: "Continuation registered", input: "boolean" });
  });

  it("provides useful starter recipes plus a scratch option", () => {
    expect(HOOK_TEMPLATES.length).toBeGreaterThanOrEqual(5);
    expect(HOOK_TEMPLATES.length).toBeLessThanOrEqual(8);
    expect(HOOK_TEMPLATES.some((template) => template.id === "scratch")).toBe(true);
    expect(HOOK_TEMPLATES.filter((template) => template.id !== "scratch").every((template) => template.hook.events.length > 0)).toBe(true);
  });

  it("keeps all seven guided recipes structurally saveable as This workspace Drafts", () => {
    const recipes = HOOK_TEMPLATES.filter((template) => template.id !== "scratch");

    expect(recipes).toHaveLength(7);
    expect(recipes.every((template) =>
      template.hook.events.length > 0
      && template.hook.bindings.some((binding) => binding.kind === "workspace" && binding.value === "")
      && template.hook.actions.every((action) => ACTION_CONFIGURATION[action.type] !== undefined),
    )).toBe(true);
  });

  it("names the missing action choice in an incomplete recipe", () => {
    const recipe = HOOK_TEMPLATES.find((template) => template.id === "notify-task-failure");

    expect(recipe).toBeDefined();
    expect(validateHook(recipe!.hook)).toEqual({
      valid: false,
      message: "Choose Member for Notify member.",
    });
  });

  it("ships the no-silent-failure recipe guarding terminal task failures", () => {
    const recipe = HOOK_TEMPLATES.find((template) => template.id === "no-silent-failure");
    expect(recipe?.hook.events).toEqual(["on.task.failure"]);
    expect(recipe?.hook.conditions).toEqual([{ field: "retry.pending", operator: "eq", value: "false" }]);
    const actionTypes = recipe?.hook.actions.map((action) => action.type);
    expect(actionTypes).toEqual(["issue.comment", "issue.status"]);
    expect(recipe?.hook.actions.at(1)?.config.status).toBe("blocked");
  });

  it("ships the think-before-comment recipe wiring before.message.send to a judge gate", () => {
    const recipe = HOOK_TEMPLATES.find((template) => template.id === "think-before-comment");
    expect(recipe?.hook.events).toEqual(["before.message.send"]);
    expect(recipe?.hook.decision).toBe("require");
    const judge = recipe?.hook.actions.find((action) => action.type === "judge.gate");
    expect(judge).toBeDefined();
    expect(String(judge?.config.rubric ?? "")).toMatch(/wakeup/i);
    expect(String(judge?.config.rubric ?? "")).toMatch(/hand off|handoff/i);
  });

  it("keeps chain and overview summaries in sync with the current draft", () => {
    const directory = {
      issue: [{ value: "issue-1", label: "Fulfilment launch" }],
      member: [{ value: "member-1", label: "Jesper" }],
    };
    const hook = {
      ...createHookDraft(),
      name: "Protect completed work",
      events: ["before.task.complete" as const],
      bindings: [{ kind: "issue" as const, value: "issue-1" }],
      conditions: [{ field: "issue.status", operator: "eq", value: "done" }],
      decision: "block" as const,
      requirement: "Add a continuation",
      actions: [{ type: "member.notify", label: "Notify member", config: { member_id: "member-1", title: "Sensitive title", message: "Sensitive message" } }],
    };
    expect(triggerSummary(hook)).toBe("Before task completes");
    expect(scopeSummary(hook, directory)).toBe("Fulfilment launch");
    expect(filterSummary(hook)).toBe("All: Issue status is Done");
    expect(decisionSummary(hook)).toBe("Stop the action");
    expect(failModeSummary(hook)).toBe("Continue and log");
    expect(stepSummary(hook, "when", directory)).toBe("Before task completes · Fulfilment launch · All: Issue status is Done");
    expect(stepSummary(hook, "guide")).toBe("Stop the action");
    expect(stepSummary(hook, "actions", directory)).toBe("Notify member — Member: Jesper; Title: <redacted>; Message: <redacted>");
    expect(describeHook(hook, directory)).toBe("When Before task completes for Fulfilment launch, if all Issue status is Done, Stop the action, then Notify member — Member: Jesper; Title: <redacted>; Message: <redacted>.");
    expect(describeHook(hook, directory)).not.toContain("issue-1");
    expect(describeHook(hook, directory)).not.toContain("member-1");
    expect(describeHook(hook, directory)).not.toContain("Sensitive");
  });

  it("never exposes unresolved target IDs or sensitive condition values", () => {
    const hook = {
      ...createHookDraft(),
      events: ["before.message.send" as const],
      bindings: [{ kind: "issue" as const, value: "private-issue-id" }],
      conditions: [{ field: "message.body", operator: "starts_with", value: "private message" }],
      actions: [{ type: "agent.dispatch", label: "Start agent", config: { agent_id: "private-agent-id" } }],
    };

    expect(describeHook(hook)).toContain("Unknown target");
    expect(describeHook(hook)).toContain("Message text starts with <redacted>");
    expect(describeHook(hook)).not.toContain("private-issue-id");
    expect(describeHook(hook)).not.toContain("private-agent-id");
    expect(describeHook(hook)).not.toContain("private message");
  });

  it("names the canonical empty workspace scope as This workspace", () => {
    const hook = {
      ...createHookDraft(),
      bindings: [{ kind: "workspace" as const, value: "" }],
    };

    expect(scopeSummary(hook)).toBe("This workspace");
  });
});
