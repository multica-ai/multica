import { screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Issue } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { IssueChildProgress } from "./issue-child-progress";

describe("IssueChildProgress", () => {
  it.each([
    { status: "done" },
    { status: "shipped", status_category: "done" },
  ] satisfies Pick<Issue, "status" | "status_category">[])(
    "shows remaining children for a parent in $status",
    (issue) => {
      renderWithI18n(<IssueChildProgress issue={issue} progress={{ done: 1, total: 3 }} />);
      expect(screen.getByText("2 sub-issues still unfinished")).toBeInTheDocument();
      expect(screen.queryByText("1/3")).not.toBeInTheDocument();
    },
  );

  it.each([
    { status: "in_review" },
    { status: "todo" },
    { status: "in_progress" },
    { status: "blocked" },
    { status: "cancelled" },
    { status: "review_custom", status_category: "started" },
    { status: "dropped", status_category: "closed" },
  ] satisfies Pick<Issue, "status" | "status_category">[])(
    "keeps ordinary progress for a parent in $status",
    (issue) => {
      renderWithI18n(<IssueChildProgress issue={issue} progress={{ done: 0, total: 1 }} />);
      expect(screen.getByText("0/1")).toBeInTheDocument();
      expect(screen.queryByText(/still unfinished/)).not.toBeInTheDocument();
    },
  );

  it("restores ordinary progress when a reopened child closes again", () => {
    const issue = { status: "done" };
    const { rerender } = renderWithI18n(
      <IssueChildProgress issue={issue} progress={{ done: 1, total: 1 }} />,
    );
    expect(screen.getByText("1/1")).toBeInTheDocument();
    rerender(<IssueChildProgress issue={issue} progress={{ done: 0, total: 1 }} />);
    expect(screen.getByText("1 sub-issue still unfinished")).toBeInTheDocument();
    rerender(<IssueChildProgress issue={issue} progress={{ done: 1, total: 1 }} />);
    expect(screen.getByText("1/1")).toBeInTheDocument();
    expect(screen.queryByText(/still unfinished/)).not.toBeInTheDocument();
  });
});
