// @vitest-environment jsdom
import { cleanup, screen } from "@testing-library/react";
import { afterEach, expect, it } from "vitest";
import type { AgentTask } from "@multica/core/types";
import { renderWithI18n } from "../test/i18n";
import { TaskUsageCoverage } from "./task-usage-coverage";

afterEach(cleanup);

// Source combinations belong to usage-coverage.test.ts; these pin the
// no-counter affordance, localization and keyboard-accessible compact hint.
it("shows explicit no-statistics evidence without inventing a token figure", () => {
  renderWithI18n(<TaskUsageCoverage task={{ usage_sources: ["none"] } as AgentTask} />);
  expect(screen.getByText("Usage unavailable")).toBeInTheDocument();
  expect(screen.getByLabelText(/does not mean the run used zero tokens/)).toHaveAttribute("tabindex", "0");
});

it("localizes the compact partial-usage hint", () => {
  renderWithI18n(<TaskUsageCoverage task={{ usage_sources: ["assistant_fallback"] } as AgentTask} compact />, { locale: "zh-Hans" });
  expect(screen.getByLabelText(/^部分统计：|^部分统计:/)).toHaveAttribute("tabindex", "0");
});
