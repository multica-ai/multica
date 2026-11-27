import { afterEach, describe, expect, it, vi } from "vitest";
import { isStandalonePwa } from "./standalone";

function stubMatchMedia(matches: boolean) {
  vi.stubGlobal("matchMedia", (query: string) => ({
    matches: matches && query === "(display-mode: standalone)",
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
  }));
}

afterEach(() => {
  vi.unstubAllGlobals();
  delete (navigator as unknown as Record<string, unknown>).standalone;
  delete (window as unknown as Record<string, unknown>).desktopAPI;
});

describe("isStandalonePwa", () => {
  it("is false in a plain browser tab", () => {
    stubMatchMedia(false);
    expect(isStandalonePwa()).toBe(false);
  });

  it("follows the display-mode media query", () => {
    stubMatchMedia(true);
    expect(isStandalonePwa()).toBe(true);
  });

  it("follows iOS Safari's navigator.standalone", () => {
    stubMatchMedia(false);
    Object.defineProperty(navigator, "standalone", {
      value: true,
      configurable: true,
    });
    expect(isStandalonePwa()).toBe(true);
  });

  it("is false inside the desktop shell, which has its own window frame", () => {
    stubMatchMedia(true);
    (window as unknown as { desktopAPI: object }).desktopAPI = {};
    expect(isStandalonePwa()).toBe(false);
  });
});
