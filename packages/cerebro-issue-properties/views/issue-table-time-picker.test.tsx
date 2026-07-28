// @vitest-environment jsdom
import "@testing-library/jest-dom/vitest";
import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { IssueTableTimePicker } from "./issue-table-time-picker";

const mocks = vi.hoisted(() => ({ featureEnabled: true }));

vi.mock("@multica/cerebro-feature-flags", () => ({
  useFeatureFlag: () => mocks.featureEnabled,
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "workspace-1",
}));

vi.mock("@multica/cerebro-issue-datetime/views", () => ({
  IssueTimePicker: ({ kind }: { kind: string }) => (
    <span data-testid={`time-picker-${kind}`} />
  ),
}));

describe("IssueTableTimePicker", () => {
  afterEach(cleanup);

  beforeEach(() => {
    mocks.featureEnabled = true;
  });

  it("shows the time picker below a populated table date when the feature is enabled", () => {
    render(
      <IssueTableTimePicker
        issueId="issue-1"
        kind="start"
        date="2026-07-18"
      />,
    );

    expect(screen.getByTestId("time-picker-start")).toBeInTheDocument();
  });

  it("hides the time picker when the feature is disabled or the date is empty", () => {
    mocks.featureEnabled = false;
    const { rerender } = render(
      <IssueTableTimePicker
        issueId="issue-1"
        kind="start"
        date="2026-07-18"
      />,
    );

    expect(screen.queryByTestId("time-picker-start")).not.toBeInTheDocument();

    mocks.featureEnabled = true;
    rerender(
      <IssueTableTimePicker issueId="issue-1" kind="start" date={null} />,
    );

    expect(screen.queryByTestId("time-picker-start")).not.toBeInTheDocument();
  });
});
