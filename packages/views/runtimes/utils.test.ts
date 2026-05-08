import { describe, expect, it } from "vitest";
import type {
  RuntimeUsage,
  RuntimeUsageByAgent,
  RuntimeUsageByHour,
} from "@multica/core/types";
import {
  aggregateByDate,
  aggregateCostByAgent,
  aggregateCostByHour,
  aggregateCostByModel,
  countInputOutputTokens,
  isVersionNewer,
} from "./utils";

describe("runtime CLI version comparison", () => {
  it("uses commit counts when semantic versions match", () => {
    expect(isVersionNewer("v0.2.11-124-bbbb", "v0.2.11-123-aaaa")).toBe(true);
    expect(isVersionNewer("v0.2.11-123-bbbb", "v0.2.11-124-aaaa")).toBe(false);
  });
});

describe("runtime token aggregation", () => {
  it("counts only input and output tokens in displayed totals", () => {
    expect(
      countInputOutputTokens({
        input_tokens: 1_200,
        output_tokens: 300,
      }),
    ).toBe(1_500);
  });

  it("keeps cache columns separate from model token totals", () => {
    const usage: RuntimeUsage[] = [
      {
        runtime_id: "runtime-1",
        date: "2026-05-07",
        provider: "claude",
        model: "claude-sonnet-4-5",
        input_tokens: 1_200,
        output_tokens: 300,
        cache_read_tokens: 7_000,
        cache_write_tokens: 4_000,
      },
      {
        runtime_id: "runtime-1",
        date: "2026-05-07",
        provider: "claude",
        model: "claude-sonnet-4-5",
        input_tokens: 800,
        output_tokens: 200,
        cache_read_tokens: 1_000,
        cache_write_tokens: 500,
      },
    ];

    const { dailyTokens, modelDist } = aggregateByDate(usage);

    expect(dailyTokens).toEqual([
      {
        date: "2026-05-07",
        label: "5/7",
        input: 2_000,
        output: 500,
        cacheRead: 8_000,
        cacheWrite: 4_500,
      },
    ]);
    expect(modelDist).toEqual([
      expect.objectContaining({
        model: "claude-sonnet-4-5",
        tokens: 2_500,
      }),
    ]);
  });

  it("excludes cache tokens from cost-by token columns", () => {
    const byModel = aggregateCostByModel([
      {
        runtime_id: "runtime-1",
        date: "2026-05-07",
        provider: "claude",
        model: "claude-sonnet-4-5",
        input_tokens: 1_200,
        output_tokens: 300,
        cache_read_tokens: 7_000,
        cache_write_tokens: 4_000,
      },
    ] satisfies RuntimeUsage[]);
    const byAgent = aggregateCostByAgent([
      {
        agent_id: "agent-1",
        model: "claude-sonnet-4-5",
        input_tokens: 1_200,
        output_tokens: 300,
        cache_read_tokens: 7_000,
        cache_write_tokens: 4_000,
        task_count: 3,
      },
    ] satisfies RuntimeUsageByAgent[]);
    const byHour = aggregateCostByHour([
      {
        hour: 9,
        model: "claude-sonnet-4-5",
        input_tokens: 1_200,
        output_tokens: 300,
        cache_read_tokens: 7_000,
        cache_write_tokens: 4_000,
        task_count: 3,
      },
    ] satisfies RuntimeUsageByHour[]);

    expect(byModel[0]).toEqual(expect.objectContaining({ key: "claude-sonnet-4-5", tokens: 1_500 }));
    expect(byAgent[0]).toEqual(expect.objectContaining({ key: "agent-1", tokens: 1_500 }));
    expect(byHour.find((row) => row.key === "9")).toEqual(
      expect.objectContaining({ key: "9", tokens: 1_500 }),
    );
  });
});
