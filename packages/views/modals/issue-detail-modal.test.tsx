import React from "react";
import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import { render, screen } from "@testing-library/react";
import { IssueDetailModal } from "./issue-detail-modal";

// Mock IssueDetail to avoid rendering its heavy children
vi.mock("../issues/components/issue-detail", () => ({
  IssueDetail: ({ issueId }: { issueId: string }) => (
    <div data-testid="mock-issue-detail">Issue ID: {issueId}</div>
  ),
}));

vi.mock("@tanstack/react-query", () => ({
  useQuery: () => ({ data: { id: "MUL-123", identifier: "MUL-123" } }),
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "workspace-1",
}));

vi.mock("@multica/core/issues/queries", () => ({
  issueDetailOptions: vi.fn(),
}));

vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({
    issueDetail: (id: string) => `/test-workspace/issues/${id}`,
    issues: () => "/test-workspace/issues",
  }),
}));

let mockPathname = "/test-workspace/issues";

vi.mock("../navigation", () => ({
  useNavigation: () => ({
    pathname: mockPathname,
    searchParams: new URLSearchParams("?view=board"),
    push: vi.fn(),
  }),
  NavigationProvider: ({ children }: { children: React.ReactNode }) => <>{children}</>,
}));

vi.mock("@multica/ui/components/ui/dialog", () => ({
  Dialog: ({ children }: { children: React.ReactNode }) => <div data-testid="mock-dialog">{children}</div>,
  DialogContent: ({ children }: { children: React.ReactNode }) => <div data-testid="mock-dialog-content">{children}</div>,
}));

describe("IssueDetailModal", () => {
  const originalPushState = window.history.pushState;
  const mockPushState = vi.fn();

  beforeEach(() => {
    mockPathname = "/test-workspace/issues";
    window.history.pushState = mockPushState;
  });

  afterEach(() => {
    window.history.pushState = originalPushState;
    mockPushState.mockClear();
  });

  it("renders the dialog and passes issueId to IssueDetail", () => {
    render(<IssueDetailModal onClose={vi.fn()} data={{ issueId: "MUL-123" }} />);

    expect(screen.getByTestId("mock-dialog")).toBeInTheDocument();
    expect(screen.getByTestId("mock-issue-detail")).toHaveTextContent("Issue ID: MUL-123");
  });

  it("updates URL on mount and restores it on unmount", () => {
    const { unmount } = render(
      <IssueDetailModal onClose={vi.fn()} data={{ issueId: "MUL-456" }} />
    );

    // Should push the issue detail URL on mount
    expect(mockPushState).toHaveBeenCalledWith(null, "", "/test-workspace/issues/MUL-456");

    unmount();

    // Should push the initial URL back on unmount
    expect(mockPushState).toHaveBeenLastCalledWith(null, "", "/test-workspace/issues?view=board");
  });

  it("closes the modal and skips restoring URL on router pathname change", () => {
    const onClose = vi.fn();
    const { rerender } = render(
      <IssueDetailModal onClose={onClose} data={{ issueId: "MUL-789" }} />
    );

    expect(mockPushState).toHaveBeenCalledWith(null, "", "/test-workspace/issues/MUL-789");

    // Simulate route navigation by changing mockPathname and rerendering
    mockPathname = "/test-workspace/settings";
    rerender(<IssueDetailModal onClose={onClose} data={{ issueId: "MUL-789" }} />);

    // onClose should have been called because navigation occurred
    expect(onClose).toHaveBeenCalled();

    // The cleanup should skip restoring the URL (mockPushState was NOT called with /test-workspace/issues?view=board)
    const initialUrlRestorations = mockPushState.mock.calls.filter(
      (call) => call[2] === "/test-workspace/issues?view=board"
    );
    expect(initialUrlRestorations).toHaveLength(0);
  });
});
