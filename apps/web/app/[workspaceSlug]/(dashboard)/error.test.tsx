import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { I18nProvider } from "@multica/core/i18n/react";
import { RESOURCES } from "@multica/views/locales";
import { captureException } from "@multica/core/analytics";
import DashboardError from "./error";

vi.mock("@multica/core/analytics", () => ({
  captureException: vi.fn(),
}));

describe("DashboardError", () => {
  it("shows a retry panel in the selected locale and reports the error", () => {
    const reset = vi.fn();
    const error = Object.assign(new Error("boom"), { digest: "d1" });
    render(
      <I18nProvider locale="zh-Hans" resources={RESOURCES}>
        <DashboardError error={error} reset={reset} />
      </I18nProvider>,
    );

    expect(screen.getByRole("alert")).toBeInTheDocument();
    expect(
      screen.getByRole("heading", { name: "此区域显示时出现问题。" }),
    ).toBeInTheDocument();
    expect(screen.getByText("boom")).toBeInTheDocument();
    expect(captureException).toHaveBeenCalledWith(error, {
      source: "dashboard-error",
      digest: "d1",
    });

    fireEvent.click(screen.getByRole("button", { name: "重试" }));
    expect(reset).toHaveBeenCalledTimes(1);
  });

  it("falls back to the generic description when the error has no message", () => {
    render(
      <I18nProvider locale="zh-Hans" resources={RESOURCES}>
        <DashboardError error={new Error("")} reset={() => {}} />
      </I18nProvider>,
    );

    expect(screen.getByText("发生了意外错误。")).toBeInTheDocument();
  });
});
