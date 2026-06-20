import { describe, expect, it } from "vitest";

import { frequencyPhrase, summarizeRecurrencePlan } from "./summary";
import type { RecurrenceWriteInput } from "./types";

function completionForm(over: Partial<RecurrenceWriteInput> = {}): RecurrenceWriteInput {
  return {
    frequency: "weekly",
    interval_count: 1,
    weekdays: [1],
    trigger_status: "done",
    anchor: "completion",
    create_new_issue: true,
    new_status: "todo",
    recur_forever: true,
    trigger_type: "completion",
    ...over,
  };
}

function scheduleForm(over: Partial<RecurrenceWriteInput> = {}): RecurrenceWriteInput {
  return {
    frequency: "monthly",
    interval_count: 1,
    weekdays: [],
    new_status: "todo",
    recur_forever: true,
    trigger_type: "schedule",
    stale_handling: "leave_open",
    stale_status: "cancelled",
    ...over,
  };
}

describe("frequencyPhrase", () => {
  it("names the weekday for a weekly rule", () => {
    expect(frequencyPhrase(completionForm({ weekdays: [1] }))).toBe("Every Monday");
  });

  it("joins multiple weekdays", () => {
    expect(frequencyPhrase(completionForm({ weekdays: [3, 1] }))).toBe(
      "Every Monday and Wednesday"
    );
  });

  it("falls back to 'Every week' with no weekday selected", () => {
    expect(frequencyPhrase(completionForm({ weekdays: [] }))).toBe("Every week");
  });

  it("pluralises the interval", () => {
    expect(frequencyPhrase(completionForm({ frequency: "daily", interval_count: 3 }))).toBe(
      "Every 3 days"
    );
  });

  it("handles every_weekday", () => {
    expect(frequencyPhrase(completionForm({ frequency: "every_weekday" }))).toBe(
      "Every weekday"
    );
  });
});

describe("summarizeRecurrencePlan — completion mode", () => {
  it("describes a fresh-copy weekly rule", () => {
    const s = summarizeRecurrencePlan(completionForm());
    expect(s).toBe(
      "Every Monday, when you move this task to Done, Multica creates a fresh copy that starts as To do. The clock counts from the day you finish. It runs forever."
    );
  });

  it("describes reopening the same task", () => {
    const s = summarizeRecurrencePlan(completionForm({ create_new_issue: false }));
    expect(s).toContain("Multica reopens this same task as To do");
  });

  it("reflects the due-date anchor", () => {
    const s = summarizeRecurrencePlan(completionForm({ anchor: "due_date" }));
    expect(s).toContain("The clock counts from its due date.");
  });

  it("reflects a non-forever run with a max-occurrence cap", () => {
    const s = summarizeRecurrencePlan(
      completionForm({ recur_forever: false, max_occurrences: 5 })
    );
    expect(s).toContain("It runs 5 times.");
  });
});

describe("summarizeRecurrencePlan — schedule mode", () => {
  it("describes a monthly schedule that leaves the old one open", () => {
    const s = summarizeRecurrencePlan(scheduleForm());
    expect(s).toBe(
      "Every month, a brand-new task is created — even if the last one isn't done. The old one is left open. It starts as To do. It runs forever."
    );
  });

  it("describes auto-closing the previous occurrence", () => {
    const s = summarizeRecurrencePlan(
      scheduleForm({ stale_handling: "auto_close", stale_status: "cancelled" })
    );
    expect(s).toContain("The old one is closed as Cancelled.");
  });
});
