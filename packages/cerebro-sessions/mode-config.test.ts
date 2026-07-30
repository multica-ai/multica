import { describe, expect, it } from "vitest";
import { parseWithFallback } from "@multica/core/api";
import {
  EMPTY_MODE_CONFIG_LIST,
  modeConfigListSchema,
  modeRecordSchema,
} from "./mode-config";

describe("Mode configuration API schemas", () => {
  it("accepts a versioned published profile with a draft and history", () => {
    const result = modeRecordSchema.safeParse({
      mode: "plan",
      active_version: "2",
      active: {
        mode: "plan",
        version: "2",
        instruction: "Plan first.",
        model: "gpt-5.4",
        thinking_level: "high",
        timeout_minutes: 30,
        max_turns: 20,
        allows_write: false,
        allowed_tools: ["graphify"],
        data_sources: ["Company Brain"],
        approval_policy: "require",
        extra_skill_ids: ["skill-1"],
      },
      draft: null,
      versions: [{ id: "v2", version: "2", config: {}, description: "Safer Plan", created_at: "2026-07-15T08:00:00Z" }],
      updated_at: "2026-07-15T08:00:00Z",
    });
    expect(result.success).toBe(true);
  });

  it("falls back to an empty list on contract drift", () => {
    const result = parseWithFallback(
      { modes: "invalid" },
      modeConfigListSchema,
      EMPTY_MODE_CONFIG_LIST,
      { endpoint: "/api/cerebro/session-modes/" },
    );
    expect(result).toEqual({ modes: [] });
  });

  it("normalizes nullable list fields returned by the Go API", () => {
    const result = modeRecordSchema.safeParse({
      mode: "plan",
      active_version: "1",
      active: {
        mode: "plan",
        version: "1",
        instruction: "Plan first.",
        thinking_level: "medium",
        timeout_minutes: 30,
        max_turns: 20,
        allows_write: false,
        allowed_tools: null,
        data_sources: null,
        approval_policy: "inherit",
        extra_skill_ids: null,
      },
      versions: [],
      updated_at: "2026-07-15T08:00:00Z",
    });

    expect(result.success).toBe(true);
    if (!result.success) return;
    expect(result.data.active.allowed_tools).toEqual([]);
    expect(result.data.active.data_sources).toEqual([]);
    expect(result.data.active.extra_skill_ids).toEqual([]);
  });

  it("defaults the plan-write scope off when an older server omits it", () => {
    const result = modeRecordSchema.safeParse({
      mode: "review",
      active_version: "1",
      active: {
        mode: "review",
        version: "1",
        instruction: "Review only.",
        thinking_level: "high",
        timeout_minutes: 45,
        max_turns: 30,
        allows_write: false,
        approval_policy: "inherit",
      },
      versions: [],
      updated_at: "2026-07-15T08:00:00Z",
    });

    expect(result.success).toBe(true);
    if (!result.success) return;
    expect(result.data.active.allows_plan_write).toBe(false);
  });
});
