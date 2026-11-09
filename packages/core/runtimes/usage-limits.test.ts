// @vitest-environment node
import { describe, expect, it } from "vitest";
import { EMPTY_RUNTIME } from "./schema";
import type { AgentRuntime } from "../types";
import {
  USAGE_LIMIT_STALE_AFTER_MS,
  groupUsageLimitAccounts,
  isUsageLimitWarning,
  readRuntimeUsageLimits,
  usageLimitBarWindows,
  usageLimitPace,
  type UsageLimitWindow,
} from "./usage-limits";

const NOW = Date.parse("2026-11-09T12:00:00Z");
const minutesAgo = (minutes: number) => new Date(NOW - minutes * 60_000).toISOString();

function runtime(id: string, overrides: Partial<AgentRuntime>, usageLimits?: unknown): AgentRuntime {
  return {
    ...EMPTY_RUNTIME,
    id,
    workspace_id: "ws",
    status: "online",
    metadata: usageLimits === undefined ? {} : { usage_limits: usageLimits },
    ...overrides,
  };
}

function snapshot(overrides: Record<string, unknown> = {}) {
  return {
    provider: "claude",
    account_id: "acct-a",
    plan: "max",
    device: "pc-home",
    windows: [
      { id: "seven_day", window_minutes: 10080, used_percent: 30, resets_at: "2026-11-14T00:00:00Z" },
      { id: "five_hour", window_minutes: 300, used_percent: 85, resets_at: "2026-11-09T14:00:00Z" },
    ],
    updated_at: minutesAgo(5),
    checked_at: minutesAgo(5),
    source: "probe",
    ...overrides,
  };
}

describe("readRuntimeUsageLimits", () => {
  it("returns null for runtimes without a report", () => {
    expect(readRuntimeUsageLimits(runtime("r", {}))).toBeNull();
  });

  it("returns null when the report has no provider", () => {
    expect(readRuntimeUsageLimits(runtime("r", {}, { windows: [] }))).toBeNull();
    expect(readRuntimeUsageLimits(runtime("r", {}, "broken"))).toBeNull();
  });

  it("drops malformed windows and keeps the valid ones", () => {
    const parsed = readRuntimeUsageLimits(
      runtime("r", {}, snapshot({
        windows: [
          { id: "five_hour", window_minutes: 300, used_percent: 12, resets_at: "not a date" },
          { window_minutes: 10080, used_percent: 5 },
          "garbage",
        ],
      })),
    );
    expect(parsed?.windows).toEqual([
      { id: "five_hour", label: "", window_minutes: 300, used_percent: 12, resets_at: null },
    ]);
  });

  it("defaults malformed fields instead of dropping the report", () => {
    const parsed = readRuntimeUsageLimits(
      runtime("r", {}, { provider: "codex", windows: "nope", updated_at: 5, error: null }),
    );
    expect(parsed).toMatchObject({ provider: "codex", windows: [], updated_at: null, error: "" });
  });
});

describe("groupUsageLimitAccounts", () => {
  it("shows one entry per login, tightest window first", () => {
    const accounts = groupUsageLimitAccounts(
      [
        runtime("codex-home", { provider: "codex", daemon_id: "home" }, snapshot({ provider: "codex", account_id: "codex-1", device: "pc-home", updated_at: minutesAgo(10) })),
        runtime("codex-old", { provider: "codex", daemon_id: "old" }, snapshot({ provider: "codex", account_id: "codex-1", device: "pc-old", updated_at: minutesAgo(2) })),
        runtime("claude-old", { provider: "claude", daemon_id: "old" }, snapshot({ account_id: "acct-b", device: "pc-old" })),
        runtime("claude-home", { provider: "claude", daemon_id: "home" }, snapshot()),
        runtime("claude-home-ws2", { provider: "claude", daemon_id: "home", workspace_id: "ws2" }, snapshot()),
        runtime("gemini", { provider: "gemini" }),
      ],
      NOW,
    );

    expect(accounts.map((account) => [account.provider, account.devices])).toEqual([
      ["claude", ["pc-home"]],
      ["claude", ["pc-old"]],
      ["codex", ["pc-home", "pc-old"]],
    ]);
    expect(accounts[0]!.windows.map((window) => window.id)).toEqual(["five_hour", "seven_day"]);
    expect(accounts[0]!.refreshRuntimeIds).toEqual(["claude-home"]);
    expect(accounts[2]!.updatedAt).toBe(minutesAgo(2));
    expect(accounts[2]!.refreshRuntimeIds).toEqual(["codex-home", "codex-old"]);
  });

  it("keeps a login fresh when another machine checked it recently", () => {
    const [account] = groupUsageLimitAccounts(
      [
        runtime("a", { provider: "codex", daemon_id: "home" }, snapshot({ provider: "codex", account_id: "c", updated_at: null, error: "not logged in" })),
        runtime("b", { provider: "codex", daemon_id: "old" }, snapshot({ provider: "codex", account_id: "c" })),
      ],
      NOW,
    );
    expect(account).toMatchObject({ stale: false, error: "" });
  });

  it("marks a login stale when its last check failed, keeping the last numbers", () => {
    const [account] = groupUsageLimitAccounts(
      [runtime("a", { provider: "claude" }, snapshot({ error: "get_usage timed out", checked_at: minutesAgo(1) }))],
      NOW,
    );
    expect(account).toMatchObject({ stale: true, error: "get_usage timed out" });
    expect(account!.windows[0]!.used_percent).toBe(85);
  });

  it("marks a login stale when no check arrived for too long", () => {
    const old = new Date(NOW - USAGE_LIMIT_STALE_AFTER_MS - 1000).toISOString();
    const [account] = groupUsageLimitAccounts([runtime("a", { provider: "claude" }, snapshot({ updated_at: old }))], NOW);
    expect(account!.stale).toBe(true);
  });

  it("treats a window past its reset time as started over", () => {
    const [account] = groupUsageLimitAccounts(
      [
        runtime("a", { provider: "claude" }, snapshot({
          windows: [{ id: "five_hour", window_minutes: 300, used_percent: 95, resets_at: minutesAgo(1) }],
        })),
      ],
      NOW,
    );
    expect(account!.windows[0]).toMatchObject({ used_percent: 0, resets_at: null });
  });

  it("ignores a report whose provider does not match the runtime", () => {
    expect(groupUsageLimitAccounts([runtime("a", { provider: "codex" }, snapshot())], NOW)).toEqual([]);
  });

  it("does not offer a refresh through offline runtimes", () => {
    const [account] = groupUsageLimitAccounts([runtime("a", { provider: "claude", status: "offline" }, snapshot())], NOW);
    expect(account!.refreshRuntimeIds).toEqual([]);
  });
});

describe("isUsageLimitWarning", () => {
  it("warns from 80% used", () => {
    expect(isUsageLimitWarning({ used_percent: 79.9 })).toBe(false);
    expect(isUsageLimitWarning({ used_percent: 80 })).toBe(true);
  });
});

describe("usageLimitPace", () => {
  const minutesFromNow = (minutes: number) => new Date(NOW + minutes * 60_000).toISOString();
  const fiveHour = (used: number, resetsInMinutes: number): UsageLimitWindow => ({
    id: "five_hour",
    label: "",
    window_minutes: 300,
    used_percent: used,
    resets_at: minutesFromNow(resetsInMinutes),
  });

  it("forecasts when a window runs out before its reset", () => {
    const pace = usageLimitPace(fiveHour(86, 150), NOW);
    expect(pace!.elapsedPercent).toBe(50);
    expect((pace!.runsOutAt! - NOW) / 60_000).toBeCloseTo(24.4, 1);
  });

  it("gives no forecast when the window lasts until its reset", () => {
    expect(usageLimitPace(fiveHour(40, 150), NOW)).toEqual({ elapsedPercent: 50, runsOutAt: null });
  });

  it("gives no forecast early in a window, for an unused window, or at the limit", () => {
    expect(usageLimitPace(fiveHour(20, 290), NOW)!.runsOutAt).toBeNull();
    expect(usageLimitPace(fiveHour(0, 150), NOW)!.runsOutAt).toBeNull();
    expect(usageLimitPace(fiveHour(100, 150), NOW)!.runsOutAt).toBeNull();
  });

  it("has no pace without a reset time, a length, or with a reset beyond the window", () => {
    expect(usageLimitPace({ ...fiveHour(50, 60), resets_at: null }, NOW)).toBeNull();
    expect(usageLimitPace({ ...fiveHour(50, 60), window_minutes: 0 }, NOW)).toBeNull();
    expect(usageLimitPace(fiveHour(50, 400), NOW)).toBeNull();
  });
});

describe("usageLimitBarWindows", () => {
  const window = (id: string, minutes: number, used: number): UsageLimitWindow => ({
    id,
    label: "",
    window_minutes: minutes,
    used_percent: used,
    resets_at: null,
  });

  it("keeps the shortest window and the tightest of the rest, shortest first", () => {
    const windows = [window("seven_day", 10080, 41), window("opus", 10080, 90), window("five_hour", 300, 20)];
    expect(usageLimitBarWindows(windows).map((item) => item.id)).toEqual(["five_hour", "opus"]);
  });

  it("orders one or two windows by length", () => {
    expect(usageLimitBarWindows([window("seven_day", 10080, 63), window("five_hour", 300, 48)]).map((item) => item.id)).toEqual([
      "five_hour",
      "seven_day",
    ]);
  });
});
