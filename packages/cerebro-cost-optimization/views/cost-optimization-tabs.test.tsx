import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { CostOptimizationTabs } from "./cost-optimization-tabs";

const replace = vi.fn();
let pathname = "/firtal/settings";
let params = new URLSearchParams();

vi.mock("next/navigation", () => ({
  usePathname: () => pathname,
  useRouter: () => ({ replace }),
  useSearchParams: () => params,
}));

vi.mock("./settings-panel", () => ({
  CostOptimizationSettingsPanel: () => <div>settings-panel</div>,
}));

vi.mock("./analytics-tab", () => ({
  CostOptimizationAnalyticsTab: () => <div>analytics-panel</div>,
}));

vi.mock("./inspector-tab", () => ({
  CostOptimizationInspectorTab: () => <div>inspector-panel</div>,
}));

describe("CostOptimizationTabs", () => {
  beforeEach(() => {
    replace.mockClear();
    pathname = "/firtal/settings";
    params = new URLSearchParams("tab=cost-optimization");
  });

  it("defaults to Settings when no cost-optimization subtab is present", () => {
    render(<CostOptimizationTabs />);

    expect(screen.getByText("settings-panel")).toBeInTheDocument();
    expect(screen.getByText("Settings")).toBeInTheDocument();
    expect(screen.getByText("Analytics")).toBeInTheDocument();
    expect(screen.getByText("System prompt inspector")).toBeInTheDocument();
  });

  it("honors analytics deeplink and preserves the parent settings tab in URL changes", () => {
    params = new URLSearchParams("tab=cost-optimization&subtab=analytics");
    render(<CostOptimizationTabs />);

    expect(screen.getByText("analytics-panel")).toBeInTheDocument();

    fireEvent.click(screen.getByText("System prompt inspector"));

    expect(replace).toHaveBeenCalledWith(
      "/firtal/settings?tab=cost-optimization&subtab=inspector",
    );
  });
});
