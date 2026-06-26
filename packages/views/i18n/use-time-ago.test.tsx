import type { ReactNode } from "react";
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { renderHook } from "@testing-library/react";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../locales/en/common.json";
import zhCommon from "../locales/zh-Hans/common.json";

// Calendar-day / month / year buckets all depend on the viewer's Viewing
// Timezone, so the auth user's timezone drives these tests.
const userRef = vi.hoisted(
  () => ({ current: { timezone: "UTC" } as { timezone?: string | null } }),
);

vi.mock("@multica/core/auth", () => {
  type AuthState = { user: typeof userRef.current };
  const useAuthStore = Object.assign(
    (sel: (s: AuthState) => unknown) => sel({ user: userRef.current }),
    { getState: () => ({ user: userRef.current }) },
  );
  return { useAuthStore };
});

vi.mock("../common/timezone-select", () => ({
  browserTimezone: () => "America/New_York",
}));

import { useTimeAgo } from "./use-time-ago";

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

// Fixed "now"; fake only Date so React/i18next scheduling keeps real timers.
const NOW = "2026-03-15T12:00:00Z";

describe("useTimeAgo", () => {
  beforeEach(() => {
    vi.useFakeTimers({ toFake: ["Date"] });
    vi.setSystemTime(new Date(NOW));
    userRef.current = { timezone: "UTC" };
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("buckets sub-day diffs by elapsed minutes/hours", () => {
    const { result } = renderHook(() => useTimeAgo(), { wrapper: wrapper("en") });
    const timeAgo = result.current;
    expect(timeAgo("2026-03-15T11:59:30Z")).toBe("just now"); // 30s
    expect(timeAgo("2026-03-15T11:55:00Z")).toBe("5m ago"); // 5m
    expect(timeAgo("2026-03-15T09:00:00Z")).toBe("3h ago"); // 3h
    expect(timeAgo("2026-03-14T13:00:00Z")).toBe("23h ago"); // 23h, still hours
  });

  it("counts day granularity by calendar days, not 24h windows", () => {
    const { result } = renderHook(() => useTimeAgo(), { wrapper: wrapper("en") });
    const timeAgo = result.current;
    // 25h ago but one calendar day → yesterday → "1d ago".
    expect(timeAgo("2026-03-14T11:00:00Z")).toBe("1d ago");
    // 47h ago (under 48h) but TWO calendar days → "2d ago", not "1d ago".
    // This is the motivating bug from the user report.
    expect(timeAgo("2026-03-13T13:00:00Z")).toBe("2d ago");
  });

  it("renders future instants in the opposite direction", () => {
    const { result } = renderHook(() => useTimeAgo(), { wrapper: wrapper("en") });
    const timeAgo = result.current;
    expect(timeAgo("2026-03-15T15:00:00Z")).toBe("in 3h"); // +3h
    expect(timeAgo("2026-03-17T12:00:00Z")).toBe("in 2d"); // +2 calendar days
    expect(timeAgo("2026-06-15T12:00:00Z")).toBe("in 3mo"); // +3 calendar months
  });

  it("renders imminent future instants as 'soon' only with soonForFuture", () => {
    const plain = renderHook(() => useTimeAgo(), { wrapper: wrapper("en") });
    // Without the option a sub-minute future instant stays "just now".
    expect(plain.result.current("2026-03-15T12:00:30Z")).toBe("just now"); // +30s

    const soon = renderHook(() => useTimeAgo({ soonForFuture: true }), {
      wrapper: wrapper("en"),
    });
    expect(soon.result.current("2026-03-15T12:00:30Z")).toBe("soon"); // +30s
    // A past sub-minute instant keeps "just now" even with the option.
    expect(soon.result.current("2026-03-15T11:59:30Z")).toBe("just now"); // -30s
  });

  it("localizes the labels via the Language setting", () => {
    const { result } = renderHook(() => useTimeAgo(), {
      wrapper: wrapper("zh-Hans"),
    });
    expect(result.current("2026-03-15T11:55:00Z")).toBe("5 分钟前");
    expect(result.current("2026-03-13T13:00:00Z")).toBe("2 天前");
    expect(result.current("2026-03-15T15:00:00Z")).toBe("3 小时后");
  });

  it("continues into calendar months and years past the day cap", () => {
    const en = renderHook(() => useTimeAgo(), { wrapper: wrapper("en") });
    expect(en.result.current("2026-02-13T12:00:00Z")).toBe("30d ago"); // exactly 30d
    expect(en.result.current("2026-02-12T12:00:00Z")).toBe("1mo ago"); // 31d → months
    expect(en.result.current("2025-03-15T12:00:00Z")).toBe("1y ago"); // exactly 1 year

    const zh = renderHook(() => useTimeAgo(), { wrapper: wrapper("zh-Hans") });
    expect(zh.result.current("2026-02-12T12:00:00Z")).toBe("1 个月前");
    expect(zh.result.current("2025-03-15T12:00:00Z")).toBe("1 年前");
  });

  it("renders a placeholder for an unparseable date instead of blank", () => {
    const { result } = renderHook(() => useTimeAgo(), { wrapper: wrapper("en") });
    expect(result.current("not-a-date")).toBe("—");
  });

  it("makes the day bucket depend on the Viewing timezone", () => {
    // 37h apart, straddling UTC midnight.
    const then = "2026-03-13T23:00:00Z";
    const utc = renderHook(() => useTimeAgo(), { wrapper: wrapper("en") });
    expect(utc.result.current(then)).toBe("2d ago"); // UTC: Mar 13 → Mar 15

    userRef.current = { timezone: "Asia/Tokyo" };
    const tokyo = renderHook(() => useTimeAgo(), { wrapper: wrapper("en") });
    expect(tokyo.result.current(then)).toBe("1d ago"); // Tokyo: Mar 14 → Mar 15
  });
});
