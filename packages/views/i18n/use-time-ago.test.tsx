import { renderHook } from "@testing-library/react";
import { I18nProvider } from "@multica/core/i18n/react";
import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { RESOURCES } from "../locales";
import { useTimeAgo } from "./use-time-ago";
import type { SupportedLocale } from "@multica/core/i18n";

function wrapperFor(locale: SupportedLocale) {
  return function Wrapper({ children }: { children: ReactNode }) {
    return (
      <I18nProvider locale={locale} resources={RESOURCES}>
        {children}
      </I18nProvider>
    );
  };
}

describe("useTimeAgo", () => {
  afterEach(() => {
    vi.useRealTimers();
  });

  it("keeps relative timestamps for English", () => {
    vi.setSystemTime(new Date("2026-05-19T16:30:00"));

    const { result } = renderHook(() => useTimeAgo(), { wrapper: wrapperFor("en") });

    expect(result.current("2026-05-19T14:30:00")).toBe("2h ago");
  });

  it("uses absolute Chinese date-time labels for Simplified Chinese", () => {
    vi.setSystemTime(new Date("2026-05-19T16:30:00"));

    const { result } = renderHook(() => useTimeAgo(), {
      wrapper: wrapperFor("zh-Hans"),
    });

    expect(result.current("2026-05-19T14:30:00")).toBe("2026年5月19日 14:30");
  });
});
