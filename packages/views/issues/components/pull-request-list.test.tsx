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

  it("renders checks-passed status when only passed counts are non-zero", async () => {
    mockPRs = [makePR({ checks_passed: 3 })];
    renderList();
    await waitForRender();
    expect(screen.getByText("Checks passed")).toBeInTheDocument();
  });

  // MUL-5180: the App holds Checks read-only, so GitHub delivers `completed`
  // suites only — we can never know whether every suite has reported. The
  // copy must therefore not claim completeness.
  it("never claims that ALL checks passed", async () => {
    mockPRs = [makePR({ checks_passed: 1 })];
    renderList();
    await waitForRender();
    expect(screen.queryByText(/all checks passed/i)).not.toBeInTheDocument();
  });

  it("renders Some-checks-failed when any failed count is non-zero", async () => {
    mockPRs = [makePR({ checks_failed: 1, checks_passed: 5 })];
    renderList();
    await waitForRender();
    expect(screen.getByText("Some checks failed")).toBeInTheDocument();
  });

  it("renders pending status when only pending suites remain", async () => {
    mockPRs = [makePR({ checks_pending: 2, checks_passed: 1 })];
    renderList();
    await waitForRender();
    expect(screen.getByText("Some checks haven't completed yet")).toBeInTheDocument();
  });

  it("renders conflicts status when mergeable_state=dirty", async () => {
    mockPRs = [makePR({ mergeable_state: "dirty" })];
    renderList();
    await waitForRender();
    expect(screen.getByText("Has merge conflicts")).toBeInTheDocument();
  });

  it("renders Ready-to-merge when mergeable=clean and no suites observed", async () => {
    mockPRs = [makePR({ mergeable_state: "clean" })];
    renderList();
    await waitForRender();
    expect(screen.getByText("Ready to merge")).toBeInTheDocument();
  });

  it("renders Merged status for merged PRs, suppressing conflict/check text", async () => {
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
    expect(screen.getByText("Merged")).toBeInTheDocument();
    expect(screen.queryByText("Has merge conflicts")).not.toBeInTheDocument();
    expect(screen.queryByText("Some checks failed")).not.toBeInTheDocument();
    expect(screen.queryByText("Conflicts")).not.toBeInTheDocument();
    expect(screen.queryByText("Checks failed")).not.toBeInTheDocument();
  });

  it("renders Closed-without-merging status for closed PRs, suppressing conflict/check badges", async () => {
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
    expect(screen.getByText("Closed without merging")).toBeInTheDocument();
    expect(screen.queryByText("Ready to merge")).not.toBeInTheDocument();
    expect(screen.queryByText("All checks passed")).not.toBeInTheDocument();
    expect(screen.queryByText("No conflicts")).not.toBeInTheDocument();
    expect(screen.queryByText("Checks passed")).not.toBeInTheDocument();
  });

  // MUL-5180: CI outcome used to render as plain muted text, visually
  // indistinguishable from the diff stats next to it. The actionable kinds
  // must carry their own color + icon; terminal/unknown kinds must not.
  it("gives failing checks a colored status treatment", async () => {
    mockPRs = [makePR({ checks_failed: 1, checks_passed: 5 })];
    renderList();
    await waitForRender();
    const status = screen.getByTestId("pull-request-status");
    expect(status).toHaveAttribute("data-status-kind", "checks_failed");
    expect(status).toHaveClass("text-rose-600");
    expect(status).toHaveTextContent("Some checks failed");
  });

  it("gives pending and passing checks their own status treatment", async () => {
    mockPRs = [makePR({ checks_pending: 2 })];
    const { unmount } = renderList();
    await waitForRender();
    expect(screen.getByTestId("pull-request-status")).toHaveClass("text-amber-600");
    unmount();

    mockPRs = [makePR({ checks_passed: 2 })];
    renderList();
    await waitForRender();
    expect(screen.getByTestId("pull-request-status")).toHaveClass("text-emerald-600");
  });

  it("gives merge conflicts a colored status treatment", async () => {
    mockPRs = [makePR({ mergeable_state: "dirty" })];
    renderList();
    await waitForRender();
    const status = screen.getByTestId("pull-request-status");
    expect(status).toHaveAttribute("data-status-kind", "conflicts");
    expect(status).toHaveClass("text-rose-600");
  });

  it("leaves terminal and unknown status kinds muted", async () => {
    mockPRs = [makePR({ state: "merged", checks_failed: 3 })];
    const { unmount } = renderList();
    await waitForRender();
    expect(screen.queryByTestId("pull-request-status")).not.toBeInTheDocument();
    expect(screen.getByText("Merged")).toBeInTheDocument();
    unmount();

    // No suite observed and no mergeable verdict — the state the card sits in
    // when the GitHub App is not subscribed to `check_suite`.
    mockPRs = [makePR()];
    renderList();
    await waitForRender();
    expect(screen.queryByTestId("pull-request-status")).not.toBeInTheDocument();
    expect(screen.getByText("Checks haven't reported yet")).toBeInTheDocument();
  });

  it("keeps the draft prefix wrapped in the colored status treatment", async () => {
    mockPRs = [makePR({ state: "draft", checks_failed: 1 })];
    renderList();
    await waitForRender();
    const status = screen.getByTestId("pull-request-status");
    expect(status).toHaveClass("text-rose-600");
    expect(status).toHaveTextContent("Draft · Some checks failed");
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
