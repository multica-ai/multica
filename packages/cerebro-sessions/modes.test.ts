import { describe, expect, it } from "vitest";
import { SESSION_MODES, normalizeSessionMode } from "./modes";

describe("session modes", () => {
  it("exposes the five fixed modes in product order", () => {
    expect(SESSION_MODES.map(({ value, label }) => [value, label])).toEqual([
      ["auto", "Auto"],
      ["plan", "Plan"],
      ["build", "Build"],
      ["research", "Research"],
      ["review", "Review"],
    ]);
  });

  it("normalizes legacy and unknown API values safely", () => {
    expect(normalizeSessionMode("default")).toBe("build");
    expect(normalizeSessionMode("review")).toBe("review");
    expect(normalizeSessionMode("future-mode")).toBe("auto");
    expect(normalizeSessionMode(undefined)).toBe("auto");
  });
});
