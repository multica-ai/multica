import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import { issueKeys } from "@multica/core/issues/queries";
import type { TaskCodeChange, TimelineEntry } from "@multica/core/types";
import enCommon from "../../../locales/en/common.json";
import enEditor from "../../../locales/en/editor.json";
import enIssues from "../../../locales/en/issues.json";

const TEST_RESOURCES = { en: { common: enCommon, editor: enEditor, issues: enIssues } };

const apiMock = vi.hoisted(() => ({
  listIssueCodeChanges: vi.fn(),
  getIssueCodeChange: vi.fn(),
  listTasksByIssue: vi.fn(),
  listIssuePullRequests: vi.fn(),
  getIssuePullRequestDiff: vi.fn(),
  listTimeline: vi.fn(),
}));
vi.mock("@multica/core/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/api")>()),
  api: apiMock,
}));
vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({ getActorName: () => "Lambda" }),
}));
vi.mock("../../../platform", () => ({ openExternal: vi.fn() }));

import { CommentRunCodeChanges, RunCodeChanges } from "./run-code-changes";

const BRANCH = "agent/lambda/mul-1";

function change(over: Partial<TaskCodeChange>): TaskCodeChange {
  return {
    id: "cc-run",
    issue_id: "issue-1",
    task_id: "task-2",
    agent_id: "agent-1",
    scope: "run",
    source: "repo_checkout",
    repo_key: "https://github.com/acme/app",
    repo_label: "acme/app",
    repo_url: "https://github.com/acme/app.git",
    branch: BRANCH,
    base_ref: "origin/main",
    base_commit: "1111111",
    head_commit: "2222222",
    file_count: 3,
    additions: 96,
    deletions: 43,
    files_truncated: false,
    patch_available: true,
    patch_size: 100,
    patch_omitted: null,
    created_at: "2026-09-27T10:42:00Z",
    ...over,
  };
}

const RUN_PATCH = [
  "diff --git a/src/tab.tsx b/src/tab.tsx",
  "--- a/src/tab.tsx",
  "+++ b/src/tab.tsx",
  "@@ -1 +1 @@",
  "-old tab",
  "+new tab",
  "",
].join("\n");

const LINE_PATCH = [
  "diff --git a/src/first-run.ts b/src/first-run.ts",
  "new file mode 100644",
  "--- /dev/null",
  "+++ b/src/first-run.ts",
  "@@ -0,0 +1 @@",
  "+from the first run",
  "",
].join("\n");

const runRow = change({});
const branchRow = change({ id: "cc-branch", scope: "branch", file_count: 7, additions: 164, deletions: 58 });

function renderWithClient(ui: React.ReactElement, seed?: (qc: QueryClient) => void) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  seed?.(qc);
  render(
    <QueryClientProvider client={qc}>
      <I18nProvider resources={TEST_RESOURCES} locale="en">{ui}</I18nProvider>
    </QueryClientProvider>,
  );
  return qc;
}

describe("RunCodeChanges (MUL-7651)", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    apiMock.listIssueCodeChanges.mockResolvedValue({
      code_changes: [runRow, branchRow, change({ id: "other", task_id: "task-9" })],
    });
    apiMock.listTasksByIssue.mockResolvedValue([
      { id: "task-1", created_at: "2026-09-26T00:00:00Z", kind: "comment" },
      { id: "task-2", created_at: "2026-09-27T00:00:00Z", kind: "comment" },
    ]);
    apiMock.listIssuePullRequests.mockResolvedValue({ pull_requests: [], auto_complete: null });
    apiMock.getIssueCodeChange.mockImplementation(async (_issue: string, id: string) =>
      id === "cc-run"
        ? { ...runRow, files: [{ path: "src/tab.tsx", status: "modified", additions: 1, deletions: 1 }], patch: RUN_PATCH }
        : { ...branchRow, files: [{ path: "src/first-run.ts", status: "added", additions: 1, deletions: 0 }], patch: LINE_PATCH },
    );
  });

  it("shows the run's own changes and opens them in the viewer", async () => {
    renderWithClient(<RunCodeChanges issueId="issue-1" taskId="task-2" />);

    const card = await screen.findByRole("button", { name: /Changes in this run/ });
    expect(card.textContent).toContain("3 files");
    expect(card.textContent).toContain("+96");
    expect(card.textContent).toContain("−43");
    // One card: the branch row and another run's row are not this run's.
    expect(screen.getAllByRole("button", { name: /Changes in this run/ })).toHaveLength(1);

    fireEvent.click(card);
    expect(await screen.findByRole("button", { name: /tab\.tsx/ })).toBeTruthy();
    expect(apiMock.getIssueCodeChange).toHaveBeenCalledWith("issue-1", "cc-run");
    expect(screen.getByRole("button", { name: "This run #2" }).getAttribute("aria-pressed")).toBe("true");
    expect(screen.getByText("This run · 3 files")).toBeTruthy();
  });

  it("shows the whole issue through the pull request carrying the branch", async () => {
    apiMock.listIssuePullRequests.mockResolvedValue({
      pull_requests: [{ id: "pr-1", provider: "github", repo_owner: "acme", repo_name: "app", number: 8712, branch: BRANCH, additions: 164, deletions: 58, html_url: "https://github.com/acme/app/pull/8712" }],
      auto_complete: null,
    });
    apiMock.getIssuePullRequestDiff.mockResolvedValue({
      pull_request_id: "pr-1",
      head_sha: "",
      file_count: 1,
      additions: 1,
      deletions: 0,
      files: [{ path: "src/first-run.ts", status: "added", additions: 1, deletions: 0, binary: false }],
      files_truncated: false,
      patch: LINE_PATCH,
      patch_omitted: null,
    });
    renderWithClient(<RunCodeChanges issueId="issue-1" taskId="task-2" />);
    fireEvent.click(await screen.findByRole("button", { name: /Changes in this run/ }));

    fireEvent.click(await screen.findByRole("button", { name: /Whole issue · PR #8712/ }));
    expect(await screen.findByRole("button", { name: /first-run\.ts/ })).toBeTruthy();
    expect(apiMock.getIssuePullRequestDiff).toHaveBeenCalledWith("issue-1", "pr-1");
    expect(apiMock.getIssueCodeChange).not.toHaveBeenCalledWith("issue-1", "cc-branch");
  });

  it("falls back to the branch diff when GitHub cannot supply the pull request", async () => {
    apiMock.listIssuePullRequests.mockResolvedValue({
      pull_requests: [{ id: "pr-1", provider: "github", repo_owner: "acme", repo_name: "app", number: 8712, branch: BRANCH, html_url: "" }],
      auto_complete: null,
    });
    apiMock.getIssuePullRequestDiff.mockRejectedValue(new Error("github app not configured"));
    renderWithClient(<RunCodeChanges issueId="issue-1" taskId="task-2" />);
    fireEvent.click(await screen.findByRole("button", { name: /Changes in this run/ }));

    fireEvent.click(await screen.findByRole("button", { name: /Whole issue/ }));
    expect(await screen.findByRole("button", { name: /first-run\.ts/ })).toBeTruthy();
    expect(apiMock.getIssueCodeChange).toHaveBeenCalledWith("issue-1", "cc-branch");
    expect(screen.getByText(/Couldn't read PR #8712 from GitHub/)).toBeTruthy();
  });

  it("keeps the file list when the patch was too large to keep", async () => {
    apiMock.getIssueCodeChange.mockResolvedValue({
      ...runRow,
      patch_available: false,
      patch_omitted: "too_large",
      files: [{ path: "gen/huge.json", status: "added", additions: 90000, deletions: 0 }],
      patch: null,
    });
    renderWithClient(<RunCodeChanges issueId="issue-1" taskId="task-2" />);
    fireEvent.click(await screen.findByRole("button", { name: /Changes in this run/ }));

    expect(await screen.findByRole("button", { name: /huge\.json/ })).toBeTruthy();
    expect(screen.getByText(/only the file list was kept/)).toBeTruthy();
    expect(screen.getByText("No line-by-line diff was kept for this file")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Download patch" })).toBeNull();
  });
});

describe("CommentRunCodeChanges (MUL-7651)", () => {
  const comment = (id: string, created_at: string): TimelineEntry =>
    ({
      id,
      type: "comment",
      actor_type: "agent",
      actor_id: "agent-1",
      source_task_id: "task-2",
      created_at,
      content: "done",
    }) as TimelineEntry;
  const progress = comment("c-progress", "2026-09-27T10:40:00Z");
  const reply = comment("c-reply", "2026-09-27T10:42:00Z");

  beforeEach(() => {
    vi.clearAllMocks();
    apiMock.listIssueCodeChanges.mockResolvedValue({ code_changes: [runRow] });
    apiMock.listTasksByIssue.mockResolvedValue([]);
  });

  it("puts the card on the run's reply, not on its earlier comments", async () => {
    const seed = (qc: QueryClient) => qc.setQueryData(issueKeys.timeline("issue-1"), [progress, reply]);
    renderWithClient(
      <>
        <div data-testid="progress"><CommentRunCodeChanges issueId="issue-1" entry={progress} /></div>
        <div data-testid="reply"><CommentRunCodeChanges issueId="issue-1" entry={reply} /></div>
      </>,
      seed,
    );
    await waitFor(() => expect(screen.getByTestId("reply").textContent).toContain("Changes in this run"));
    expect(screen.getByTestId("progress").textContent).toBe("");
    expect(apiMock.listTimeline).not.toHaveBeenCalled();
  });

  it("shows nothing outside an issue page's timeline", () => {
    renderWithClient(<CommentRunCodeChanges issueId="issue-1" entry={reply} />);
    expect(apiMock.listTimeline).not.toHaveBeenCalled();
    expect(apiMock.listIssueCodeChanges).not.toHaveBeenCalled();
  });
});
