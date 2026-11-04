import { describe, expect, it } from "vitest";
import fixture from "../../../server/internal/metrics/testdata/cost_estimate_cases.json";
import { estimateCost } from "./utils";

// The shared fixture the server's EstimateCostUSD is tested against
// (server/internal/metrics/cost_estimate_test.go). Both sides must give the same
// figure for the same token counts, so a budget and the Analytics page agree.
interface Case {
  name: string;
  provider: string;
  model: string;
  input_tokens: number;
  output_tokens: number;
  cache_read_tokens: number;
  cache_write_tokens: number;
  expected_usd: number | null;
}

describe("cost estimate equals the server's estimate", () => {
  for (const c of fixture.cases as Case[]) {
    it(c.name, () => {
      const usage = {
        provider: c.provider,
        model: c.model,
        input_tokens: c.input_tokens,
        output_tokens: c.output_tokens,
        cache_read_tokens: c.cache_read_tokens,
        cache_write_tokens: c.cache_write_tokens,
      };
      const got = estimateCost(usage as Parameters<typeof estimateCost>[0]);
      if (c.expected_usd === null) {
        // No rate row: the Analytics estimate has nothing to add.
        expect(got).toBe(0);
        return;
      }
      expect(got).toBeCloseTo(c.expected_usd, 9);
    });
  }
});
