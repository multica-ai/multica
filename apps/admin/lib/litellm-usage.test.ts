import { describe, expect, it } from "vitest";
import { aggregateTeamUsage } from "./litellm-usage";
import type { LiteLlmDayResult } from "./litellm-schema";

describe("aggregateTeamUsage", () => {
  it("reads spend/tokens nested under metrics", () => {
    const days: LiteLlmDayResult[] = [
      {
        date: "2026-08-20",
        breakdown: {
          models: {
            "gpt-4o": { metrics: { spend: 1.5, total_tokens: 100 } },
          },
        },
      },
    ];
    expect(aggregateTeamUsage(days, "2026-08-20")).toEqual({
      cost24h: 1.5,
      cost30d: 1.5,
      tokens24h: 100,
    });
  });

  it("falls back to top-level spend/tokens when metrics is absent", () => {
    const days: LiteLlmDayResult[] = [
      {
        date: "2026-08-19",
        breakdown: {
          models: {
            "gpt-4o": { spend: 2, total_tokens: 50 },
          },
        },
      },
    ];
    expect(aggregateTeamUsage(days, "2026-08-20")).toEqual({
      cost24h: 0,
      cost30d: 2,
      tokens24h: 0,
    });
  });

  it("returns null when no day has any spend or tokens", () => {
    const days: LiteLlmDayResult[] = [
      { date: "2026-08-20", breakdown: { models: { "gpt-4o": { metrics: {} } } } },
    ];
    expect(aggregateTeamUsage(days, "2026-08-20")).toBeNull();
  });

  it("sums 30d spend across days and only counts today's spend/tokens for the 24h window", () => {
    const days: LiteLlmDayResult[] = [
      { date: "2026-08-19", breakdown: { models: { m1: { metrics: { spend: 3, total_tokens: 30 } } } } },
      { date: "2026-08-20", breakdown: { models: { m1: { metrics: { spend: 4, total_tokens: 40 } } } } },
    ];
    expect(aggregateTeamUsage(days, "2026-08-20")).toEqual({
      cost24h: 4,
      cost30d: 7,
      tokens24h: 40,
    });
  });
});
