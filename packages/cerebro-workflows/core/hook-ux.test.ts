import { describe, expect, it } from "vitest";
import { createHookDraft, HOOK_EVENT_OPTIONS } from "./hook-types";
import { validateHook } from "./hook-validation";
import {
  ACTION_CONFIGURATION,
  EMPTY_HOOK_FILTERS,
  HOOK_TEMPLATES,
  enforcedDecisionSummary,
  filterHooks,
  hookDraftState,
  hookFilterOptions,
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
    expect(fieldDefinition("continuation.present")).toMatchObject({ label: "Continuation registered", input: "boolean" });
    // Fields the suggestion list does not know are marked, not trusted.
    expect(fieldDefinition("message.body")).toMatchObject({ known: false });
  });

  it("provides useful starter recipes plus a scratch option", () => {
    expect(HOOK_TEMPLATES.length).toBeGreaterThanOrEqual(5);
    expect(HOOK_TEMPLATES.length).toBeLessThanOrEqual(9);
    expect(HOOK_TEMPLATES.some((template) => template.id === "scratch")).toBe(true);
    expect(HOOK_TEMPLATES.filter((template) => template.id !== "scratch").every((template) => template.hook.events.length > 0)).toBe(true);
  });

  it("keeps all eight guided recipes structurally saveable as This workspace Drafts", () => {
    const recipes = HOOK_TEMPLATES.filter((template) => template.id !== "scratch");

    expect(recipes).toHaveLength(8);
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
    expect(recipe?.hook.conditions).toEqual([{ field: "failure.attempt", operator: "gte", value: "$event.failure.max_attempts" }]);
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
    // message.body is not a field the server sends, so it is marked unknown —
    // and its free-text value stays redacted.
    expect(describeHook(hook)).toContain("starts with <redacted> (unknown field)");
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

describe("Reading a hook list without opening every hook (FIR-4797)", () => {
  const withLive = Object.assign(createHookDraft(), {
    id: "draft-1", family_id: "family-1", name: "Chain approval",
    contract_rule: "Approve the final step.", contract_satisfy: "Approve it.",
    events: ["before.issue.status_change" as const], bindings: [{ kind: "workspace" as const, value: "" }],
    decision: "allow" as const, actions: [],
    lifecycle: { state: "live_with_draft" as const, live_policy_id: "live-1", live_version: 1, draft_id: "draft-1", live_unchanged_by_draft: true },
    live: { policy_id: "live-1", version: 1, name: "Chain approval", decision: "block" as const, requirement: "Approve it.", fail_mode: "closed" as const },
  });
  const plain = Object.assign(createHookDraft(), {
    id: "plain", family_id: "family-2", name: "Message guard",
    contract_rule: "Address someone.", contract_satisfy: "Mention a recipient.",
    events: ["before.message.send" as const], bindings: [{ kind: "agent" as const, value: "agent-1" }],
    decision: "require" as const, requirement: "Mention a recipient.",
    actions: [{ type: "issue.comment", label: "Comment on issue", config: { body: "hi" } }],
    mode: "enforce" as const,
    lifecycle: { state: "live" as const, live_policy_id: "live-2", live_version: 1, live_unchanged_by_draft: false },
  });

  it("reports the decision that is enforcing, not the unpublished draft's", () => {
    expect(enforcedDecisionSummary(withLive)).toBe("Stop the action");
    expect(decisionSummary(withLive)).toBe("Guide (let it continue)");
  });

  it("separates a draft that cannot be published from a hook that is broken", () => {
    const draft = hookDraftState(withLive);

    expect(draft).toMatchObject({ present: true, publishable: false, overLive: true });
    expect(draft.blocker).toBeTruthy();
    expect(hookDraftState(plain)).toMatchObject({ present: false, publishable: true });
  });

  it("offers a filter for every value the hooks on screen actually use", () => {
    const options = hookFilterOptions([withLive, plain]);

    expect(options.trigger.map((option) => option.value)).toEqual(["before.issue.status_change", "before.message.send"]);
    expect(options.decision.map((option) => option.value).sort()).toEqual(["block", "require"]);
    expect(options.scope.map((option) => option.value).sort()).toEqual(["agent", "workspace"]);
    expect(options.action.map((option) => option.value)).toEqual(["issue.comment"]);
  });

  it("filters on trigger, decision, scope, and action — not only on state", () => {
    const hooks = [withLive, plain];

    expect(filterHooks(hooks, { ...EMPTY_HOOK_FILTERS, trigger: "before.message.send" })).toEqual([plain]);
    expect(filterHooks(hooks, { ...EMPTY_HOOK_FILTERS, decision: "block" })).toEqual([withLive]);
    expect(filterHooks(hooks, { ...EMPTY_HOOK_FILTERS, scope: "agent" })).toEqual([plain]);
    expect(filterHooks(hooks, { ...EMPTY_HOOK_FILTERS, action: "issue.comment" })).toEqual([plain]);
    expect(filterHooks(hooks, { ...EMPTY_HOOK_FILTERS, attention: true })).toEqual([withLive]);
  });

  it("names the symbolic action target instead of calling it an unknown target", () => {
    const instruct = Object.assign(createHookDraft(), {
      events: ["on.task.failure" as const], bindings: [{ kind: "workspace" as const, value: "" }],
      actions: [{ type: "agent.dispatch", label: "Instruct an agent", config: { agent_id: "event.agent", prompt: "Try again." } }],
    });

    expect(describeHook(instruct)).toContain("The agent that triggered this hook");
    expect(describeHook(instruct)).not.toContain("Unknown target");
  });
});

describe("The editor must not call a working hook broken (FIR-4797)", () => {
  const platformHook = Object.assign(createHookDraft(), {
    name: "Require a recipient on agent comments",
    contract_rule: "An agent comment must address someone.",
    contract_satisfy: "Name the recipient.",
    mode: "managed" as const,
    events: ["before.message.send" as const],
    bindings: [{ kind: "workspace" as const, value: "" }],
    // Fields the platform's own hooks use, which the editor's suggestion list
    // never contained.
    conditions: [{ field: "message.agent_authored", operator: "eq", value: "true" }],
    decision: "require" as const, requirement: "Name the recipient.",
    actions: [],
  });

  it("accepts a filter field the suggestion list does not know", () => {
    expect(validateHook(platformHook)).toEqual({ valid: true });
  });

  it("accepts a Require decision with no action, because the requirement IS the instruction", () => {
    expect(validateHook({ ...platformHook, conditions: [] })).toEqual({ valid: true });
  });

  it("still demands an action from a hook that only guides", () => {
    const guiding = { ...platformHook, decision: "allow" as const, requirement: "", actions: [] };

    expect(validateHook(guiding).valid).toBe(false);
  });

  it("prints the value of a known boolean field instead of hiding it as redacted", () => {
    const summary = describeHook(platformHook);

    expect(summary).toContain("Message · agent authored is Yes");
    expect(summary).not.toContain("<redacted>");
    expect(summary).not.toContain("unknown field");
  });

  it("marks fields the server does not send as unknown (FIR-4933)", () => {
    const futuristic = { ...platformHook, conditions: [{ field: "message.channel_id", operator: "eq", value: "chan-1" }] };

    expect(describeHook(futuristic)).toContain("(unknown field)");
  });

  it("still hides free text and identifiers", () => {
    const secretive = { ...platformHook, conditions: [{ field: "message.body", operator: "starts_with", value: "Hi Jesper" }] };

    expect(describeHook(secretive)).toContain("<redacted>");
    expect(describeHook(secretive)).not.toContain("Hi Jesper");
  });
});
