import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ApiError } from "@multica/core/api";

const { mockGetIssue, mockIssueDetailMounted } = vi.hoisted(() => ({
  mockGetIssue: vi.fn<(id: string) => Promise<unknown>>(),
  mockIssueDetailMounted: vi.fn(),
}));

vi.mock("@multica/core/api", async () => {
  const actual =
    await vi.importActual<typeof import("@multica/core/api")>(
      "@multica/core/api",
    );
  return {
    ...actual,
    api: { getIssue: (id: string) => mockGetIssue(id) },
  };
});

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({ issues: () => "/jeh/issues" }),
}));

vi.mock("@multica/views/navigation", () => ({
  useNavigation: () => ({ push: vi.fn(), replace: vi.fn() }),
}));

vi.mock("@multica/views/i18n", () => ({
  useT: () => ({
    t: (fn: (root: { detail: { not_found: string; back_to_issues: string } }) => string) =>
      fn({
        detail: {
          not_found: "This issue does not exist or has been deleted in this workspace.",
          back_to_issues: "Back to issues",
        },
      }),
  }),
}));

vi.mock("@multica/views/issues/components", () => ({
  IssueDetail: ({ issueId }: { issueId: string }) => {
    mockIssueDetailMounted(issueId);
    return <div data-testid="issue-detail">issue: {issueId}</div>;
  },
}));

import { CerebroIssueDetailRoute } from "./cerebro-issue-detail-route";

function renderRoute(id: string) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  });
  return render(
    <QueryClientProvider client={client}>
      <CerebroIssueDetailRoute id={id} />
    </QueryClientProvider>,
  );
}

describe("<CerebroIssueDetailRoute />", () => {
  beforeEach(() => {
    mockGetIssue.mockReset();
    mockIssueDetailMounted.mockReset();
  });

  it("renders a stable 404 state when /api/issues/<id> returns 404 and never mounts IssueDetail", async () => {
    mockGetIssue.mockRejectedValue(
      new ApiError("not found", 404, "Not Found"),
    );

    renderRoute("private-issue-id");

    await waitFor(() =>
      expect(screen.getByTestId("issue-detail-not-found")).toBeInTheDocument(),
    );
    expect(mockGetIssue).toHaveBeenCalledTimes(1);
    expect(mockIssueDetailMounted).not.toHaveBeenCalled();
  });

  it("does not retry on other 4xx (403) — also short-circuits to NotFound without crashing", async () => {
    mockGetIssue.mockRejectedValue(
      new ApiError("forbidden", 403, "Forbidden"),
    );

    renderRoute("forbidden-id");

    await waitFor(() =>
      expect(screen.getByTestId("issue-detail-not-found")).toBeInTheDocument(),
    );
    expect(mockGetIssue).toHaveBeenCalledTimes(1);
    expect(mockIssueDetailMounted).not.toHaveBeenCalled();
  });

  it("mounts IssueDetail once the issue resolves", async () => {
    mockGetIssue.mockResolvedValue({ id: "ok-id", title: "Hello" });

    renderRoute("ok-id");

    await waitFor(() =>
      expect(screen.getByTestId("issue-detail")).toBeInTheDocument(),
    );
    expect(mockIssueDetailMounted).toHaveBeenCalledWith("ok-id");
  });
});
