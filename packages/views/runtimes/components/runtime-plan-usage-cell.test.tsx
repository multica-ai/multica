// @vitest-environment jsdom

import type { ReactNode } from "react";
import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enRuntimes from "../../locales/en/runtimes.json";
import { RuntimePlanUsageCell } from "./runtime-plan-usage-cell";

const TEST_RESOURCES = { en: { common: enCommon, runtimes: enRuntimes } };

function Wrapper({ children }: { children: ReactNode }) {
  return (
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      {children}
    </I18nProvider>
  );
}

const NOW = Date.parse("2026-09-22T12:00:00.000Z");

describe("RuntimePlanUsageCell", () => {
  it("shows the headline percent and a short reset for a snapshot", () => {
    const resetsAt = "2026-09-22T14:00:00.000Z";
    render(
      <RuntimePlanUsageCell
        provider="claude"
        now={NOW}
        providers={[
          {
            provider: "claude",
            plan_name: "Max",
            windows: [
              { id: "session", percent_used: 38.2, resets_at: resetsAt },
              { id: "weekly_all", percent_used: 4, resets_at: resetsAt },
            ],
          },
        ]}
      />,
      { wrapper: Wrapper },
    );

    expect(screen.getByText("38%")).toBeInTheDocument();
    expect(screen.queryByText("4%")).not.toBeInTheDocument();
    expect(
      screen.getByText(
        new Intl.RelativeTimeFormat("en", { numeric: "always", style: "narrow" }).format(
          2,
          "hour",
        ),
      ),
    ).toBeInTheDocument();
    expect(screen.queryByText("Max")).not.toBeInTheDocument();
  });

  it("shows the plan name when the snapshot has no short reset", () => {
    render(
      <RuntimePlanUsageCell
        provider="cursor"
        now={NOW}
        providers={[
          {
            provider: "cursor",
            plan_name: "Pro",
            windows: [
              { id: "api", percent_used: 80 },
              { id: "auto", percent_used: 12 },
            ],
          },
        ]}
      />,
      { wrapper: Wrapper },
    );

    expect(screen.getByText("12%")).toBeInTheDocument();
    expect(screen.getByText("Pro")).toBeInTheDocument();
    expect(screen.queryByText("80%")).not.toBeInTheDocument();
  });

  it("renders an empty cell when the provider is not logged in", () => {
    render(
      <RuntimePlanUsageCell
        provider="codex"
        providers={[
          {
            provider: "codex",
            reason_code: "not_logged_in",
            windows: [],
          },
        ]}
      />,
      { wrapper: Wrapper },
    );

    expect(screen.getByText("—")).toBeInTheDocument();
    expect(screen.queryByText(/%/)).not.toBeInTheDocument();
  });

  it("shows the Grok credits headline and ignores other vendors", () => {
    render(
      <RuntimePlanUsageCell
        provider="grok"
        now={NOW}
        providers={[
          {
            provider: "claude",
            windows: [{ id: "session", percent_used: 38 }],
          },
          {
            provider: "grok",
            plan_name: "Grok Build",
            windows: [{ id: "credits", percent_used: 8 }],
          },
        ]}
      />,
      { wrapper: Wrapper },
    );

    expect(screen.getByText("8%")).toBeInTheDocument();
    expect(screen.getByText("Grok Build")).toBeInTheDocument();
    expect(screen.queryByText("38%")).not.toBeInTheDocument();
  });

  it("renders an empty cell for a provider with no plan limits", () => {
    render(
      <RuntimePlanUsageCell
        provider="qwen"
        providers={[
          {
            provider: "claude",
            plan_name: "Max",
            windows: [{ id: "session", percent_used: 38 }],
          },
          {
            provider: "mystery",
            windows: [{ id: "primary", percent_used: 90 }],
          },
        ]}
      />,
      { wrapper: Wrapper },
    );

    expect(screen.getByText("—")).toBeInTheDocument();
    expect(screen.queryByText("38%")).not.toBeInTheDocument();
    expect(screen.queryByText("90%")).not.toBeInTheDocument();
  });
});
