// @vitest-environment node
import { describe, expect, it } from "vitest";
import { buildWakeupInput, emptyWakeupDraft, wakesAssigneeOnComments, type WakeupDraft } from "./wakeup-draft";

const now = new Date(2026, 8, 24, 15, 0, 0);
const draft = (patch: Partial<WakeupDraft>): WakeupDraft => ({
  ...emptyWakeupDraft("agent", "Asia/Shanghai", now),
  instruction: "Check the result",
  ...patch,
});

describe("buildWakeupInput", () => {
  it("requires a condition, an agent and bounded instructions", () => {
    expect(buildWakeupInput(draft({}), now)).toEqual({ error: "missing_condition" });
    expect(buildWakeupInput(draft({ condition: "at", agentId: "" }), now)).toEqual({ error: "missing_agent" });
    expect(buildWakeupInput(draft({ condition: "at", instruction: "  " }), now)).toEqual({ error: "instruction_invalid" });
    expect(buildWakeupInput(draft({ condition: "at", instruction: "字".repeat(4001) }), now)).toEqual({ error: "instruction_invalid" });
  });

  it("schedules a single time from a preset or a future custom time", () => {
    expect(buildWakeupInput(draft({ condition: "at", atPreset: "10m" }), now)).toEqual({
      input: { agent_id: "agent", instruction: "Check the result", kind: "at", mode: "once", at: new Date(2026, 8, 24, 15, 10).toISOString() },
    });
    const tomorrow = buildWakeupInput(draft({ condition: "at", atPreset: "tomorrow" }), now);
    expect(tomorrow).toMatchObject({ input: { at: new Date(2026, 8, 25, 9, 0).toISOString() } });
    expect(buildWakeupInput(draft({ condition: "at", atPreset: "custom", atCustom: "2026-09-24T14:00" }), now)).toEqual({ error: "future_time" });
    expect(buildWakeupInput(draft({ condition: "at", atPreset: "custom", atCustom: "" }), now)).toEqual({ error: "future_time" });
  });

  it("gives recurring checks an end date and the viewer's timezone", () => {
    const daily = buildWakeupInput(draft({ condition: "recurring", recurrence: "daily", until: "2026-09-30" }), now);
    expect(daily).toEqual({
      input: {
        agent_id: "agent", instruction: "Check the result", kind: "cron", cron_expression: "0 9 * * *",
        timezone: "Asia/Shanghai", mode: "continuous", expires_at: new Date(2026, 8, 30, 23, 59, 59).toISOString(),
      },
    });
    expect(buildWakeupInput(draft({ condition: "recurring", recurrence: "hourly", until: "2026-09-30" }), now)).toMatchObject({
      input: { kind: "every", interval_seconds: 3600 },
    });
    expect(buildWakeupInput(draft({ condition: "recurring", recurrence: "weekdays", until: "2026-09-30" }), now)).toMatchObject({
      input: { cron_expression: "0 9 * * 1-5" },
    });
    expect(buildWakeupInput(draft({ condition: "recurring", until: "2026-09-23" }), now)).toEqual({ error: "until_future" });
  });

  it("waits for a reply with a deadline and a timeout action", () => {
    expect(buildWakeupInput(draft({ condition: "reply", replyActor: { type: "member", id: "user" }, waitDays: 3 }), now)).toEqual({
      input: {
        agent_id: "agent", instruction: "Check the result", kind: "event", mode: "once",
        expires_in_seconds: 259200, on_timeout: "wake", event_types: ["comment.created"],
        filter_actor_type: "member", filter_actor_id: "user",
      },
    });
    const anyone = buildWakeupInput(draft({ condition: "reply", mode: "continuous", onTimeout: "end" }), now);
    expect(anyone).toMatchObject({ input: { mode: "continuous", on_timeout: "end" } });
    expect("input" in anyone && anyone.input.filter_actor_type).toBeFalsy();
  });

  it("waits for runs to end, optionally from one agent", () => {
    expect(buildWakeupInput(draft({ condition: "run_end", runAgentId: "emacs" }), now)).toMatchObject({
      input: { event_types: ["task.completed", "task.failed", "task.cancelled"], filter_agent_id: "emacs" },
    });
  });

  it("requires at least one custom event", () => {
    expect(buildWakeupInput(draft({ condition: "custom" }), now)).toEqual({ error: "missing_events" });
    expect(buildWakeupInput(draft({ condition: "custom", events: ["issue.labels_changed"] }), now)).toMatchObject({
      input: { event_types: ["issue.labels_changed"] },
    });
  });
});

describe("platform conditions", () => {
  const input = (patch: Partial<WakeupDraft>, propertyType?: string) => buildWakeupInput(draft(patch), now, propertyType);

  it("turns each condition choice into the predicate the platform checks", () => {
    expect(input({ condition: "field", field: "status", fieldTarget: "in_review" })).toMatchObject({
      input: { kind: "event", mode: "once", condition: { type: "issue_field", field: "status", value: "in_review" }, expires_in_seconds: 604800 },
    });
    expect(input({ condition: "field", field: "assignee", assignee: { type: "squad", id: "sq" } })).toMatchObject({
      input: { condition: { type: "issue_field", field: "assignee", assignee_type: "squad", assignee_id: "sq" } },
    });
    expect(input({ condition: "field", field: "label", fieldTarget: "lbl" })).toMatchObject({
      input: { condition: { type: "issue_field", field: "label", label_id: "lbl" } },
    });
    expect(input({ condition: "children", stage: 2 })).toMatchObject({ input: { condition: { type: "children_done", stage: 2 } } });
    expect(input({ condition: "children", stage: null })).toMatchObject({ input: { condition: { type: "children_done" } } });
    expect(input({ condition: "pull_request", prEvent: "merged" })).toMatchObject({ input: { condition: { type: "pull_request", event: "merged" } } });
    expect(input({ condition: "other_issue", otherIssue: { id: "i2", identifier: "MUL-2" }, otherState: "ended" })).toMatchObject({
      input: { condition: { type: "other_issue", issue_id: "i2", state: "ended" } },
    });
    // Conditions never send raw events.
    const children = input({ condition: "children" });
    expect("input" in children && children.input.event_types).toBeUndefined();
  });

  it("types a property value the way the property stores it", () => {
    const property = (fieldValue: string, type: string) =>
      input({ condition: "field", field: "property", fieldTarget: "p", fieldValue }, type);
    expect(property("opt-1", "select")).toMatchObject({ input: { condition: { property_id: "p", value: "opt-1" } } });
    expect(property("false", "checkbox")).toMatchObject({ input: { condition: { value: false } } });
    expect(property("3", "number")).toMatchObject({ input: { condition: { value: 3 } } });
    expect(property("3a", "number")).toMatchObject({ input: { condition: { value: "3a" } } });
  });

  it("asks for the missing value or issue", () => {
    expect(input({ condition: "field", field: "status" })).toEqual({ error: "missing_value" });
    expect(input({ condition: "field", field: "assignee" })).toEqual({ error: "missing_value" });
    expect(input({ condition: "field", field: "property", fieldTarget: "p", fieldValue: "  " })).toEqual({ error: "missing_value" });
    expect(input({ condition: "other_issue" })).toEqual({ error: "missing_issue" });
  });

  it("caps a repeating wait and leaves a single one uncapped", () => {
    expect(input({ condition: "reply", mode: "continuous", maxFires: 10 })).toMatchObject({ input: { max_fires: 10 } });
    const once = input({ condition: "reply" });
    expect("input" in once && once.input.max_fires).toBeUndefined();
  });
});

describe("wakesAssigneeOnComments", () => {
  it("flags rules that wake the agent assignee on members' comments", () => {
    const reply = draft({ condition: "reply", agentId: "agent" });
    expect(wakesAssigneeOnComments(reply, "agent")).toBe(true);
    expect(wakesAssigneeOnComments({ ...reply, replyActor: { type: "member", id: "u" } }, "agent")).toBe(true);
    // Agents' comments do not start the assignee's runs.
    expect(wakesAssigneeOnComments({ ...reply, replyActor: { type: "agent", id: "a" } }, "agent")).toBe(false);
    expect(wakesAssigneeOnComments(draft({ condition: "custom", agentId: "agent", events: ["comment.created"] }), "agent")).toBe(true);
    expect(wakesAssigneeOnComments(draft({ condition: "custom", agentId: "agent", events: ["issue.status_changed"] }), "agent")).toBe(false);
    expect(wakesAssigneeOnComments(reply, "someone-else")).toBe(false);
    expect(wakesAssigneeOnComments(reply, null)).toBe(false);
  });
});
