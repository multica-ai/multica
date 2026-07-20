import { describe, expect, it } from "vitest";
import { StaticProvider } from "./static-provider";

describe("StaticProvider", () => {
  it("returns undefined for unknown keys so callers fall through", () => {
    const sp = new StaticProvider();
    expect(sp.lookup("missing", {})).toBeUndefined();
  });

  it("returns the rule default for known keys", () => {
    const sp = new StaticProvider({ on: { default: true }, off: { default: false } });
    expect(sp.lookup("on", {})?.enabled).toBe(true);
    expect(sp.lookup("off", {})?.enabled).toBe(false);
  });

  it("allow forces ON for matching users", () => {
    const sp = new StaticProvider({
      internal_dashboard: { default: false, allow: ["user-internal"] },
    });
    expect(sp.lookup("internal_dashboard", { userId: "user-internal" })?.enabled).toBe(true);
    expect(sp.lookup("internal_dashboard", { userId: "user-random" })?.enabled).toBe(false);
  });

  it("deny wins over allow for the same user", () => {
    const sp = new StaticProvider({
      conflict: { default: true, allow: ["same"], deny: ["same"] },
    });
    expect(sp.lookup("conflict", { userId: "same" })?.enabled).toBe(false);
  });

  it("percent rollout is deterministic for a fixed user", () => {
    const sp = new StaticProvider({ split: { percent: { percent: 50 } } });
    const first = sp.lookup("split", { userId: "stable" })?.enabled;
    for (let i = 0; i < 100; i++) {
      expect(sp.lookup("split", { userId: "stable" })?.enabled).toBe(first);
    }
  });

  it("percent rollout with by=workspace_id buckets by workspace", () => {
    const sp = new StaticProvider({
      ws_rollout: { percent: { percent: 100, by: "workspace_id" } },
    });
    const decision = sp.lookup("ws_rollout", { workspaceId: "w-1" });
    expect(decision?.enabled).toBe(true);
    expect(decision?.reason).toBe("percent");
  });

  it("variant overrides the boolean variant string", () => {
    const sp = new StaticProvider({
      checkout: { default: true, variant: "experiment-v2" },
    });
    const d = sp.lookup("checkout", { userId: "anyone" });
    expect(d?.variant).toBe("experiment-v2");
    expect(d?.enabled).toBe(true);
  });

  // Regression test for the MUL-3615 review: when a rule sets `variant`
  // but the rule itself evaluates to enabled=false (deny match, percent
  // miss, default-off), the decision MUST report variant="off", never
  // the on-variant. Otherwise a switch on `useVariant()` would route
  // non-rolled-in users into the experiment arm.
  it("variant: returns 'off' when the rule evaluates to disabled", () => {
    const sp = new StaticProvider({
      exp: {
        default: false,
        variant: "experiment-v2",
        deny: ["banned-user"],
        percent: { percent: 0 },
      },
    });
    for (const userId of ["banned-user", "random-user", ""]) {
      const d = sp.lookup("exp", { userId });
      expect(d?.enabled).toBe(false);
      expect(d?.variant).toBe("off");
    }
  });

  it("variant: returns the on-variant when the rule evaluates to enabled", () => {
    const sp = new StaticProvider({
      exp: { default: false, variant: "experiment-v2", allow: ["rolled-in"] },
    });
    const d = sp.lookup("exp", { userId: "rolled-in" });
    expect(d?.enabled).toBe(true);
    expect(d?.variant).toBe("experiment-v2");
  });

  it("loadRules replaces, not merges, the rule map", () => {
    const sp = new StaticProvider({ old: { default: true } });
    sp.loadRules({ fresh: { default: true } });
    expect(sp.lookup("old", {})).toBeUndefined();
    expect(sp.lookup("fresh", {})?.enabled).toBe(true);
  });

  it("custom attribute lookup against attributes map", () => {
    const sp = new StaticProvider({
      plan_gate: { default: false, allow: ["enterprise"], allowBy: "plan" },
    });
    expect(
      sp.lookup("plan_gate", { attributes: { plan: "enterprise" } })?.enabled,
    ).toBe(true);
    expect(sp.lookup("plan_gate", { attributes: { plan: "free" } })?.enabled).toBe(false);
  });

  it("keys returns a sorted snapshot", () => {
    const sp = new StaticProvider({ zeta: {}, alpha: {}, mu: {} });
    expect(sp.keys()).toEqual(["alpha", "mu", "zeta"]);
  });
});
