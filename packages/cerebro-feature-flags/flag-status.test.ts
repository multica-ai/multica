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

// FIR-4643: cerebro_workflow_hooks is enforced from the workspace row only.
// Reporting the personal value for such a flag is what let an owner believe a
// switch had taken effect when the server never read it.
describe("flagEffectiveState — workspace-only flags", () => {
  it("ignores the personal value and reports the registry default", () => {
    expect(
      flagEffectiveState({
        enabled: false,
        locked: false,
        workspaceValue: undefined,
        workspaceOnly: true,
        defaultValue: true,
      }),
    ).toBe("workspace_on");
  });

  it("reports the workspace override when one exists", () => {
    expect(
      flagEffectiveState({
        enabled: true,
        locked: false,
        workspaceValue: false,
        workspaceOnly: true,
        defaultValue: true,
      }),
    ).toBe("workspace_off");
  });

  it("still reports a locked workspace value as forced", () => {
    expect(
      flagEffectiveState({
        enabled: true,
        locked: true,
        workspaceValue: false,
        workspaceOnly: true,
        defaultValue: true,
      }),
    ).toBe("forced_off");
  });

  it("names the scope in the status line", () => {
    expect(flagStatusCopy("workspace_on").text).toBe("On for the whole workspace");
    expect(flagStatusCopy("workspace_off").text).toBe("Off for the whole workspace");
  });

  it("stays reachable through the on/off filter chips", () => {
    expect(matchesFilter("workspace_on", "on")).toBe(true);
    expect(matchesFilter("workspace_off", "off")).toBe(true);
    expect(matchesFilter("workspace_on", "off")).toBe(false);
  });
});
