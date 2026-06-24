import { beforeEach, describe, expect, it, vi } from "vitest";

const mockCerebroRequest = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/api", async () => {
  const actual =
    await vi.importActual<typeof import("@multica/core/api")>("@multica/core/api");
  return {
    ...actual,
    api: { cerebroRequest: mockCerebroRequest },
  };
});

import { getContextUsage } from "./api";

beforeEach(() => {
  mockCerebroRequest.mockReset();
});

// Enforces the API Response Compatibility rule: a malformed/partial backend
// response must downgrade to a safe fallback, never throw NaN into the hairline.
describe("context-usage api compatibility", () => {
  it("downgrades garbage to a no-data fallback", async () => {
    mockCerebroRequest.mockResolvedValueOnce({ unexpected: true });
    const res = await getContextUsage("issue-1");
    expect(res.has_data).toBe(false);
    expect(res.used_percent).toBe(0);
    expect(res.max_context_tokens).toBe(0);
  });

  it("fills defaults for missing fields", async () => {
    mockCerebroRequest.mockResolvedValueOnce({ has_data: true, used_percent: 62 });
    const res = await getContextUsage("issue-1");
    expect(res.has_data).toBe(true);
    expect(res.used_percent).toBe(62);
    expect(res.input_tokens).toBe(0); // missing → defaulted, not undefined
    expect(res.cache_share_percent).toBe(0);
  });

  it("passes through a well-formed cross-model response", async () => {
    mockCerebroRequest.mockResolvedValueOnce({
      session_id: "s1",
      has_data: true,
      model: "claude-opus-4-8",
      input_tokens: 24000,
      cache_read_tokens: 100000,
      cache_write_tokens: 0,
      output_tokens: 2000,
      context_tokens: 124000,
      max_context_tokens: 200000,
      used_percent: 62,
      cache_share_percent: 80,
    });
    const res = await getContextUsage("issue-1");
    expect(res.model).toBe("claude-opus-4-8");
    expect(res.used_percent).toBe(62);
    expect(res.cache_share_percent).toBe(80);
    expect(res.context_tokens).toBe(124000);
    expect(res.max_context_tokens).toBe(200000);
  });

  // FIR-1931 Fix C: the bar reads `approximate` to decide whether to prefix "~".
  it("passes the approximate flag through and defaults it to false", async () => {
    mockCerebroRequest.mockResolvedValueOnce({
      has_data: true,
      used_percent: 100,
      context_tokens: 1000000,
      max_context_tokens: 1000000,
      approximate: true,
    });
    const res = await getContextUsage("issue-1");
    expect(res.approximate).toBe(true);

    mockCerebroRequest.mockResolvedValueOnce({ has_data: true, used_percent: 40 });
    const res2 = await getContextUsage("issue-1");
    expect(res2.approximate).toBe(false); // missing → defaulted, not undefined
  });
});
