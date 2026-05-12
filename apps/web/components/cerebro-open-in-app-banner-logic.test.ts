import { describe, expect, it } from "vitest";
import {
  isMobileUserAgent,
  readDismissedAt,
  shouldShowOpenInAppBanner,
  writeDismissedAt,
} from "./cerebro-open-in-app-banner-logic";

const IPHONE_UA =
  "Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Mobile/15E148 Safari/604.1";
const IPAD_UA =
  "Mozilla/5.0 (iPad; CPU OS 17_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Mobile/15E148 Safari/604.1";
const ANDROID_CHROME_UA =
  "Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Mobile Safari/537.36";
const MAC_DESKTOP_UA =
  "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Safari/605.1.15";
const ANDROID_TABLET_UA =
  "Mozilla/5.0 (Linux; Android 14; SM-X910) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36";

describe("isMobileUserAgent", () => {
  it("detects iPhone", () => {
    expect(isMobileUserAgent(IPHONE_UA)).toBe(true);
  });
  it("detects iPad", () => {
    expect(isMobileUserAgent(IPAD_UA)).toBe(true);
  });
  it("detects Android phones (UA contains 'Mobile')", () => {
    expect(isMobileUserAgent(ANDROID_CHROME_UA)).toBe(true);
  });
  it("does not show on Mac desktop Safari", () => {
    expect(isMobileUserAgent(MAC_DESKTOP_UA)).toBe(false);
  });
  it("does not show on Android tablets that omit 'Mobile'", () => {
    // Tablet Chrome UA omits "Mobile"; we deliberately skip those because the
    // PWA experience on a tablet browser is acceptable as-is.
    expect(isMobileUserAgent(ANDROID_TABLET_UA)).toBe(false);
  });
  it("handles empty string defensively", () => {
    expect(isMobileUserAgent("")).toBe(false);
  });
});

describe("shouldShowOpenInAppBanner", () => {
  const NOW = new Date("2026-05-12T12:00:00Z").getTime();

  it("shows on a mobile browser that is not standalone and never dismissed", () => {
    expect(
      shouldShowOpenInAppBanner({
        ua: IPHONE_UA,
        isStandalone: false,
        dismissedAt: null,
        now: NOW,
      }),
    ).toBe(true);
  });

  it("hides when running as a standalone PWA", () => {
    expect(
      shouldShowOpenInAppBanner({
        ua: IPHONE_UA,
        isStandalone: true,
        dismissedAt: null,
        now: NOW,
      }),
    ).toBe(false);
  });

  it("hides on desktop browsers", () => {
    expect(
      shouldShowOpenInAppBanner({
        ua: MAC_DESKTOP_UA,
        isStandalone: false,
        dismissedAt: null,
        now: NOW,
      }),
    ).toBe(false);
  });

  it("hides for the dismiss-for window after a recent dismiss", () => {
    const oneDayAgo = NOW - 24 * 60 * 60 * 1000;
    expect(
      shouldShowOpenInAppBanner({
        ua: ANDROID_CHROME_UA,
        isStandalone: false,
        dismissedAt: oneDayAgo,
        dismissForDays: 7,
        now: NOW,
      }),
    ).toBe(false);
  });

  it("re-shows after the dismiss-for window expires", () => {
    const eightDaysAgo = NOW - 8 * 24 * 60 * 60 * 1000;
    expect(
      shouldShowOpenInAppBanner({
        ua: ANDROID_CHROME_UA,
        isStandalone: false,
        dismissedAt: eightDaysAgo,
        dismissForDays: 7,
        now: NOW,
      }),
    ).toBe(true);
  });

  it("ignores dismiss when dismissForDays is 0 (always show)", () => {
    expect(
      shouldShowOpenInAppBanner({
        ua: IPHONE_UA,
        isStandalone: false,
        dismissedAt: NOW - 1000,
        dismissForDays: 0,
        now: NOW,
      }),
    ).toBe(true);
  });

  it("standalone wins over not-yet-dismissed", () => {
    expect(
      shouldShowOpenInAppBanner({
        ua: IPHONE_UA,
        isStandalone: true,
        dismissedAt: null,
        dismissForDays: 7,
        now: NOW,
      }),
    ).toBe(false);
  });
});

describe("readDismissedAt / writeDismissedAt", () => {
  function makeStorage() {
    const data: Record<string, string> = {};
    return {
      data,
      storage: {
        getItem: (k: string) => data[k] ?? null,
        setItem: (k: string, v: string) => {
          data[k] = v;
        },
      } as Pick<Storage, "getItem" | "setItem">,
    };
  }

  it("round-trips a timestamp", () => {
    const { storage } = makeStorage();
    writeDismissedAt(storage, "k", 1234);
    expect(readDismissedAt(storage, "k")).toBe(1234);
  });

  it("returns null when storage is null (SSR)", () => {
    expect(readDismissedAt(null, "k")).toBeNull();
  });

  it("returns null on malformed values", () => {
    const { data, storage } = makeStorage();
    data["k"] = "not-a-number";
    expect(readDismissedAt(storage, "k")).toBeNull();
  });

  it("returns null when key is missing", () => {
    const { storage } = makeStorage();
    expect(readDismissedAt(storage, "k")).toBeNull();
  });

  it("swallows getItem throwing (private mode quota)", () => {
    const storage = {
      getItem: () => {
        throw new Error("QuotaExceededError");
      },
    } as Pick<Storage, "getItem">;
    expect(readDismissedAt(storage, "k")).toBeNull();
  });

  it("swallows setItem throwing (private mode quota)", () => {
    const storage = {
      setItem: () => {
        throw new Error("QuotaExceededError");
      },
    } as Pick<Storage, "setItem">;
    expect(() => writeDismissedAt(storage, "k", 1)).not.toThrow();
  });
});
