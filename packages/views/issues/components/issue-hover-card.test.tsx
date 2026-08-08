import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi, beforeEach } from "vitest";
import { useQuery } from "@tanstack/react-query";
import { IssueHoverCard } from "./issue-hover-card";

vi.mock("@tanstack/react-query", () => ({
  useQuery: vi.fn(),
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "workspace-1",
}));

vi.mock("@multica/core/issues/queries", () => ({
  issueDetailOptions: (_workspaceId: string, issueId: string) => ({
    queryKey: ["issue", issueId],
  }),
}));

vi.mock("./status-icon", () => ({
  StatusIcon: () => <svg data-testid="status-icon" />,
}));

const mockUseQuery = vi.mocked(useQuery);

describe("IssueHoverCard", () => {
  beforeEach(() => {
    mockUseQuery.mockReset();
    mockUseQuery.mockReturnValue({
      data: {
        id: "issue-1",
        identifier: "MUL-3405",
        title: "A very long issue title that the compact chip hides",
        status: "todo",
      },
    } as ReturnType<typeof useQuery>);
  });

  it("does not fetch issue detail until the card opens", () => {
    render(
      <IssueHoverCard issueId="issue-1" delay={0}>
        <span>MUL-3405</span>
      </IssueHoverCard>,
    );

    // Assert the trigger actually rendered BEFORE asserting the absent fetch.
    // Without this, a component that throws or renders nothing would satisfy
    // the deferred-fetch assertion and the guarantee would be untested.
    expect(screen.getByText("MUL-3405")).toBeInTheDocument();
    expect(mockUseQuery).not.toHaveBeenCalled();
  });

  it("reveals the full title on hover", async () => {
    const user = userEvent.setup();
    render(
      <IssueHoverCard issueId="issue-1" delay={0}>
        <span>MUL-3405</span>
      </IssueHoverCard>,
    );

    await user.hover(screen.getByText("MUL-3405"));

    expect(
      await screen.findByText("A very long issue title that the compact chip hides"),
    ).toBeInTheDocument();
    expect(mockUseQuery).toHaveBeenCalled();
  });
});
