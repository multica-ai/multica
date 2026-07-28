import { beforeEach, describe, expect, it, vi } from "vitest";

const { cerebroRequest } = vi.hoisted(() => ({
  cerebroRequest: vi.fn(),
}));

vi.mock("@multica/core/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/api")>()),
  api: { cerebroRequest },
}));

import {
  fetchAIImpactFunctions,
  fetchAIImpactOverview,
  fetchAIImpactPeople,
  fetchAIImpactQualityRisk,
} from "./index";

describe("AI Impact dashboard client", () => {
  beforeEach(() => {
    cerebroRequest.mockReset();
  });

  it("validates the three dashboard read models and falls back safely on malformed responses", async () => {
    cerebroRequest.mockImplementation(async (path: string) => {
      if (path.endsWith("/overview/summary")) {
        return {
          families: [{ family: "Outcome", evidence: [] }],
        };
      }
      if (path.endsWith("/functions/summary")) {
        return {
          functions: [
            {
              id: "function-1",
              name: "Customer Service",
              operating_loops: [
                { id: "loop-1", name: "Resolve customer needs", decision: "Scale" },
              ],
            },
          ],
        };
      }
      return {
        decisions: [
          {
            function_id: "function-1",
            function_name: "Customer Service",
            operating_loop_id: "loop-1",
            operating_loop_name: "Resolve customer needs",
            decision: "Scale",
          },
        ],
      };
    });

    await expect(fetchAIImpactOverview()).resolves.toEqual({
      families: [{ family: "Outcome", evidence: [] }],
    });
    await expect(fetchAIImpactFunctions()).resolves.toEqual({
      functions: [
        {
          id: "function-1",
          name: "Customer Service",
          operating_loops: [
            { id: "loop-1", name: "Resolve customer needs", decision: "Scale" },
          ],
        },
      ],
    });
    await expect(fetchAIImpactQualityRisk()).resolves.toEqual({
      decisions: [
        {
          function_id: "function-1",
          function_name: "Customer Service",
          operating_loop_id: "loop-1",
          operating_loop_name: "Resolve customer needs",
          decision: "Scale",
        },
      ],
    });
    expect(cerebroRequest.mock.calls.map(([path]) => path)).toEqual([
      "/api/cerebro/ai-impact/overview/summary",
      "/api/cerebro/ai-impact/functions/summary",
      "/api/cerebro/ai-impact/quality-risk/decisions",
    ]);

    cerebroRequest.mockResolvedValue({ families: null, functions: null, decisions: null });
    await expect(fetchAIImpactOverview()).resolves.toEqual({ families: [] });
    await expect(fetchAIImpactFunctions()).resolves.toEqual({ functions: [] });
    await expect(fetchAIImpactQualityRisk()).resolves.toEqual({ decisions: [] });
  });

  it("loads privacy-protected People results for the selected activity period", async () => {
    cerebroRequest.mockResolvedValue({
      period: "month",
      people: [
        {
          id: "member-1",
          type: "member",
          name: "Maya",
          activity: [{ bucket: "2026-07", count: 4 }],
          usage: { runs: 2, issues: 3, projects: 1, chats: 5, channels: 2 },
          outcomes: {
            needs_solved: { solved: 8, measurable: 10 },
            solution_quality: 0.91,
            frustration_free: 0.84,
            prompt_effectiveness: 0.79,
            skill_activity: 3,
            cost_cents: 42,
          },
          confidence: 0.76,
          sample_size: 12,
        },
      ],
    });

    await expect(fetchAIImpactPeople("month")).resolves.toMatchObject({
      period: "month",
      people: [{ id: "member-1", name: "Maya" }],
    });
    expect(cerebroRequest).toHaveBeenCalledWith(
      "/api/cerebro/ai-impact/people?period=month",
    );

    cerebroRequest.mockResolvedValue({ period: "month", people: null });
    await expect(fetchAIImpactPeople("month")).resolves.toEqual({
      period: "month",
      people: [],
    });
  });
});
