import { memo, StrictMode } from "react";
import { act, cleanup, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import { RelativeTime } from "./relative-time";

beforeEach(() => {
  vi.useFakeTimers();
  vi.setSystemTime(new Date("2026-01-18T00:04:45Z"));
  vi.spyOn(document, "visibilityState", "get").mockReturnValue("visible");
});

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  vi.useRealTimers();
});

// Clock lifecycle coverage lives in core/hooks/use-relative-time-tick.test.tsx.
it("updates only timestamp leaves beneath memoized rows, preserving localized text", () => {
  const renderRow = vi.fn();
  const Row = memo(function Row() {
    renderRow();
    return <RelativeTime dateTime="2026-01-18T00:00:00Z" />;
  });
  render(
    <StrictMode>
      <I18nProvider locale="en" resources={{ en: { common: enCommon } }}>
        <Row />
        <Row />
      </I18nProvider>
    </StrictMode>,
  );
  expect(screen.getAllByText("4m ago")).toHaveLength(2);
  const renders = renderRow.mock.calls.length;
  act(() => vi.advanceTimersByTime(30_000));
  expect(screen.getAllByText("5m ago")).toHaveLength(2);
  expect(renderRow).toHaveBeenCalledTimes(renders);
  expect(screen.getAllByText("5m ago")[0]).toHaveAttribute("dateTime", "2026-01-18T00:00:00Z");
});
