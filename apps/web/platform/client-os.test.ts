import { describe, expect, it } from "vitest";
import { detectWebOS } from "./client-os";

describe("detectWebOS", () => {
  it.each([
    ["MacIntel", "Mozilla/5.0", "macos"],
    ["Win32", "Mozilla/5.0", "windows"],
    ["Linux x86_64", "Mozilla/5.0 (X11; Linux x86_64)", "linux"],
    ["Linux armv8l", "Mozilla/5.0 (Linux; Android 15)", "android"],
    ["Linux x86_64", "Mozilla/5.0 (X11; CrOS x86_64)", "chromeos"],
    ["MacIntel", "Mozilla/5.0 (iPhone; CPU iPhone OS 18_0)", "ios"],
  ])("maps %s to %s", (platform, userAgent, expected) => {
    expect(detectWebOS({ platform, userAgent } as Navigator)).toBe(expected);
  });
});
