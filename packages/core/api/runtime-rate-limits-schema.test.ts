import { describe, expect, it } from "vitest";
import { RuntimeRateLimitsResponseSchema } from "./schemas";

describe("RuntimeRateLimitsResponseSchema", () => {
  it("passes a null snapshot through", () => {
    expect(RuntimeRateLimitsResponseSchema.parse({ rate_limits: null })).toEqual({ rate_limits: null });
  });

  it("degrades unknown values and drops malformed windows", () => {
    const parsed = RuntimeRateLimitsResponseSchema.parse({
      rate_limits: {
        plan_type: "pro",
        status: "throttled_by_future_server",
        windows: [
          { kind: "session", used_percent: 72, window_minutes: 300, resets_at: "2026-11-16T06:00:00Z", status: "warning" },
          { kind: "monthly", used_percent: "lots", window_minutes: 43200 },
          "not a window",
        ],
        credits: { has_credits: true, unlimited: false, balance: "240" },
        observed_at: "2026-11-16T03:46:00Z",
      },
    });
    const rl = parsed.rate_limits;
    expect(rl?.status).toBe("");
    expect(rl?.windows).toHaveLength(2);
    expect(rl?.windows[0]).toMatchObject({ kind: "session", used_percent: 72, status: "warning" });
    expect(rl?.windows[1]).toMatchObject({ kind: "other", used_percent: undefined, window_minutes: 43200, status: "" });
    expect(rl?.credits?.balance).toBe("240");
  });

  it("falls back to null when the snapshot itself is unusable", () => {
    expect(RuntimeRateLimitsResponseSchema.parse({ rate_limits: { windows: [] } })).toEqual({ rate_limits: null });
    expect(RuntimeRateLimitsResponseSchema.parse({ rate_limits: "nope" })).toEqual({ rate_limits: null });
  });
});
