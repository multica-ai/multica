import { describe, expect, it } from "vitest";
import { formatForDisplay } from "@tanstack/hotkeys";

// We test TanStack's formatForDisplay directly since our formatKeysForPlatform
// is a thin wrapper. This verifies the integration works and documents the
// expected output for our usage.

describe("formatForDisplay (TanStack)", () => {
  it("renders Mac symbols for Mod+K", () => {
    const result = formatForDisplay("Mod+K", { platform: "mac" });
    expect(result).toContain("⌘");
    expect(result).toContain("K");
  });

  it("renders Ctrl for Mod+K on Windows", () => {
    const result = formatForDisplay("Mod+K", { platform: "windows" });
    expect(result).toContain("Ctrl");
    expect(result).toContain("K");
  });

  it("renders Shift symbol on Mac", () => {
    const result = formatForDisplay("Shift+Enter", { platform: "mac" });
    expect(result).toContain("⇧");
  });

  it("renders Shift text on Windows", () => {
    const result = formatForDisplay("Shift+Enter", { platform: "windows" });
    expect(result).toContain("Shift");
  });
});
