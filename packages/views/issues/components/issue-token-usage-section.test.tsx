// @vitest-environment jsdom

import type { ReactNode } from "react";
import { describe, it, expect } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import type { IssueUsageSummary } from "@multica/core/types";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enIssues from "../../locales/en/issues.json";
import { IssueTokenUsageSection } from "./issue-token-usage-section";

const TEST_RESOURCES = { en: { common: enCommon, issues: enIssues } };

function wrap(children: ReactNode) {
  return <I18nProvider locale="en" resources={TEST_RESOURCES}>{children}</I18nProvider>;
}

const USAGE: IssueUsageSummary = {
  total_input_tokens: 3000,
  total_output_tokens: 200,
  total_cache_read_tokens: 50_000,
  total_cache_write_tokens: 10_000,
  task_count: 2,
  tasks: [
    {
      task_id: "t2",
      created_at: "2026-07-08T10:00:00Z",
      comment_triggered: true,
      provider: "anthropic",
      model: "claude-sonnet-4.6",
      input_tokens: 2000,
      output_tokens: 100,
      cache_read_tokens: 30_000,
      cache_write_tokens: 5_000,
    },
    {
      task_id: "t1",
      created_at: "2026-07-08T09:00:00Z",
      comment_triggered: false,
      provider: "anthropic",
      model: "claude-sonnet-4.6",
      input_tokens: 1000,
      output_tokens: 100,
      cache_read_tokens: 20_000,
      cache_write_tokens: 5_000,
    },
  ],
};

describe("IssueTokenUsageSection", () => {
  it("renders nothing when there are no runs", () => {
    const { container } = render(wrap(<IssueTokenUsageSection usage={{ ...USAGE, task_count: 0, tasks: [] }} />));
    expect(container).toBeEmptyDOMElement();
  });

  it("shows totals and an estimated cost", () => {
    render(wrap(<IssueTokenUsageSection usage={USAGE} />));
    expect(screen.getByText("3.0k")).toBeInTheDocument(); // input total
    expect(screen.getByText("Est. cost")).toBeInTheDocument();
    // claude-sonnet-4.6: (3000*3 + 200*15 + 50000*0.3 + 10000*3.75) / 1e6 ≈ $0.0645
    expect(screen.getByText(/\$0\.06/)).toBeInTheDocument();
  });

  it("expands a per-run breakdown labelled by trigger type", () => {
    render(wrap(<IssueTokenUsageSection usage={USAGE} />));
    fireEvent.click(screen.getByText("2 runs"));
    expect(screen.getByText("Comment")).toBeInTheDocument();
    expect(screen.getByText("Assignment")).toBeInTheDocument();
  });

  it("explains cache read/write in a tooltip", () => {
    render(wrap(<IssueTokenUsageSection usage={USAGE} />));
    expect(screen.getByText("Cache").closest("[title]")).toHaveAttribute(
      "title",
      expect.stringContaining("Cache read"),
    );
  });
});
