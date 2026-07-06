import { describe, expect, it } from "vitest";
import { ChainProvider } from "./chain-provider";
import { StaticProvider } from "./static-provider";
import { FeatureFlagService } from "./service";

describe("FeatureFlagService", () => {
  it("returns the default when no provider is configured", () => {
    const s = new FeatureFlagService(null);
    expect(s.isEnabled("any", {}, true)).toBe(true);
    expect(s.isEnabled("any", {}, false)).toBe(false);
    expect(s.variant("any", {}, "control")).toBe("control");
    expect(s.decision("any", {}, false).reason).toBe("default");
  });

  it("returns the default when the provider does not know the key", () => {
    const s = new FeatureFlagService(new StaticProvider({}));
    expect(s.isEnabled("missing", {}, true)).toBe(true);
    expect(s.decision("missing", {}, true).reason).toBe("default");
  });

  it("uses the provider decision when found", () => {
    const sp = new StaticProvider({ billing: { default: true } });
    const s = new FeatureFlagService(sp);
    const d = s.decision("billing", {}, false);
    expect(d.enabled).toBe(true);
    expect(d.reason).toBe("static");
    expect(d.source).toBe("static");
  });

  it("echoes the requested key in the decision", () => {
    const sp = new StaticProvider({ a: { default: true } });
    const s = new FeatureFlagService(sp);
    expect(s.decision("a", {}, false).key).toBe("a");
  });

  it("setProvider swaps the underlying provider", () => {
    const s = new FeatureFlagService(null);
    expect(s.isEnabled("k", {}, false)).toBe(false);
    s.setProvider(new StaticProvider({ k: { default: true } }));
    expect(s.isEnabled("k", {}, false)).toBe(true);
  });
});

describe("ChainProvider", () => {
  it("first match wins", () => {
    const top = new StaticProvider({ shared: { default: true } });
    const bottom = new StaticProvider({ shared: { default: false } });
    const chain = new ChainProvider([top, bottom]);
    expect(chain.lookup("shared", {})?.enabled).toBe(true);
  });

  it("falls through to the next provider", () => {
    const top = new StaticProvider({});
    const bottom = new StaticProvider({ only_in_bottom: { default: true } });
    const chain = new ChainProvider([top, bottom]);
    expect(chain.lookup("only_in_bottom", {})?.enabled).toBe(true);
  });

  it("returns undefined when no provider matches", () => {
    const chain = new ChainProvider([new StaticProvider({})]);
    expect(chain.lookup("nope", {})).toBeUndefined();
  });

  it("skips null and undefined entries", () => {
    const sp = new StaticProvider({ real: { default: true } });
    const chain = new ChainProvider([null, sp, undefined]);
    expect(chain.lookup("real", {})?.enabled).toBe(true);
  });
});
