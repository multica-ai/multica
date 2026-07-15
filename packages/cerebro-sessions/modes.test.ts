import { describe, expect, it } from "vitest";
import { SESSION_MODES, normalizeSessionMode } from "./modes";

describe("session modes", () => {
  it("exposes the four explicit working modes in product order", () => {
    expect(SESSION_MODES.map(({ value, label }) => [value, label])).toEqual([
      ["plan", "Plan"],
      ["build", "Build"],
      ["research", "Research"],
      ["review", "Review"],
    ]);
  });

  it("normalizes legacy and unknown API values safely", () => {
    expect(normalizeSessionMode("default")).toBe("build");
    expect(normalizeSessionMode("review")).toBe("review");
    expect(normalizeSessionMode("auto")).toBe("build");
    expect(normalizeSessionMode("future-mode")).toBe("build");
    expect(normalizeSessionMode(undefined)).toBe("build");
  });
});
