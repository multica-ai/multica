import type { ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";
import { render as rtlRender, screen, fireEvent, type RenderOptions } from "@testing-library/react";
import { I18nProvider } from "@multica/core/i18n/react";
import { ApiError, EndpointUnavailableError } from "@multica/core/api";
import enIssues from "../../locales/en/issues.json";
import { ListLoadMoreFooter } from "./list-load-more-footer";

const TEST_RESOURCES = {
  en: { issues: enIssues },
};

function I18nWrapper({ children }: { children: ReactNode }) {
  return (
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      {children}
    </I18nProvider>
  );
}

function render(ui: React.ReactElement, options?: RenderOptions) {
  return rtlRender(ui, { wrapper: I18nWrapper, ...options });
}

const BASE_PROPS = {
  hasMore: false,
  isLoading: false,
  total: 0,
  onLoadMore: () => {},
};

describe("ListLoadMoreFooter", () => {
  it("shows the generic retry copy for an ordinary error", () => {
    render(
      <ListLoadMoreFooter
        {...BASE_PROPS}
        isError
        error={new ApiError("boom", 500, "Internal Server Error")}
        onRetry={() => {}}
      />,
    );
    expect(
      screen.getByRole("button", { name: "Loading more failed — Retry" }),
    ).toBeInTheDocument();
  });

  it("shows the upgrade-server copy for an EndpointUnavailableError, keeping Retry", () => {
    const onRetry = vi.fn();
    render(
      <ListLoadMoreFooter
        {...BASE_PROPS}
        isError
        error={new EndpointUnavailableError("API error: 404 Not Found", 404, "Not Found")}
        onRetry={onRetry}
      />,
    );
    const button = screen.getByRole("button", {
      name: "Server version is too old and lacks this endpoint — please upgrade the server — Retry",
    });
    fireEvent.click(button);
    expect(onRetry).toHaveBeenCalledOnce();
  });

  it("falls back to the generic copy when the error is unknown", () => {
    render(
      <ListLoadMoreFooter {...BASE_PROPS} isError onRetry={() => {}} />,
    );
    expect(
      screen.getByRole("button", { name: "Loading more failed — Retry" }),
    ).toBeInTheDocument();
  });
});
