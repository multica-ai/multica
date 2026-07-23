import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import type { GitHubPullRequest } from "@multica/core/types";
import enCommon from "../../locales/en/common.json";
import enIssues from "../../locales/en/issues.json";

const TEST_RESOURCES = { en: { common: enCommon, issues: enIssues } };

vi.mock("@multica/core/github/queries", async () => {
  const actual = await vi.importActual<typeof import("@multica/core/github/queries")>(
    "@multica/core/github/queries",
  );
  return {
    ...actual,
    issuePullRequestsOptions: (issueId: string) => ({
      queryKey: ["github", "pull-requests", issueId],
      queryFn: async () => ({ pull_requests: mockPRs }),
      enabled: !!issueId,
    }),
  };
});

import { PullRequestList } from "./pull-request-list";

let mockPRs: GitHubPullRequest[] = [];

function makePR(overrides: Partial<GitHubPullRequest> = {}): GitHubPullRequest {
  return {
    id: "pr-1",
    workspace_id: "ws-1",
    repo_owner: "acme",
    repo_name: "widget",
    number: 1,
    title: "Test PR",
    state: "open",
    html_url: "https://example.test/pr/1",
    branch: "feat/x",
    author_login: "octocat",
    author_avatar_url: null,
    merged_at: null,
    closed_at: null,
    pr_created_at: "2026-01-01T00:00:00Z",
    pr_updated_at: "2026-01-01T00:00:00Z",
    mergeable_state: null,
    checks_conclusion: null,
    checks_passed: 0,
    checks_failed: 0,
    checks_pending: 0,
    additions: 0,
    deletions: 0,
    changed_files: 0,
    ...overrides,
  };
}

function renderList() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <I18nProvider resources={TEST_RESOURCES} locale="en">
        <PullRequestList issueId="issue-1" />
      </I18nProvider>
    </QueryClientProvider>,
  );
}

async function waitForRender() {
  return screen.findAllByRole("link");
}

describe("PullRequestList sidebar rows", () => {
  it("uses the sidebar list-row surface instead of a card surface", async () => {
    mockPRs = [makePR({ title: "Visual row" })];
    renderList();
    await waitForRender();
    const row = screen.getByTestId("pull-request-row");
    expect(row).toHaveClass("rounded-md", "-mx-2", "hover:bg-accent/50");
    expect(row).not.toHaveClass("rounded-lg", "border", "bg-card");
  });

  // MUL-5180 fail-only contract: completed-suite webhooks cannot vouch for
  // "everything passed" or "still running", so the row renders no CI element
  // at all unless something needs attention. No text, no progress strip.
  it("renders no CI element when observed checks passed", async () => {
    mockPRs = [makePR({ checks_passed: 3, checks_conclusion: "passed" })];
    renderList();
    await waitForRender();
    expect(screen.queryByTestId("pull-request-alert")).not.toBeInTheDocument();
    expect(screen.queryByText(/checks/i)).not.toBeInTheDocument();
    expect(screen.getByTestId("pull-request-row").querySelector(".w-12")).toBeNull();
  });

  it("never claims that ALL checks passed", async () => {
    mockPRs = [makePR({ checks_passed: 1 })];
    renderList();
    await waitForRender();
    expect(screen.queryByText(/all checks passed/i)).not.toBeInTheDocument();
  });

  it("renders no CI element while only pending suites are observed", async () => {
    mockPRs = [makePR({ checks_pending: 2, checks_passed: 1 })];
    renderList();
    await waitForRender();
    expect(screen.queryByTestId("pull-request-alert")).not.toBeInTheDocument();
  });

  it("renders no CI element when nothing has reported and there is no mergeable verdict", async () => {
    mockPRs = [makePR()];
    renderList();
    await waitForRender();
    expect(screen.queryByTestId("pull-request-alert")).not.toBeInTheDocument();
    expect(screen.queryByText("Checks haven't reported yet")).not.toBeInTheDocument();
  });

  it("renders nothing special for ready-to-merge (clean, no suites)", async () => {
    mockPRs = [makePR({ mergeable_state: "clean" })];
    renderList();
    await waitForRender();
    expect(screen.queryByTestId("pull-request-alert")).not.toBeInTheDocument();
    expect(screen.queryByText("Ready to merge")).not.toBeInTheDocument();
  });

  it("renders a red Checks-failed alert when any observed suite failed", async () => {
    mockPRs = [makePR({ checks_failed: 1, checks_passed: 5 })];
    renderList();
    await waitForRender();
    const alert = screen.getByTestId("pull-request-alert");
    expect(alert).toHaveAttribute("data-alert-kind", "checks_failed");
    expect(alert).toHaveClass("text-rose-600");
    expect(alert).toHaveTextContent("Checks failed");
  });

  it("renders a red conflicts alert when mergeable_state=dirty", async () => {
    mockPRs = [makePR({ mergeable_state: "dirty" })];
    renderList();
    await waitForRender();
    const alert = screen.getByTestId("pull-request-alert");
    expect(alert).toHaveAttribute("data-alert-kind", "conflicts");
    expect(alert).toHaveClass("text-rose-600");
    expect(alert).toHaveTextContent("Has merge conflicts");
  });

  it("shows failure and conflict alerts together — one must not mask the other", async () => {
    mockPRs = [makePR({ mergeable_state: "dirty", checks_failed: 2 })];
    renderList();
    await waitForRender();
    const alerts = screen.getAllByTestId("pull-request-alert");
    expect(alerts.map((a) => a.getAttribute("data-alert-kind"))).toEqual([
      "checks_failed",
      "conflicts",
    ]);
  });

  it("suppresses alerts on merged PRs — the state label already tells the story", async () => {
    mockPRs = [
      makePR({
        state: "merged",
        mergeable_state: "dirty",
        checks_conclusion: "failed",
        checks_failed: 5,
      }),
    ];
    renderList();
    await waitForRender();
    expect(screen.getByText(/Merged/)).toBeInTheDocument();
    expect(screen.queryByTestId("pull-request-alert")).not.toBeInTheDocument();
  });

  it("suppresses alerts and status text on closed PRs", async () => {
    mockPRs = [
      makePR({
        state: "closed",
        mergeable_state: "clean",
        checks_conclusion: "passed",
        checks_passed: 3,
      }),
    ];
    renderList();
    await waitForRender();
    expect(screen.getByText(/Closed/)).toBeInTheDocument();
    expect(screen.queryByTestId("pull-request-alert")).not.toBeInTheDocument();
    expect(screen.queryByText("Closed without merging")).not.toBeInTheDocument();
  });

  it("keeps the failure alert on draft PRs without a Draft prefix (state label covers it)", async () => {
    mockPRs = [makePR({ state: "draft", checks_failed: 1 })];
    renderList();
    await waitForRender();
    const alert = screen.getByTestId("pull-request-alert");
    expect(alert.textContent).toBe("Checks failed");
    expect(screen.queryByText("Draft · Checks failed")).not.toBeInTheDocument();
  });

  it("hides stats row when all stats are 0 (legacy backend)", async () => {
    mockPRs = [makePR()];
    renderList();
    await waitForRender();
    expect(screen.queryByText(/files?$/)).not.toBeInTheDocument();
    expect(screen.queryByText(/^\+0/)).not.toBeInTheDocument();
  });

  it("shows stats row with additions / deletions / file count when present", async () => {
    mockPRs = [makePR({ additions: 437, deletions: 6, changed_files: 6 })];
    renderList();
    await waitForRender();
    expect(screen.getByText("+437")).toBeInTheDocument();
    expect(screen.getByText("−6")).toBeInTheDocument();
    expect(screen.getByText("6 files")).toBeInTheDocument();
  });

  it("uses singular file copy when changed_files=1", async () => {
    mockPRs = [makePR({ additions: 1, changed_files: 1 })];
    renderList();
    await waitForRender();
    expect(screen.getByText("1 file")).toBeInTheDocument();
  });

  it("collapses extra PR rows past the visible limit behind Show more toggle", async () => {
    mockPRs = [
      makePR({ id: "a", number: 1, title: "PR-A" }),
      makePR({ id: "b", number: 2, title: "PR-B" }),
      makePR({ id: "c", number: 3, title: "PR-C" }),
      makePR({ id: "d", number: 4, title: "PR-D" }),
      makePR({ id: "e", number: 5, title: "PR-E" }),
    ];
    renderList();
    await waitForRender();
    expect(screen.getByText("PR-A")).toBeInTheDocument();
    expect(screen.getByText("PR-B")).toBeInTheDocument();
    expect(screen.getByText("PR-C")).toBeInTheDocument();
    expect(screen.queryByText("PR-D")).not.toBeInTheDocument();
    expect(screen.queryByText("PR-E")).not.toBeInTheDocument();
    expect(screen.getByText("Show 2 more")).toBeInTheDocument();
  });

  it("collapses to 3 rows + hidden tail when count == threshold", async () => {
    mockPRs = [
      makePR({ id: "a", number: 1, title: "PR-A" }),
      makePR({ id: "b", number: 2, title: "PR-B" }),
      makePR({ id: "c", number: 3, title: "PR-C" }),
      makePR({ id: "d", number: 4, title: "PR-D" }),
    ];
    renderList();
    await waitForRender();
    expect(screen.getByText("PR-A")).toBeInTheDocument();
    expect(screen.getByText("PR-B")).toBeInTheDocument();
    expect(screen.getByText("PR-C")).toBeInTheDocument();
    expect(screen.queryByText("PR-D")).not.toBeInTheDocument();
    expect(screen.getByText("Show 1 more")).toBeInTheDocument();
  });
});
