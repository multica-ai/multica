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

vi.mock("@multica/core/issues/queries", () => ({
  issueListOptions: () => ({ queryKey: ["issues"] }),
  issueDetailOptions: (_workspaceId: string, issueId: string) => ({
    queryKey: ["issue", issueId],
  }),
}));

vi.mock("./status-icon", () => ({
  StatusIcon: ({ className }: { className?: string }) => (
    <svg data-testid="status-icon" className={className} />
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
          ],
        } as ReturnType<typeof useQuery>;
      }
      return { data: undefined } as ReturnType<typeof useQuery>;
    });
  });

  it("caps the chip to its parent container and truncates the title", () => {
    render(<IssueChip issueId="issue-1" />);

    const chip = screen.getByText("MUL-3405").closest(".issue-mention");
    expect(chip).toHaveClass("min-w-0", "max-w-full");
    expect(screen.getByText("A very long issue title that should stay inside a narrow chat bubble"))
      .toHaveClass("min-w-0", "truncate");
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

  it("renders status icon, identifier and title in full mode", () => {
    render(<IssueChip issueId="issue-1" variant="full" />);

    expect(screen.getByTestId("status-icon")).toBeInTheDocument();
    expect(screen.getByText("MUL-3405")).toBeInTheDocument();
    expect(
      screen.getByText("A very long issue title that should stay inside a narrow chat bubble"),
    ).toBeInTheDocument();
  });

  it("drops the title but keeps the boxed status icon in compact mode", () => {
    render(<IssueChip issueId="issue-1" variant="compact" />);

    expect(screen.getByTestId("status-icon")).toBeInTheDocument();
    expect(screen.getByText("MUL-3405")).toBeInTheDocument();
    expect(
      screen.queryByText("A very long issue title that should stay inside a narrow chat bubble"),
    ).not.toBeInTheDocument();
    expect(screen.getByText("MUL-3405").closest(".issue-mention")).toHaveClass("border");
  });

  it("renders bare prose-sized text with no box or status icon in plain mode", () => {
    render(<IssueChip issueId="issue-1" variant="plain" />);

    expect(screen.queryByTestId("status-icon")).not.toBeInTheDocument();
    expect(
      screen.queryByText("A very long issue title that should stay inside a narrow chat bubble"),
    ).not.toBeInTheDocument();

    const mention = screen.getByText("MUL-3405");
    expect(mention).toHaveClass("issue-mention", "text-primary");
    expect(mention).not.toHaveClass("border", "text-caption");
  });

  it("renders the unresolved fallback label in each variant", () => {
    const { unmount } = render(<IssueChip issueId="missing" fallbackLabel="MUL-77" variant="compact" />);
    expect(screen.getByText("MUL-77")).toHaveClass("truncate");
    unmount();

    render(<IssueChip issueId="missing" fallbackLabel="MUL-77" variant="plain" />);
    const plain = screen.getByText("MUL-77");
    expect(plain).toHaveClass("issue-mention");
    expect(plain).not.toHaveClass("border");
  });
});
