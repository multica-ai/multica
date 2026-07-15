import { afterEach, describe, expect, it, vi } from "vitest";
import { detectOS } from "./os-detect";

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("detectOS", () => {
  it("does not guess a Mac architecture from Safari's legacy platform string", async () => {
    vi.stubGlobal("navigator", {
      platform: "MacIntel",
      userAgent:
        "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 Safari/605.1.15",
    });

    await expect(detectOS()).resolves.toEqual({
      os: "mac",
      arch: "unknown",
      archConfident: false,
    });
  });

  it("uses high-entropy architecture data when the browser provides it", async () => {
    vi.stubGlobal("navigator", {
      platform: "MacIntel",
      userAgent: "",
      userAgentData: {
        getHighEntropyValues: vi.fn().mockResolvedValue({
          platform: "macOS",
          architecture: "x86",
        }),
      },
    });

    await expect(detectOS()).resolves.toEqual({
      os: "mac",
      arch: "x64",
      archConfident: true,
    });
  });
});
