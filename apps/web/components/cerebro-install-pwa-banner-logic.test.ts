import { describe, it, expect } from "vitest";
import {
  detectMobilePlatform,
  shouldShowInstallBanner,
  readDismissedAt,
  writeDismissedAt,
} from "./cerebro-install-pwa-banner-logic";

const IPHONE_UA =
  "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1";
const ANDROID_UA =
  "Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Mobile Safari/537.36";
const DESKTOP_UA =
  "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36";
const IPAD_DESKTOP_UA =
  "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Safari/605.1.15";

describe("detectMobilePlatform", () => {
  it("detects iPhone as ios", () => {
    expect(detectMobilePlatform({ ua: IPHONE_UA })).toBe("ios");
  });

  it("detects Android as android", () => {
    expect(detectMobilePlatform({ ua: ANDROID_UA })).toBe("android");
  });

  it("treats desktop browsers as other", () => {
    expect(detectMobilePlatform({ ua: DESKTOP_UA })).toBe("other");
  });

  it("treats a touch-capable Mac (iPadOS 13+) as ios", () => {
    expect(
      detectMobilePlatform({
        ua: IPAD_DESKTOP_UA,
        maxTouchPoints: 5,
        platform: "MacIntel",
      }),
    ).toBe("ios");
  });

  it("treats a real desktop Mac (no touch) as other", () => {
    expect(
      detectMobilePlatform({
        ua: IPAD_DESKTOP_UA,
        maxTouchPoints: 0,
        platform: "MacIntel",
      }),
    ).toBe("other");
  });

  it("returns other for empty input", () => {
    expect(detectMobilePlatform({ ua: "" })).toBe("other");
  });
});

describe("shouldShowInstallBanner", () => {
  const base = { platform: "android" as const, isStandalone: false };

  it("shows on mobile when not installed and never dismissed", () => {
    expect(shouldShowInstallBanner(base)).toBe(true);
  });

  it("hides when running standalone (already installed)", () => {
    expect(
      shouldShowInstallBanner({ ...base, isStandalone: true }),
    ).toBe(false);
  });

  it("hides on desktop / non-mobile", () => {
    expect(
      shouldShowInstallBanner({ ...base, platform: "other" }),
    ).toBe(false);
  });

  it("hides within the dismiss window", () => {
    const now = 1_000_000_000_000;
    const dismissedAt = now - 2 * 24 * 60 * 60 * 1000; // 2 days ago
    expect(
      shouldShowInstallBanner({
        ...base,
        dismissedAt,
        dismissForDays: 7,
        now,
      }),
    ).toBe(false);
  });

  it("shows again after the dismiss window elapses", () => {
    const now = 1_000_000_000_000;
    const dismissedAt = now - 8 * 24 * 60 * 60 * 1000; // 8 days ago
    expect(
      shouldShowInstallBanner({
        ...base,
        dismissedAt,
        dismissForDays: 7,
        now,
      }),
    ).toBe(true);
  });

  it("shows on iOS too", () => {
    expect(
      shouldShowInstallBanner({ ...base, platform: "ios" }),
    ).toBe(true);
  });
});

describe("dismiss persistence", () => {
  function makeStorage(): Storage {
    const map = new Map<string, string>();
    return {
      getItem: (k) => map.get(k) ?? null,
      setItem: (k, v) => void map.set(k, v),
      removeItem: (k) => void map.delete(k),
      clear: () => map.clear(),
      key: () => null,
      length: 0,
    } as Storage;
  }

  it("round-trips a timestamp", () => {
    const storage = makeStorage();
    writeDismissedAt(storage, "k", 123456);
    expect(readDismissedAt(storage, "k")).toBe(123456);
  });

  it("returns null when unset or unparseable", () => {
    const storage = makeStorage();
    expect(readDismissedAt(storage, "missing")).toBeNull();
    storage.setItem("bad", "not-a-number");
    expect(readDismissedAt(storage, "bad")).toBeNull();
  });

  it("tolerates a null storage", () => {
    expect(readDismissedAt(null, "k")).toBeNull();
    expect(() => writeDismissedAt(null, "k", 1)).not.toThrow();
  });
});
