import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useQuery } from "@tanstack/react-query";
import { IssueChip } from "./issue-chip";

vi.mock("@tanstack/react-query", () => ({
  useQuery: vi.fn(),
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "workspace-1",
}));

vi.mock("@multica/core/issue-statuses/hooks", () => ({
  useIssueStatuses: () => ({
    colorOf: (status: string) =>
      status === "awaiting_response" ? "#f97316" : null,
  }),
}));

vi.mock("@multica/core/issues/queries", () => ({
  issueListOptions: () => ({ queryKey: ["issues"] }),
  issueDetailOptions: (_workspaceId: string, issueId: string) => ({
    queryKey: ["issue", issueId],
  }),
}));

vi.mock("./status-icon", () => ({
  StatusIcon: ({
    status,
    category,
    color,
    className,
  }: {
    status: string;
    category?: string;
    color?: string | null;
    className?: string;
  }) => (
    <svg
      data-testid="status-icon"
      data-status={status}
      data-category={category}
      data-color={color}
      className={className}
    />
  ),
}));

const mockUseQuery = vi.mocked(useQuery);

describe("IssueChip", () => {
  beforeEach(() => {
    mockUseQuery.mockImplementation((options: { queryKey?: readonly unknown[] }) => {
      if (options.queryKey?.[0] === "issues") {
        return {
          data: [
            {
              id: "issue-1",
              identifier: "MUL-3405",
              title: "A very long issue title that should stay inside a narrow chat bubble",
              status: "todo",
            },
            {
              id: "issue-2",
              identifier: "MUL-6956",
              title: "Custom status color in Chat",
              status: "awaiting_response",
              status_category: "in_review",
            },
          ],
        } as ReturnType<typeof useQuery>;
      }
      return { data: undefined } as ReturnType<typeof useQuery>;
    });
  });

  it("truncates unresolved fallback labels inside the chip width", () => {
    render(
      <IssueChip
        issueId="missing-issue"
        fallbackLabel="MUL-999999999999999999999999999999999"
      />,
    );

    expect(screen.getByText("MUL-999999999999999999999999999999999"))
      .toHaveClass("min-w-0", "truncate");
  });

  it("paints a custom status with its catalog color instead of the category token", () => {
    render(<IssueChip issueId="issue-2" />);

    expect(screen.getByTestId("status-icon")).toHaveAttribute(
      "data-status",
      "awaiting_response",
    );
    expect(screen.getByTestId("status-icon")).toHaveAttribute(
      "data-category",
      "in_review",
    );
    expect(screen.getByTestId("status-icon")).toHaveAttribute(
      "data-color",
      "#f97316",
    );
  });
});
