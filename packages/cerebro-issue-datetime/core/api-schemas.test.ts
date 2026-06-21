import { describe, expect, it } from "vitest";

import {
  issueDateTimesSchema,
  workspaceDateTimeSettingsSchema,
} from "./api-schemas";

// API Response Compatibility (CLAUDE.md): a malformed/older/newer backend
// response must degrade to safe defaults, never throw into the UI.
describe("issueDateTimesSchema", () => {
  it("parses a full valid response", () => {
    const out = issueDateTimesSchema.parse({
      start_time: "08:00",
      due_time: "16:30",
      agent_start_kind: "start",
    });
    expect(out).toEqual({
      start_time: "08:00",
      due_time: "16:30",
      agent_start_kind: "start",
    });
  });

  it("defaults missing fields (older backend with no agent_start_kind)", () => {
    const out = issueDateTimesSchema.parse({ start_time: null });
    expect(out.due_time).toBeNull();
    expect(out.agent_start_kind).toBeNull();
  });

  it("downgrades an unknown agent_start_kind enum value to null (enum drift)", () => {
    const out = issueDateTimesSchema.parse({
      start_time: null,
      due_time: null,
      agent_start_kind: "someday",
    });
    expect(out.agent_start_kind).toBeNull();
  });

  it("downgrades a wrong-typed agent_start_kind to null", () => {
    const out = issueDateTimesSchema.parse({ agent_start_kind: 42 });
    expect(out.agent_start_kind).toBeNull();
  });
});

describe("workspaceDateTimeSettingsSchema", () => {
  it("parses a valid default", () => {
    expect(
      workspaceDateTimeSettingsSchema.parse({ default_agent_start_kind: "due" }),
    ).toEqual({ default_agent_start_kind: "due" });
  });

  it("falls back to 'none' on a missing field", () => {
    expect(workspaceDateTimeSettingsSchema.parse({})).toEqual({
      default_agent_start_kind: "none",
    });
  });

  it("falls back to 'none' on an unknown enum value", () => {
    expect(
      workspaceDateTimeSettingsSchema.parse({ default_agent_start_kind: "later" }),
    ).toEqual({ default_agent_start_kind: "none" });
  });
});
