import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import type { GitHubPullRequest, PRAutoComplete } from "@multica/core/types";
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
      queryFn: async () => ({ pull_requests: mockPRs, auto_complete: mockAutoComplete }),
      enabled: !!issueId,
    }),
  };
});

const apiMock = vi.hoisted(() => ({
  unlinkIssuePullRequest: vi.fn(),
  linkIssuePullRequest: vi.fn(),
  setIssuePRAutoComplete: vi.fn(),
}));
vi.mock("@multica/core/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/api")>()),
  api: apiMock,
}));
const toastMock = vi.hoisted(() => ({ success: vi.fn(), error: vi.fn() }));
vi.mock("sonner", () => ({ toast: toastMock }));
vi.mock("@multica/core/paths", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/paths")>()),
  useWorkspacePaths: () => ({ settings: () => "/acme/settings" }),
}));
vi.mock("../../navigation", () => ({
  AppLink: ({ href, children, className }: { href: string; children: React.ReactNode; className?: string }) => (
    <a href={href} className={className}>{children}</a>
  ),
}));

import { PullRequestList } from "./pull-request-list";

let mockPRs: GitHubPullRequest[] = [];
let mockAutoComplete: PRAutoComplete | null = null;

function makePR(overrides: Partial<GitHubPullRequest> = {}): GitHubPullRequest {
  return {
    id: "pr-1",
    provider: "github",
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
    mergeable: null,
    merge_state_status: null,
    snapshot_available: true,
    checks_rollup: null,
    checks_total: 0,
    checks_passed: 0,
    checks_failed: 0,
    checks_running: 0,
    failed_check_names: [],
    snapshot_stale: false,
    snapshot_fetched_at: null,
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
        <PullRequestList issueId="issue-1" identifier="MUL-1" />
      </I18nProvider>
    </QueryClientProvider>,
  );
}

async function waitForRender() {
  return screen.findAllByRole("link");
}

describe("PullRequestList sidebar rows", () => {

  // --- CI status element ---------------------------------------------------

  it("renders all-checks-passed only when the rollup is success", async () => {
    mockPRs = [makePR({ checks_rollup: "success", checks_total: 7 })];
    renderList();
    await waitForRender();
    expect(screen.getByText("All checks passed (7/7)")).toBeInTheDocument();
  });

  it("renders 'No checks yet' when the rollup is absent — never passed", async () => {
    // Acceptance criterion 5: absent snapshot must not read as a green build.
    mockPRs = [makePR({ checks_rollup: null, checks_passed: 5, checks_total: 5 })];
    renderList();
    await waitForRender();
    expect(screen.getByText("No checks yet")).toBeInTheDocument();
    expect(screen.queryByText(/All checks passed/)).not.toBeInTheDocument();
  });

  it("hides snapshot status when the GitHub App key is unavailable, even with old data", async () => {
    mockPRs = [
      makePR({
        snapshot_available: false,
        checks_rollup: "failure",
        checks_conclusion: "failed",
        checks_total: 2,
        checks_failed: 2,
        mergeable: "conflicting",
        merge_state_status: "dirty",
      }),
    ];
    renderList();
    await waitForRender();
    expect(screen.queryByText(/failed/)).not.toBeInTheDocument();
    expect(screen.queryByText("No checks yet")).not.toBeInTheDocument();
    expect(screen.queryByText("Has merge conflicts")).not.toBeInTheDocument();
  });

  it("renders failed count with the first failing check names", async () => {
    mockPRs = [
      makePR({
        checks_rollup: "failure",
        checks_total: 7,
        checks_failed: 2,
        failed_check_names: ["backend", "e2e"],
      }),
    ];
    renderList();
    await waitForRender();
    const badge = screen.getByText(/2\/7 failed/);
    expect(badge).toHaveTextContent("2/7 failed");
    expect(badge).toHaveTextContent("backend, e2e");
  });

  it("truncates the failing names to two and appends a +N more count", async () => {
    mockPRs = [
      makePR({
        checks_rollup: "failure",
        checks_total: 7,
        checks_failed: 4,
        failed_check_names: ["a", "b", "c", "d"],
      }),
    ];
    renderList();
    await waitForRender();
    const badge = screen.getByText(/4\/7 failed/);
    expect(badge).toHaveTextContent("4/7 failed");
    expect(badge).toHaveTextContent("a, b, +2 more");
  });

  it("renders the running count when the rollup is pending", async () => {
    mockPRs = [
      makePR({ checks_rollup: "pending", checks_total: 7, checks_passed: 5, checks_running: 2 }),
    ];
    renderList();
    await waitForRender();
    const badge = screen.getByText(/2 running/);
    expect(badge).toHaveTextContent("5/7");
    expect(badge).toHaveTextContent("2 running");
  });

  it.each([
    ["forgejo", "passed", "All checks passed (3/3)"],
    ["gitea", "pending", "2/3 · 1 running"],
    ["gitlab", "failed", "1/3 failed"],
  ] as const)(
    "preserves %s legacy %s check status",
    async (provider, conclusion, expected) => {
      mockPRs = [
        makePR({
          provider,
          snapshot_available: undefined,
          checks_rollup: undefined,
          checks_conclusion: conclusion,
          checks_total: 3,
          checks_passed: conclusion === "passed" ? 3 : 2,
          checks_failed: conclusion === "failed" ? 1 : 0,
          checks_running: conclusion === "pending" ? 1 : 0,
          checks_pending: conclusion === "pending" ? 1 : 0,
        }),
      ];
      renderList();
      await waitForRender();
      expect(screen.getByText(expected, { exact: false })).toBeInTheDocument();
    },
  );

  // --- Mergeability element ------------------------------------------------

  it("renders 'Ready to merge' only when the merge state is clean", async () => {
    mockPRs = [makePR({ merge_state_status: "clean" })];
    renderList();
    await waitForRender();
    expect(screen.getByText("Ready to merge")).toBeInTheDocument();
  });

  it("never infers 'Ready to merge' from mergeable alone", async () => {
    // Acceptance criterion 8: mergeable without a clean state shows neither.
    mockPRs = [makePR({ mergeable: "mergeable", merge_state_status: null })];
    renderList();
    await waitForRender();
    expect(screen.queryByText("Ready to merge")).not.toBeInTheDocument();
    expect(screen.queryByText("Has merge conflicts")).not.toBeInTheDocument();
  });

  it("renders 'Has merge conflicts' when mergeable is conflicting", async () => {
    mockPRs = [makePR({ mergeable: "conflicting" })];
    renderList();
    await waitForRender();
    expect(screen.getByText("Has merge conflicts")).toBeInTheDocument();
  });

  it("shows neither conflict nor ready when the merge verdict is unknown", async () => {
    // Acceptance criterion 5: unknown mergeability shows neither element.
    mockPRs = [makePR({ mergeable: "unknown", merge_state_status: "unknown" })];
    renderList();
    await waitForRender();
    expect(screen.queryByText("Has merge conflicts")).not.toBeInTheDocument();
    expect(screen.queryByText("Ready to merge")).not.toBeInTheDocument();
  });

  // --- The two elements are independent ------------------------------------

  it("shows a failed CI element and a conflict element together", async () => {
    mockPRs = [
      makePR({
        checks_rollup: "failure",
        checks_total: 7,
        checks_failed: 2,
        failed_check_names: ["backend"],
        mergeable: "conflicting",
      }),
    ];
    renderList();
    await waitForRender();
    expect(screen.getByText(/2\/7 failed/)).toHaveTextContent("2/7 failed");
    expect(screen.getByText("Has merge conflicts")).toBeInTheDocument();
  });

  // --- Terminal PRs suppress both elements ---------------------------------

  it("shows neither status element for merged PRs", async () => {
    mockPRs = [
      makePR({
        state: "merged",
        checks_rollup: "failure",
        checks_failed: 5,
        checks_total: 5,
        mergeable: "conflicting",
      }),
    ];
    renderList();
    await waitForRender();
    expect(screen.queryByText(/failed/)).not.toBeInTheDocument();
    expect(screen.queryByText("Has merge conflicts")).not.toBeInTheDocument();
    expect(screen.queryByText("No checks yet")).not.toBeInTheDocument();
  });

  it("shows neither status element for closed PRs", async () => {
    mockPRs = [
      makePR({
        state: "closed",
        checks_rollup: "success",
        checks_passed: 3,
        checks_total: 3,
        merge_state_status: "clean",
      }),
    ];
    renderList();
    await waitForRender();
    expect(screen.queryByText(/All checks passed/)).not.toBeInTheDocument();
    expect(screen.queryByText("Ready to merge")).not.toBeInTheDocument();
    expect(screen.queryByText("No checks yet")).not.toBeInTheDocument();
  });

  // --- Stale snapshot ------------------------------------------------------

  it("greys out the status elements and annotates the age when the snapshot is stale", async () => {
    mockPRs = [
      makePR({
        checks_rollup: "success",
        checks_total: 3,
        snapshot_stale: true,
        snapshot_fetched_at: "2026-01-01T00:00:00Z",
      }),
    ];
    renderList();
    await waitForRender();
    const badge = screen.getByText("All checks passed (3/3)");
    expect(badge).toHaveClass("opacity-60");
    expect(badge).toHaveAttribute("title");
    expect(badge.getAttribute("title")).toBeTruthy();
  });

  // --- Diff stats ----------------------------------------------------------

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

  // --- Collapse behaviour --------------------------------------------------

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

// MUL-7429: the line under the list says what "every linked PR merged, one says
// Closes → Done" will do, straight from the server's decision, and each row can
// be removed.
describe("PullRequestList auto-complete", () => {
  beforeEach(() => {
    mockAutoComplete = null;
    apiMock.unlinkIssuePullRequest.mockReset();
    apiMock.linkIssuePullRequest.mockReset();
    apiMock.setIssuePRAutoComplete.mockReset();
    toastMock.success.mockReset();
  });

  const decision = (state: string, ids: string[] = [], extra: Partial<PRAutoComplete> = {}): PRAutoComplete => ({
    state,
    pull_request_ids: ids,
    issue_disabled: false,
    workspace_enabled: true,
    ...extra,
  });

  it("names the PR the issue is waiting on", async () => {
    mockPRs = [makePR({ id: "a", number: 12, state: "merged" }), makePR({ id: "b", number: 19 })];
    mockAutoComplete = decision("waiting", ["b"]);
    renderList();
    expect(await screen.findByText("Completes when #19 merges")).toBeInTheDocument();
  });

  it("offers to remove a PR that closed without merging", async () => {
    mockPRs = [makePR({ id: "a", number: 12, state: "merged" }), makePR({ id: "b", number: 19, state: "closed" })];
    mockAutoComplete = decision("not_merged", ["b"]);
    apiMock.unlinkIssuePullRequest.mockResolvedValue({ pull_requests: [], auto_complete: decision("terminal") });
    renderList();
    const line = await screen.findByTestId("pr-auto-complete-line");
    expect(line).toHaveTextContent("#19 closed without merging");
    fireEvent.click(screen.getByRole("button", { name: "Remove" }));
    await waitFor(() => expect(apiMock.unlinkIssuePullRequest).toHaveBeenCalledWith("issue-1", "b"));
    // Removing the last unmerged PR completed the issue; the toast says so and
    // offers no undo.
    await waitFor(() =>
      expect(toastMock.success).toHaveBeenCalledWith("Removed #19. Every remaining PR is merged, so the issue is done."),
    );
  });

  it("says merging won't complete the issue when no PR closes it", async () => {
    mockPRs = [makePR({ id: "a", number: 12, link_source: "title" })];
    mockAutoComplete = decision("no_close_intent");
    renderList();
    expect(await screen.findByTestId("pr-auto-complete-line")).toHaveTextContent(
      "Won’t complete: no “Closes MUL-1”",
    );
  });

  it("links the workspace setting when auto-complete is off there", async () => {
    mockPRs = [makePR({ id: "a", number: 12, state: "merged" })];
    mockAutoComplete = decision("workspace_disabled", [], { workspace_enabled: false });
    renderList();
    expect(await screen.findByTestId("pr-auto-complete-line")).toHaveTextContent("PR auto-complete is off for this workspace");
    expect(screen.getByText("Settings").closest("a")?.getAttribute("href")).toBe("/acme/settings?tab=issue-statuses");
  });

  it("turns auto-complete back on for the issue from the line", async () => {
    mockPRs = [makePR({ id: "a", number: 12 })];
    mockAutoComplete = decision("issue_disabled", [], { issue_disabled: true });
    apiMock.setIssuePRAutoComplete.mockResolvedValue({ pull_requests: mockPRs, auto_complete: decision("waiting", ["a"]) });
    renderList();
    fireEvent.click(await screen.findByRole("button", { name: "Turn on" }));
    await waitFor(() => expect(apiMock.setIssuePRAutoComplete).toHaveBeenCalledWith("issue-1", false));
  });

  it("says nothing for a finished issue", async () => {
    mockPRs = [makePR({ id: "a", number: 12, state: "merged" })];
    mockAutoComplete = decision("terminal");
    renderList();
    await waitForRender();
    expect(screen.queryByTestId("pr-auto-complete-line")).toBeNull();
  });

  it("hides row actions and the line on a backend without auto-complete", async () => {
    mockPRs = [makePR({ id: "a", number: 12 })];
    mockAutoComplete = null;
    renderList();
    await waitForRender();
    expect(screen.queryByRole("button", { name: "Pull request actions" })).toBeNull();
    expect(screen.queryByTestId("pr-auto-complete-line")).toBeNull();
  });

  it("explains how a PR was linked in its menu", async () => {
    mockPRs = [makePR({ id: "a", number: 12, link_source: "title" })];
    mockAutoComplete = decision("waiting", ["a"]);
    renderList();
    fireEvent.click(await screen.findByRole("button", { name: "Pull request actions" }));
    expect(await screen.findByText("Linked by MUL-1 in the title")).toBeInTheDocument();
    expect(screen.getByRole("menuitem", { name: "Remove from issue" })).toBeInTheDocument();
  });
});
