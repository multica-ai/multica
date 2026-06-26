import type { ReactNode } from "react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { renderHook } from "@testing-library/react";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../locales/en/common.json";
import zhCommon from "../locales/zh-Hans/common.json";

// Viewing tz is driven by the user setting, never the browser (spec §0 goal 2).
const userRef = vi.hoisted(
  () => ({ current: null as { timezone?: string | null } | null }),
);

vi.mock("@multica/core/auth", () => {
  type AuthState = { user: typeof userRef.current };
  const useAuthStore = Object.assign(
    (sel: (s: AuthState) => unknown) => sel({ user: userRef.current }),
    { getState: () => ({ user: userRef.current }) },
  );
  return { useAuthStore };
});

// If the hook ever fell back to the browser tz this distinctive value would
// surface in an assertion, proving the fallback was NOT taken.
vi.mock("./timezone-select", () => ({
  browserTimezone: () => "America/New_York",
}));

import {
  useFormatDateTime,
  useFormatInstantTooltip,
} from "./use-format-date-time";

const RESOURCES = {
  en: { common: enCommon },
  "zh-Hans": { common: zhCommon },
};

function wrapper(locale: "en" | "zh-Hans") {
  return function Wrapper({ children }: { children: ReactNode }) {
    return (
      <I18nProvider locale={locale} resources={RESOURCES}>
        {children}
      </I18nProvider>
    );
  };
}

// 2026-03-01T06:30:45Z == 14:30:45 Asia/Shanghai (+8), 22:30:45 prev day in LA.
const VALUE = "2026-03-01T06:30:45Z";

describe("useFormatDateTime", () => {
  beforeEach(() => {
    userRef.current = null;
  });

  it("renders an instant in the viewer's Viewing Timezone, not the browser", () => {
    userRef.current = { timezone: "Asia/Shanghai" };
    const sh = renderHook(() => useFormatDateTime(), {
      wrapper: wrapper("en"),
    });
    expect(sh.result.current.formatDateTime(VALUE)).toBe(
      "Mar 1, 2026, 02:30 PM",
    );

    userRef.current = { timezone: "America/Los_Angeles" };
    const la = renderHook(() => useFormatDateTime(), {
      wrapper: wrapper("en"),
    });
    // Same instant, different wall clock — and neither is the mocked browser tz.
    expect(la.result.current.formatDateTime(VALUE)).toBe(
      "Feb 28, 2026, 10:30 PM",
    );
  });

  it("takes its locale from the Language setting", () => {
    userRef.current = { timezone: "Asia/Shanghai" };
    const { result } = renderHook(() => useFormatDateTime(), {
      wrapper: wrapper("zh-Hans"),
    });
    expect(result.current.formatDateTime(VALUE)).toBe("2026年3月1日 14:30");
  });

  it("exposes date-only and time-only formatters", () => {
    // Pin "now" so date mode's current-year suppression is deterministic.
    vi.useFakeTimers({ toFake: ["Date"] });
    vi.setSystemTime(new Date("2026-06-26T12:00:00Z"));
    try {
      userRef.current = { timezone: "Asia/Shanghai" };
      const { result } = renderHook(() => useFormatDateTime(), {
        wrapper: wrapper("en"),
      });
      // Same year as "now" → date mode drops the year.
      expect(result.current.formatDate(VALUE)).toBe("Mar 1");
      expect(result.current.formatTime(VALUE)).toBe("02:30:45 PM");
    } finally {
      vi.useRealTimers();
    }
  });

  it("builds a tooltip with full time + GMT offset of the Viewing tz", () => {
    userRef.current = { timezone: "America/Los_Angeles" };
    const en = renderHook(() => useFormatDateTime(), {
      wrapper: wrapper("en"),
    });
    expect(en.result.current.formatTooltip(VALUE)).toBe(
      "Feb 28, 2026, 10:30:45 PM (GMT-8)",
    );

    userRef.current = { timezone: "Asia/Shanghai" };
    const zh = renderHook(() => useFormatDateTime(), {
      wrapper: wrapper("zh-Hans"),
    });
    // CJK locales get full-width parentheses.
    expect(zh.result.current.formatTooltip(VALUE)).toBe(
      "2026年3月1日 14:30:45（GMT+8）",
    );
  });

  it("returns empty strings for empty/unparseable values", () => {
    userRef.current = { timezone: "UTC" };
    const { result } = renderHook(() => useFormatDateTime(), {
      wrapper: wrapper("en"),
    });
    expect(result.current.formatDateTime(null)).toBe("");
    expect(result.current.formatTooltip("not-a-date")).toBe("");
  });
});

describe("useFormatInstantTooltip", () => {
  beforeEach(() => {
    userRef.current = null;
  });

  it("matches the full hook's tooltip, in the Viewing tz + Language locale", () => {
    userRef.current = { timezone: "America/Los_Angeles" };
    const en = renderHook(() => useFormatInstantTooltip(), {
      wrapper: wrapper("en"),
    });
    expect(en.result.current(VALUE)).toBe("Feb 28, 2026, 10:30:45 PM (GMT-8)");

    userRef.current = { timezone: "Asia/Shanghai" };
    const zh = renderHook(() => useFormatInstantTooltip(), {
      wrapper: wrapper("zh-Hans"),
    });
    expect(zh.result.current(VALUE)).toBe("2026年3月1日 14:30:45（GMT+8）");
  });

  it("returns an empty string for an empty/unparseable value", () => {
    userRef.current = { timezone: "UTC" };
    const { result } = renderHook(() => useFormatInstantTooltip(), {
      wrapper: wrapper("en"),
    });
    expect(result.current(null)).toBe("");
    expect(result.current("not-a-date")).toBe("");
  });
});
