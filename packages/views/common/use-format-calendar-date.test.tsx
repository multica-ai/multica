import type { ReactNode } from "react";
import { describe, it, expect } from "vitest";
import { renderHook } from "@testing-library/react";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../locales/en/common.json";
import zhCommon from "../locales/zh-Hans/common.json";
import { useFormatCalendarDate } from "./use-format-calendar-date";

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

describe("useFormatCalendarDate", () => {
  it("takes its locale from the Language setting", () => {
    const en = renderHook(() => useFormatCalendarDate(), {
      wrapper: wrapper("en"),
    });
    expect(en.result.current("2026-03-01")).toBe("Mar 1");

    const zh = renderHook(() => useFormatCalendarDate(), {
      wrapper: wrapper("zh-Hans"),
    });
    expect(zh.result.current("2026-03-01")).toBe("3月1日");
  });

  it("passes through explicit format options", () => {
    const { result } = renderHook(() => useFormatCalendarDate(), {
      wrapper: wrapper("en"),
    });
    expect(
      result.current("2026-03-01", {
        year: "numeric",
        month: "long",
        day: "numeric",
      }),
    ).toBe("March 1, 2026");
  });

  it("stays floating — the day never shifts with a timezone (spec §3.2)", () => {
    const { result } = renderHook(() => useFormatCalendarDate(), {
      wrapper: wrapper("en"),
    });
    // A legacy RFC3339 instant is read as its UTC calendar day, NOT converted
    // into the viewer's timezone (which would slide it to Mar 1 east of UTC).
    expect(result.current("2026-02-28T16:00:00Z")).toBe("Feb 28");
  });

  it("returns an empty string for empty/unparseable values", () => {
    const { result } = renderHook(() => useFormatCalendarDate(), {
      wrapper: wrapper("en"),
    });
    expect(result.current(null)).toBe("");
    expect(result.current("")).toBe("");
  });
});
