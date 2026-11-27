import { afterEach, expect, it, vi } from "vitest";
import { cleanup, screen, within } from "@testing-library/react";
import { renderWithI18n } from "../../../../test/i18n";
import { GraphCurrentSummary } from "./graph-current-summary";

afterEach(cleanup);
const execution = { total: 2, unknown: 0, categories: { running: [], blocked: ["current"], review: [] }, focusIssueIds: new Set(["current"]), ancestorIssueIds: new Set<string>() };
it.each([
  ["en", "Current window"], ["zh-Hans", "当前窗口"], ["zh-Hant", "目前視窗"], ["es", "Ventana actual"],
  ["fr", "Fenêtre actuelle"], ["ja", "現在の表示範囲"], ["ko", "현재 표시 범위"], ["ru", "Текущее окно"],
] as const)("visibly limits the statistics to the current window in %s", (locale, scope) => {
  renderWithI18n(<GraphCurrentSummary title="Project" execution={execution} available locate={vi.fn()}/>, { locale });
  expect(within(screen.getByRole("group")).getByText(scope)).toBeVisible();
});
it("shows unavailable instead of cached numbers or zero when the caller window is unusable", () => {
  renderWithI18n(<GraphCurrentSummary title="Project" execution={execution} available={false} locate={vi.fn()}/>);
  const summary = within(screen.getByRole("group"));
  expect(summary.getByText("Current window")).toBeVisible();
  expect(summary.getByText("Summary unavailable")).toBeVisible();
  expect(summary.queryByRole("button")).not.toBeInTheDocument();
  expect(summary.queryByText(/Blocked [01]/)).not.toBeInTheDocument();
});
