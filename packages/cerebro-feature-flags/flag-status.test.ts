import { describe, it, expect } from "vitest";
import {
  flagEffectiveState,
  flagStatusCopy,
  matchesFilter,
  matchesQuery,
} from "./flag-status";

describe("flagEffectiveState", () => {
  it("reports forced_on when locked on", () => {
    expect(
      flagEffectiveState({ enabled: false, locked: true, workspaceValue: true }),
    ).toBe("forced_on");
  });

  it("reports forced_off when locked off (the kill switch wins over personal on)", () => {
    expect(
      flagEffectiveState({ enabled: true, locked: true, workspaceValue: false }),
    ).toBe("forced_off");
  });

  it("falls back to the resolved personal value when not locked", () => {
    expect(
      flagEffectiveState({ enabled: true, locked: false, workspaceValue: undefined }),
    ).toBe("on");
    expect(
      flagEffectiveState({ enabled: false, locked: false, workspaceValue: true }),
    ).toBe("off");
  });
});

describe("flagStatusCopy", () => {
  it("uses the forced tone only for a locked-on flag", () => {
    expect(flagStatusCopy("forced_on").tone).toBe("forced");
    expect(flagStatusCopy("forced_off").tone).toBe("off");
    expect(flagStatusCopy("on").tone).toBe("on");
    expect(flagStatusCopy("off").tone).toBe("off");
  });

  it("gives every state a plain-language line", () => {
    for (const s of ["on", "off", "forced_on", "forced_off"] as const) {
      expect(flagStatusCopy(s).text.length).toBeGreaterThan(0);
    }
  });
});

describe("matchesFilter", () => {
  it("'on' includes forced_on, 'off' includes forced_off", () => {
    expect(matchesFilter("forced_on", "on")).toBe(true);
    expect(matchesFilter("forced_off", "off")).toBe(true);
    expect(matchesFilter("on", "off")).toBe(false);
  });

  it("'forced' matches either locked direction", () => {
    expect(matchesFilter("forced_on", "forced")).toBe(true);
    expect(matchesFilter("forced_off", "forced")).toBe(true);
    expect(matchesFilter("on", "forced")).toBe(false);
  });

  it("'all' matches everything", () => {
    expect(matchesFilter("off", "all")).toBe(true);
  });
});

describe("matchesQuery", () => {
  const flag = { label: "Web Push notifications", description: "Browser/PWA push" };

  it("matches on label, case-insensitively", () => {
    expect(matchesQuery(flag, "PUSH")).toBe(true);
  });

  it("matches on description", () => {
    expect(matchesQuery(flag, "pwa")).toBe(true);
  });

  it("empty query matches", () => {
    expect(matchesQuery(flag, "   ")).toBe(true);
  });

  it("no match returns false", () => {
    expect(matchesQuery(flag, "voice")).toBe(false);
  });
});
