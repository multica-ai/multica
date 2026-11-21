import { act, cleanup, render, screen, waitFor } from "@testing-library/react";
import { QueryClientProvider, focusManager } from "@tanstack/react-query";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { api, ApiError } from "@multica/core/api";
import { createQueryClient } from "@multica/core/query-client";
import { issueKeys } from "@multica/core/issues/queries";
import type { Issue, ListIssuesCache } from "@multica/core/types";
import { IssueChip } from "./issue-chip";

vi.mock("@multica/core/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/api")>()),
  api: { getIssue: vi.fn() },
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "workspace-1",
}));

vi.mock("@multica/core/issue-statuses/hooks", () => ({
  useIssueStatuses: () => ({ colorOf: () => null, iconOf: () => null }),
}));

vi.mock("./status-icon", () => ({
  StatusIcon: () => <svg data-testid="status-icon" />,
}));

const issue = {
  id: "11111111-1111-4111-8111-111111111111",
  workspace_id: "workspace-1",
  identifier: "MUL-7",
  title: "Newly created issue",
  status: "todo",
} as Issue;

describe("IssueChip detail fallback on window focus", () => {
  let queryClient: ReturnType<typeof createQueryClient>;

  beforeEach(() => {
    queryClient = createQueryClient();
    queryClient.setDefaultOptions({
      queries: { ...queryClient.getDefaultOptions().queries, retryDelay: 0 },
    });
    // An empty list cache makes the chip miss the list and use the detail query.
    queryClient.setQueryData<ListIssuesCache>(
      issueKeys.listSorted(issue.workspace_id),
      { byStatus: { unstarted: { issues: [], total: 0 } } },
    );
    vi.mocked(api.getIssue).mockReset();
    focusManager.setFocused(true);
  });

  afterEach(() => {
    cleanup();
    queryClient.clear();
    focusManager.setFocused(undefined);
  });

  function renderChip() {
    render(
      <QueryClientProvider client={queryClient}>
        <IssueChip issueId={issue.id} fallbackLabel={issue.identifier} />
      </QueryClientProvider>,
    );
  }

  function blurAndRefocus() {
    act(() => focusManager.setFocused(false));
    act(() => focusManager.setFocused(true));
  }

  it("recovers an exhausted detail fetch when the window regains focus", async () => {
    vi.mocked(api.getIssue).mockRejectedValue(
      new ApiError("Unavailable", 503, "Unavailable"),
    );
    renderChip();
    await waitFor(() => {
      expect(
        queryClient.getQueryState(issueKeys.detail(issue.workspace_id, issue.id))?.status,
      ).toBe("error");
    });
    expect(screen.queryByText(issue.title)).not.toBeInTheDocument();

    vi.mocked(api.getIssue).mockResolvedValue(issue);
    blurAndRefocus();

    await screen.findByText(issue.title);
    expect(screen.getByTestId("status-icon")).toBeInTheDocument();
  });

  it("does not refetch a missing issue on repeated focus", async () => {
    vi.mocked(api.getIssue).mockRejectedValue(
      new ApiError("Not found", 404, "NotFound"),
    );
    renderChip();
    await waitFor(() => {
      expect(
        queryClient.getQueryState(issueKeys.detail(issue.workspace_id, issue.id))?.status,
      ).toBe("error");
    });
    expect(api.getIssue).toHaveBeenCalledTimes(2);

    for (let i = 0; i < 2; i++) {
      blurAndRefocus();
      await act(async () => {
        await new Promise((resolve) => setTimeout(resolve, 0));
      });
      expect(api.getIssue).toHaveBeenCalledTimes(2);
    }

    expect(screen.getByText(issue.identifier)).toBeInTheDocument();
    expect(screen.queryByText(issue.title)).not.toBeInTheDocument();
    expect(screen.queryByTestId("status-icon")).not.toBeInTheDocument();
  });

  it("does not refetch an already hydrated chip on focus", async () => {
    vi.mocked(api.getIssue).mockResolvedValue(issue);
    renderChip();
    await screen.findByText(issue.title);

    blurAndRefocus();
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 0));
    });

    expect(api.getIssue).toHaveBeenCalledTimes(1);
  });
});
