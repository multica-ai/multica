// @vitest-environment jsdom

import { describe, expect, it, vi } from "vitest";
import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { GlmQuotaStatus } from "@multica/core/api";
import { renderWithI18n } from "../../test/i18n";
import { GlmQuotaCard } from "./glm-quota-card";
import { formatQuotaTime } from "./quota-time";

vi.mock("@multica/core/auth", () => {
  type AuthState = { user: { timezone: string } };
  const state = (): AuthState => ({ user: { timezone: "Asia/Shanghai" } });
  const useAuthStore = Object.assign(
    (sel: (s: AuthState) => unknown) => sel(state()),
    { getState: state },
  );
  return { useAuthStore };
});

const NOW_SEC = 1_800_000_000;
const NOW = NOW_SEC * 1000;

function at(sec: number, locale = "en") {
  return formatQuotaTime(sec, NOW, "Asia/Shanghai", locale);
}

function status(windows: NonNullable<GlmQuotaStatus["quota"]>["windows"], observedAt = NOW_SEC - 120): GlmQuotaStatus {
  return { enabled: true, quota: { level: "pro", windows, observed_at: observedAt } };
}

function chip(label: string) {
  return screen.getByText(label).closest<HTMLElement>('[data-slot="tooltip-trigger"]')!;
}

describe("GlmQuotaCard", () => {
  it("shows the reset as a time in the viewer's timezone", async () => {
    renderWithI18n(
      <GlmQuotaCard
        data={status([{ type: "TOKENS_LIMIT", window_minutes: 300, used_percent: 30, resets_at: NOW_SEC + 2 * 3600 }])}
        now={NOW}
      />,
    );
    expect(screen.getByText("70% left")).toBeInTheDocument();
    await userEvent.hover(chip("5-hour tokens"));
    const reset = await screen.findByText(`resets at ${at(NOW_SEC + 2 * 3600)}`);
    expect(reset.closest('[data-slot="tooltip-content"]')).toHaveTextContent("updated 2m ago");
  });

  it("counts a window past its reset as refilled", async () => {
    renderWithI18n(
      <GlmQuotaCard
        data={status([{ type: "TOKENS_LIMIT", window_minutes: 300, used_percent: 95, usage: 100, remaining: 5, current_value: 95, resets_at: NOW_SEC - 60 }])}
        now={NOW}
      />,
    );
    expect(screen.getByText("100% left")).toBeInTheDocument();
    await userEvent.hover(chip("5-hour tokens"));
    const reset = await screen.findByText(`reset at ${at(NOW_SEC - 60)}`);
    expect(reset.closest('[data-slot="tooltip-content"]')).not.toHaveTextContent("95 / 100");
  });

  it("leaves out a reset time past the Date range", async () => {
    renderWithI18n(
      <GlmQuotaCard
        data={status([{ type: "TOKENS_LIMIT", window_minutes: 300, used_percent: 30, resets_at: 9_000_000_000_000 }])}
        now={NOW}
      />,
    );
    await userEvent.hover(chip("5-hour tokens"));
    const updated = await screen.findByText("updated 2m ago");
    expect(updated.closest('[data-slot="tooltip-content"]')).not.toHaveTextContent("resets at");
  });

  it("flags an hour without a successful poll and keeps the balance", () => {
    renderWithI18n(
      <GlmQuotaCard
        data={status([{ type: "TIME_LIMIT", used_percent: 16, resets_at: NOW_SEC + 10 * 86400 }], NOW_SEC - 3601)}
        now={NOW}
      />,
      { locale: "zh-Hans" },
    );
    expect(screen.getByText(`采集中断 · 最后更新 ${at(NOW_SEC - 3601, "zh-Hans")}`)).toBeInTheDocument();
    expect(chip("剩 84%")).toHaveClass("bg-muted");
  });
});

// OL-1205: two TOKENS_LIMIT windows (5h + weekly) must render distinct
// labels — before the fix both hardcoded to "5-hour tokens".
it("labels two TOKENS_LIMIT windows by their real period", () => {
  renderWithI18n(
    <GlmQuotaCard
      data={status([
        { type: "TOKENS_LIMIT", window_minutes: 300, used_percent: 30, resets_at: NOW_SEC + 3600 },
        { type: "TOKENS_LIMIT", window_minutes: 10080, used_percent: 60, resets_at: NOW_SEC + 86400 },
        { type: "TIME_LIMIT", used_percent: 16, resets_at: NOW_SEC + 10 * 86400 },
      ])}
      now={NOW}
    />,
  );
  expect(screen.getByText("5-hour tokens")).toBeInTheDocument();
  expect(screen.getByText("Weekly tokens")).toBeInTheDocument();
  expect(screen.getByText("Monthly tool credits")).toBeInTheDocument();
  expect(screen.getAllByText("5-hour tokens")).toHaveLength(1);
});

// A window without a recognizable period falls back to the generic type
// label instead of guessing "5-hour".
it("labels an unlabeled TOKENS_LIMIT window generically", () => {
  renderWithI18n(
    <GlmQuotaCard
      data={status([{ type: "TOKENS_LIMIT", used_percent: 40, resets_at: NOW_SEC + 3600 }])}
      now={NOW}
    />,
  );
  expect(screen.getByText("Token window")).toBeInTheDocument();
  expect(screen.queryByText("5-hour tokens")).not.toBeInTheDocument();
});

// Credit pools get the same period split.
it("labels 5h and weekly CREDIT_LIMIT windows distinctly", () => {
  renderWithI18n(
    <GlmQuotaCard
      data={status([
        { type: "CREDIT_LIMIT", window_minutes: 300, usage: 100, remaining: 80 },
        { type: "CREDIT_LIMIT", window_minutes: 10080, usage: 500, remaining: 400 },
      ])}
      now={NOW}
    />,
  );
  expect(screen.getByText("5-hour credits")).toBeInTheDocument();
  expect(screen.getByText("Weekly credits")).toBeInTheDocument();
});
