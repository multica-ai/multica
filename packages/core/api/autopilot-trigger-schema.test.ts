// @vitest-environment node
import { describe, it, expect } from "vitest";
import {
  AutopilotTriggerSchema,
  EMPTY_AUTOPILOT_TRIGGER,
  GetAutopilotResponseSchema,
  EMPTY_GET_AUTOPILOT_RESPONSE,
} from "./schemas";
import { parseWithFallback } from "./schema";

const trigger = { id: "t", autopilot_id: "a", kind: "schedule", enabled: true };
describe("finite schedule response compatibility", () => {
  it("defaults older servers to Unlimited with no accepted occurrences", () => {
    expect(AutopilotTriggerSchema.parse(trigger)).toMatchObject({
      execution_limit: null,
      execution_count: 0,
      exhausted: false,
    });
  });
  it("preserves exhaustion and additive fields", () => {
    expect(
      AutopilotTriggerSchema.parse({
        ...trigger,
        execution_limit: 3,
        execution_count: 3,
        exhausted: true,
        future_field: "kept",
      }),
    ).toMatchObject({
      execution_limit: 3,
      execution_count: 3,
      exhausted: true,
      future_field: "kept",
    });
  });
  it.each([
    { execution_limit: 0 },
    { execution_limit: 1.5 },
    { execution_limit: 2147483648 },
    { execution_count: -1 },
    { exhausted: "true" },
  ])("degrades malformed count data safely: %j", (bad) => {
    expect(
      parseWithFallback({ ...trigger, ...bad }, AutopilotTriggerSchema, EMPTY_AUTOPILOT_TRIGGER, {
        endpoint: "trigger",
      }),
    ).toBe(EMPTY_AUTOPILOT_TRIGGER);
  });
});

describe("autopilot detail response", () => {
  const autopilot = {
    id: "a",
    workspace_id: "ws",
    title: "Work",
    assignee_id: "agent",
    status: "active",
    execution_mode: "run_only",
    created_by_type: "member",
    created_by_id: "user",
    created_at: "",
    updated_at: "",
  };
  it("parses older detail triggers and preserves collaborator/subscriber fields", () => {
    const detail = GetAutopilotResponseSchema.parse({
      autopilot: { ...autopilot, subscribers: [] },
      triggers: [trigger],
      collaborators: [],
    });
    expect(detail.triggers[0]).toMatchObject({ execution_limit: null, execution_count: 0 });
    expect(detail).toMatchObject({ collaborators: [], autopilot: { subscribers: [] } });
  });
  it("degrades malformed detail payloads", () => {
    for (const raw of [
      null,
      {},
      { autopilot, triggers: [{ ...trigger, execution_count: "two" }] },
    ]) {
      expect(
        parseWithFallback(raw, GetAutopilotResponseSchema, EMPTY_GET_AUTOPILOT_RESPONSE, {
          endpoint: "get autopilot",
        }),
      ).toBe(EMPTY_GET_AUTOPILOT_RESPONSE);
    }
  });
});
