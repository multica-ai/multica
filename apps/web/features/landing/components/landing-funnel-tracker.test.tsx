import { act, render } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const { captureQualifiedLandingView, claimQualifiedLandingView } = vi.hoisted(
  () => ({
    captureQualifiedLandingView: vi.fn(),
    claimQualifiedLandingView: vi.fn(() => true),
  }),
);

vi.mock("../analytics", () => ({
  QUALIFIED_VIEW_DELAY_MS: 3_000,
  captureQualifiedLandingView,
  claimQualifiedLandingView,
  isQualifiedLandingContext: vi.fn(() => true),
}));

import { LandingFunnelTracker } from "./landing-funnel-tracker";

describe("LandingFunnelTracker", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    captureQualifiedLandingView.mockClear();
    claimQualifiedLandingView.mockClear();
    claimQualifiedLandingView.mockReturnValue(true);
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("captures after the qualification delay and only once per mount", () => {
    render(<LandingFunnelTracker />);

    act(() => vi.advanceTimersByTime(2_999));
    expect(captureQualifiedLandingView).not.toHaveBeenCalled();

    act(() => vi.advanceTimersByTime(1));
    expect(claimQualifiedLandingView).toHaveBeenCalledTimes(1);
    expect(captureQualifiedLandingView).toHaveBeenCalledTimes(1);

    act(() => {
      document.dispatchEvent(new Event("visibilitychange"));
      vi.advanceTimersByTime(3_000);
    });
    expect(captureQualifiedLandingView).toHaveBeenCalledTimes(1);
  });

  it("does not capture when another tab-session claim already exists", () => {
    claimQualifiedLandingView.mockReturnValue(false);
    render(<LandingFunnelTracker />);

    act(() => vi.advanceTimersByTime(3_000));

    expect(claimQualifiedLandingView).toHaveBeenCalledTimes(1);
    expect(captureQualifiedLandingView).not.toHaveBeenCalled();
  });
});
