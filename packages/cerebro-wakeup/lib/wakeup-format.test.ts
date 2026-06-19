import { describe, it, expect } from "vitest";
import {
  type Wakeup,
  formatCountdown,
  formatWaiting,
  wakeupLabel,
  wakeupCounter,
  sortWakeups,
} from "./wakeup-format";

const NOW = Date.UTC(2026, 5, 19, 8, 0, 0); // 2026-06-19T08:00:00Z

function mk(partial: Partial<Wakeup>): Wakeup {
  return {
    id: partial.id ?? "w1",
    trigger_type: partial.trigger_type ?? "time",
    fire_at: partial.fire_at,
    watch_status: partial.watch_status,
    prompt: partial.prompt ?? "",
    created_at: partial.created_at ?? new Date(NOW).toISOString(),
  };
}

describe("formatCountdown", () => {
  it("counts down minutes WITH seconds so it ticks visibly", () => {
    expect(formatCountdown(new Date(NOW + 90_000).toISOString(), NOW)).toBe("om 1m 30s");
    expect(formatCountdown(new Date(NOW + 36 * 60_000 + 12_000).toISOString(), NOW)).toBe("om 36m 12s");
    expect(formatCountdown(new Date(NOW + 45_000).toISOString(), NOW)).toBe("om 45s");
  });
  it("counts down hours and days (top two units)", () => {
    expect(formatCountdown(new Date(NOW + 2 * 3600_000 + 14 * 60_000).toISOString(), NOW)).toBe("om 2t 14m");
    expect(formatCountdown(new Date(NOW + 26 * 3600_000).toISOString(), NOW)).toBe("om 1d 2t");
  });
  it("returns 'når som helst' for past / invalid fire times", () => {
    expect(formatCountdown(new Date(NOW - 1000).toISOString(), NOW)).toBe("når som helst");
    expect(formatCountdown("not-a-date", NOW)).toBe("når som helst");
  });
});

describe("formatWaiting", () => {
  it("reports elapsed waiting time", () => {
    expect(formatWaiting(new Date(NOW - 3 * 60_000).toISOString(), NOW)).toBe("i 3m");
    expect(formatWaiting(new Date(NOW + 5000).toISOString(), NOW)).toBe("i 0s"); // never negative
  });
});

describe("wakeupLabel", () => {
  it("labels each trigger type", () => {
    expect(wakeupLabel(mk({ trigger_type: "time" }))).toBe("Planlagt kørsel");
    expect(wakeupLabel(mk({ trigger_type: "issue_status", watch_status: "done" }))).toBe("Venter på status: done");
    expect(wakeupLabel(mk({ trigger_type: "github_ci" }))).toBe("Venter på CI");
  });
});

describe("wakeupCounter", () => {
  it("uses countdown for time triggers and waiting for condition triggers", () => {
    expect(wakeupCounter(mk({ trigger_type: "time", fire_at: new Date(NOW + 60_000).toISOString() }), NOW)).toBe("om 1m 0s");
    expect(wakeupCounter(mk({ trigger_type: "github_ci", created_at: new Date(NOW - 120_000).toISOString() }), NOW)).toBe("i 2m");
  });
});

describe("sortWakeups", () => {
  it("orders concrete time triggers soonest-first, conditions last", () => {
    const soon = mk({ id: "soon", trigger_type: "time", fire_at: new Date(NOW + 60_000).toISOString() });
    const later = mk({ id: "later", trigger_type: "time", fire_at: new Date(NOW + 3600_000).toISOString() });
    const cond = mk({ id: "cond", trigger_type: "github_ci", fire_at: undefined });
    expect(sortWakeups([cond, later, soon]).map((w) => w.id)).toEqual(["soon", "later", "cond"]);
  });
});
