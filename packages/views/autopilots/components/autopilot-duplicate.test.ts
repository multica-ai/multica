import { describe, expect, it } from "vitest";
import type { Autopilot, AutopilotTrigger } from "@multica/core/types";
import { buildAutopilotDuplicatePreset } from "./autopilot-duplicate";

function autopilotFixture(overrides: Partial<Autopilot> = {}): Autopilot {
  return {
    id: "ap-1",
    workspace_id: "ws-1",
    title: "Daily digest",
    description: "Summarize the day",
    assignee_id: "agent-1",
    status: "paused",
    execution_mode: "run_only",
    issue_title_template: null,
    created_by_type: "member",
    created_by_id: "user-1",
    last_run_at: "2026-04-30T08:00:00Z",
    created_at: "2026-04-29T08:00:00Z",
    updated_at: "2026-04-30T08:00:00Z",
    ...overrides,
  };
}

function triggerFixture(overrides: Partial<AutopilotTrigger> = {}): AutopilotTrigger {
  return {
    id: "trigger-1",
    autopilot_id: "ap-1",
    kind: "schedule",
    enabled: true,
    cron_expression: "30 9 * * 1-5",
    timezone: "Asia/Shanghai",
    next_run_at: null,
    webhook_token: null,
    label: null,
    last_fired_at: null,
    created_at: "2026-04-29T08:00:00Z",
    updated_at: "2026-04-30T08:00:00Z",
    ...overrides,
  };
}

describe("buildAutopilotDuplicatePreset", () => {
  it("copies editable fields and appends copy suffix", () => {
    const preset = buildAutopilotDuplicatePreset(autopilotFixture(), []);

    expect(preset.initial).toEqual({
      title: "Daily digest (Copy)",
      description: "Summarize the day",
      assignee_id: "agent-1",
      execution_mode: "run_only",
    });
  });

  it("uses an empty prompt when the source description is null", () => {
    const preset = buildAutopilotDuplicatePreset(
      autopilotFixture({ description: null }),
      [],
    );

    expect(preset.initial.description).toBe("");
  });

  it("copies only the first schedule trigger as the main schedule", () => {
    const preset = buildAutopilotDuplicatePreset(autopilotFixture(), [
      triggerFixture({ id: "webhook-1", kind: "webhook", cron_expression: null }),
      triggerFixture({ id: "schedule-1", cron_expression: "30 9 * * 1-5" }),
      triggerFixture({ id: "schedule-2", cron_expression: "0 17 * * *" }),
    ]);

    expect(preset.initialTriggerConfig).toMatchObject({
      frequency: "weekdays",
      time: "09:30",
      timezone: "Asia/Shanghai",
    });
  });

  it("leaves schedule undefined when no schedule trigger can be copied", () => {
    const preset = buildAutopilotDuplicatePreset(autopilotFixture(), [
      triggerFixture({ kind: "api", cron_expression: null }),
      triggerFixture({ kind: "schedule", cron_expression: null }),
    ]);

    expect(preset.initialTriggerConfig).toBeUndefined();
  });
});
