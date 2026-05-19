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

function renderRoute(id: string, client?: QueryClient) {
  const qc =
    client ??
    new QueryClient({
      // Production defaults from createQueryClient(). We mirror the prod
      // QueryClient (retry: 1, staleTime: Infinity) so the per-query
      // overrides in CerebroIssueDetailRoute get exercised exactly as
      // they will at runtime.
      defaultOptions: { queries: { retry: 1, staleTime: Infinity } },
    });
  const ui = render(
    <QueryClientProvider client={qc}>
      <CerebroIssueDetailRoute id={id} />
    </QueryClientProvider>,
  );
  return { ...ui, qc };
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

  it("does NOT retry on 404 even when the QueryClient default is retry: 1", async () => {
    // Reproduces the QA scenario where the production QueryClient has
    // retry: 1 by default. Without the explicit per-query retry: false
    // override, the gate would fire two parent requests on a 404.
    mockGetIssue.mockRejectedValue(
      new ApiError("not found", 404, "Not Found"),
    );

    renderRoute("private-issue-id");

    await waitFor(() =>
      expect(screen.getByTestId("issue-detail-not-found")).toBeInTheDocument(),
    );
    expect(mockGetIssue).toHaveBeenCalledTimes(1);
  });

  it("reuses the cached 404 across remounts — only one parent fetch per visit", async () => {
    // staleTime: Infinity on the gate's useQuery means a remount within
    // the same QueryClient (back/forward navigation in the SPA) hits
    // cache instead of refetching.
    mockGetIssue.mockRejectedValue(
      new ApiError("not found", 404, "Not Found"),
    );

    const qc = new QueryClient({
      defaultOptions: { queries: { retry: 1, staleTime: Infinity } },
    });

    const { unmount } = renderRoute("private-issue-id", qc);
    await waitFor(() =>
      expect(screen.getByTestId("issue-detail-not-found")).toBeInTheDocument(),
    );
    expect(mockGetIssue).toHaveBeenCalledTimes(1);

    unmount();

    // Remount the same route id against the same QueryClient — simulates
    // browser-back-then-forward to the same private issue URL within
    // gcTime. No additional parent fetch should fire.
    renderRoute("private-issue-id", qc);
    await waitFor(() =>
      expect(screen.getByTestId("issue-detail-not-found")).toBeInTheDocument(),
    );
    expect(mockGetIssue).toHaveBeenCalledTimes(1);
  });
});
