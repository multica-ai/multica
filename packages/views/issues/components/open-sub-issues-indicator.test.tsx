import { screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Issue } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { OpenSubIssuesIndicator } from "./open-sub-issues-indicator";

describe("OpenSubIssuesIndicator", () => {
  it.each([
    { status: "in_review" },
    { status: "done" },
    { status: "shipped", status_category: "done" },
  ] satisfies Pick<Issue, "status" | "status_category">[])(
    "shows the open count for a parent in $status",
    (issue) => {
      renderWithI18n(
        <OpenSubIssuesIndicator issue={issue} childProgress={{ done: 1, total: 3 }} />,
      );
      expect(screen.getByText("2 open sub-issues")).toBeInTheDocument();
    },
  );

  it.each(["todo", "in_progress", "blocked", "cancelled"])(
    "omits the indicator for a parent in %s",
    (status) => {
      const { container } = renderWithI18n(
        <OpenSubIssuesIndicator issue={{ status }} childProgress={{ done: 0, total: 1 }} />,
      );
      expect(container).toBeEmptyDOMElement();
    },
  );

  it("updates as a child reopens and closes again", () => {
    const issue = { status: "in_review" };
    const { container, rerender } = renderWithI18n(<OpenSubIssuesIndicator issue={issue} />);
    expect(container).toBeEmptyDOMElement();

    rerender(<OpenSubIssuesIndicator issue={issue} childProgress={{ done: 1, total: 1 }} />);
    expect(container).toBeEmptyDOMElement();

    rerender(<OpenSubIssuesIndicator issue={issue} childProgress={{ done: 0, total: 1 }} />);
    expect(screen.getByText("1 open sub-issue")).toBeInTheDocument();

    rerender(<OpenSubIssuesIndicator issue={issue} childProgress={{ done: 1, total: 1 }} />);
    expect(container).toBeEmptyDOMElement();
  });
});
