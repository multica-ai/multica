import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import type { ListGitHubQueuedPullRequestsResponse } from "@multica/core/types";
import enCommon from "../../locales/en/common.json";
import enIssues from "../../locales/en/issues.json";

const TEST_RESOURCES = { en: { common: enCommon, issues: enIssues } };

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("@multica/core/github", () => ({
  queuedPullRequestsOptions: (wsId: string) => ({
    queryKey: ["github", wsId, "pull-requests", "queued"],
    queryFn: async () => mockResponse,
    enabled: !!wsId,
  }),
}));

import { IssueMergeQueueIndicator } from "./issue-merge-queue-indicator";

let mockResponse: ListGitHubQueuedPullRequestsResponse = { queued_pull_requests: [] };

function renderIndicator(issueId = "issue-1") {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <I18nProvider resources={TEST_RESOURCES} locale="en">
        <IssueMergeQueueIndicator issueId={issueId} />
      </I18nProvider>
    </QueryClientProvider>,
  );
}

describe("IssueMergeQueueIndicator", () => {
  it("marks a card whose PR is waiting in the queue", async () => {
    mockResponse = {
      queued_pull_requests: [{ issue_id: "issue-1", merge_queue_state: "queued" }],
    };
    renderIndicator();
    expect(await screen.findByText("In merge queue")).toBeInTheDocument();
  });

  it("distinguishes an entry the queue cannot merge", async () => {
    mockResponse = {
      queued_pull_requests: [{ issue_id: "issue-1", merge_queue_state: "unmergeable" }],
    };
    renderIndicator();
    expect(await screen.findByText("Merge queue blocked")).toBeInTheDocument();
    expect(screen.queryByText("In merge queue")).not.toBeInTheDocument();
  });

  it("treats every other entry state as waiting, not blocked", async () => {
    mockResponse = {
      queued_pull_requests: [{ issue_id: "issue-1", merge_queue_state: "awaiting_checks" }],
    };
    renderIndicator();
    expect(await screen.findByText("In merge queue")).toBeInTheDocument();
  });

  it("renders nothing for an issue that is not in the response", async () => {
    // The response is the complete set of queued issues, so absence is a
    // definite "not queued" — the card must stay clean, not show a placeholder.
    mockResponse = {
      queued_pull_requests: [{ issue_id: "other-issue", merge_queue_state: "queued" }],
    };
    const { container } = renderIndicator("issue-1");
    await Promise.resolve();
    expect(container).toBeEmptyDOMElement();
  });

  it("renders nothing when no PR is queued at all", async () => {
    mockResponse = { queued_pull_requests: [] };
    const { container } = renderIndicator();
    await Promise.resolve();
    expect(container).toBeEmptyDOMElement();
  });
});
