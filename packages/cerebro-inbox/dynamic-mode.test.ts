import { describe, expect, it } from "vitest";
import { DEFAULT_INBOX_MODE, toInboxMode } from "./dynamic-mode";

// TECH-3413 (Jesper) — the dynamic inbox is the default surface. These lock in
// that an unset/garbage preference resolves to dynamic, while an EXPLICIT
// classic choice is still honoured so "Switch to classic" keeps sticking.
describe("toInboxMode", () => {
  it("defaults to dynamic", () => {
    expect(DEFAULT_INBOX_MODE).toBe("dynamic");
  });

  it("falls back to the default when the preference is unset", () => {
    expect(toInboxMode(undefined)).toBe("dynamic");
    expect(toInboxMode(null)).toBe("dynamic");
  });

  it("falls back to the default for a garbage value", () => {
    expect(toInboxMode("nonsense")).toBe("dynamic");
    expect(toInboxMode(42)).toBe("dynamic");
    expect(toInboxMode({})).toBe("dynamic");
  });

  it("honours an explicit dynamic choice", () => {
    expect(toInboxMode("dynamic")).toBe("dynamic");
  });

  it("honours an explicit classic choice", () => {
    expect(toInboxMode("classic")).toBe("classic");
  });
});
