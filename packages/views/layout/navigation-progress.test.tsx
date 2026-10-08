import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import { NavigationProvider, useReportNavigating } from "../navigation";
import type { NavigationAdapter } from "../navigation";
import { NavigationProgress } from "./navigation-progress";

const adapter: NavigationAdapter = {
  push: vi.fn(),
  replace: vi.fn(),
  back: vi.fn(),
  pathname: "/",
  searchParams: new URLSearchParams(),
  hash: "",
  getShareableUrl: (p) => p,
};

function Reporter({ pending }: { pending: boolean }) {
  useReportNavigating(pending);
  return <span data-testid="reporter" />;
}

function renderBar(pending: boolean) {
  const view = render(
    <NavigationProvider value={adapter}>
      <NavigationProgress />
      <Reporter pending={pending} />
    </NavigationProvider>,
  );
  const bar = () => screen.getByTestId("reporter").previousElementSibling!;
  return { ...view, bar };
}

describe("NavigationProgress", () => {
  it("stays hidden while nothing is pending", () => {
    const { bar } = renderBar(false);

    expect(bar().getAttribute("data-visible")).toBe("false");
  });

  // The desktop shell's push/replace are synchronous store writes, so the
  // provider's own transition flag is already settled while the destination
  // content is still rendering. In-page swaps report the gap themselves.
  it("shows while a view reports an in-page swap", () => {
    const { bar } = renderBar(true);

    expect(bar().getAttribute("data-visible")).toBe("true");
  });

  it("hides again once the reported swap settles", () => {
    const { bar, rerender } = renderBar(true);

    rerender(
      <NavigationProvider value={adapter}>
        <NavigationProgress />
        <Reporter pending={false} />
      </NavigationProvider>,
    );

    expect(bar().getAttribute("data-visible")).toBe("false");
  });

  it("hides when the only reporting view unmounts mid-swap", () => {
    const { bar, rerender } = renderBar(true);

    rerender(
      <NavigationProvider value={adapter}>
        <NavigationProgress />
        <span data-testid="reporter" />
      </NavigationProvider>,
    );

    expect(bar().getAttribute("data-visible")).toBe("false");
  });
});
