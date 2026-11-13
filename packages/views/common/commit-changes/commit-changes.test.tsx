import { fireEvent, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import { chatKeys } from "@multica/core/chat/queries";
import type { AgentTask, TaskMessagePayload } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import {
  CachedTaskCommitChanges,
  CommitChanges,
  IssueTaskCommitChanges,
  TaskCommitChanges,
} from "./commit-changes";

vi.mock("@multica/core/api", () => ({
  api: { listTaskMessages: vi.fn(), listTasksByIssue: vi.fn() },
}));
const message: TaskMessagePayload = {
  task_id: "33333333-3333-4333-8333-333333333333",
  issue_id: "issue",
  seq: 1,
  type: "tool_use",
  tool: "multica_commit_diff",
  created_at: "2026-11-11T00:00:00Z",
  input: {
    repository: "demo",
    repository_id: "repo-1",
    base: "a".repeat(40),
    head: "b".repeat(40),
    changes: [
      {
        path: "src/main.ts",
        kind: "modify",
        diff: "@@ -1 +1 @@\n-old value\n+new value",
      },
      { path: "image.png", kind: "add", binary: true },
    ],
  },
};
describe("CommitChanges", () => {
  it("opens a read-only commit range and switches between text and binary files", async () => {
    renderWithI18n(<CommitChanges messages={[message]} />);
    fireEvent.click(screen.getByRole("button", { name: "Run changes · 2 files" }));
    expect(await screen.findByRole("dialog", { name: "Run changes" })).toBeInTheDocument();
    expect(screen.getByText("demo")).toBeInTheDocument();
    expect(screen.getByText("Commits")).toBeInTheDocument();
    expect(screen.getByText("aaaaaaaa → bbbbbbbb")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Commits" })).not.toBeInTheDocument();
    expect(screen.getByRole("dialog")).toHaveTextContent("new value");
    fireEvent.click(screen.getByRole("button", { name: "image.png" }));
    expect(
      screen.getByText("No textual diff is available."),
    ).toBeInTheDocument();
  });
  it("shows file changes when the run ends on the same commit", async () => {
    renderWithI18n(
      <CommitChanges messages={[{
        ...message,
        input: { ...message.input, worktree: true, head: "a".repeat(40) },
      }]} />,
    );
    fireEvent.click(screen.getByRole("button", { name: "Run changes · 2 files" }));
    expect(await screen.findByText("Working tree, start to end")).toBeInTheDocument();
    expect(screen.getByRole("dialog")).toHaveTextContent("new value");
  });
  it("shows the incomplete status in place of a truncated file count", () => {
    renderWithI18n(
      <CommitChanges messages={[{ ...message, input: { ...message.input, truncated: true } }]} />,
      { locale: "zh-Hans" },
    );
    expect(screen.getByRole("button", { name: "本轮文件变化 · 记录不完整" })).toBeInTheDocument();
  });
  it("shows omitted repositories before the file viewer even when the first repository has no changes", async () => {
    renderWithI18n(
      <CommitChanges messages={[{
        ...message,
        input: { ...message.input, changes: [], repositories_omitted: true },
      }, {
        ...message,
        seq: 2,
        input: { ...message.input, repository: "other", repository_id: "repo-2" },
      }]} />,
      { locale: "zh-Hans" },
    );
    fireEvent.click(screen.getByRole("button", { name: "本轮文件变化 · 记录不完整" }));
    expect(await screen.findByText("达到仓库记录上限，部分仓库未采集。")).toBeInTheDocument();
  });
  it("shows the incomplete status when a file's diff could not be read", async () => {
    renderWithI18n(<CommitChanges messages={[{
      ...message,
      input: {
        ...message.input,
        changes: [{ path: "src/main.ts", kind: "modify", unavailable: true }],
      },
    }]} />);
    fireEvent.click(screen.getByRole("button", { name: "Run changes · Incomplete record" }));
    expect(await screen.findByRole("button", { name: "src/main.ts" })).toBeInTheDocument();
    expect(screen.getByText("This file’s diff could not be recorded in full. Review it in the repository.")).toBeInTheDocument();
  });
  it("opens commits before an empty working tree and keeps all comparisons selectable", async () => {
    const worktree = {
      ...message,
      seq: 2,
      input: { ...message.input, kind: "worktree", changes: [] },
    };
    const draft = {
      ...message,
      seq: 3,
      input: { ...message.input, kind: "draft", changes: [{ path: "draft.txt", kind: "add" }] },
    };
    renderWithI18n(<CommitChanges messages={[message, worktree, draft]} />);
    expect(screen.getByText("src/main.ts, image.png")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Run changes · 3 files" }));
    expect(await screen.findByRole("button", { name: "Commits", pressed: true })).toBeInTheDocument();
    expect(screen.getByRole("dialog")).toHaveTextContent("new value");
    fireEvent.click(screen.getByRole("button", { name: "Working tree, start to end" }));
    expect(screen.getByText("No net working tree changes.")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Working tree changes could not be recorded." })).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Saved draft" }));
    expect(screen.getByRole("button", { name: "draft.txt" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "src/main.ts" })).not.toBeInTheDocument();
  });
  it.each([
    ["no comparison lists a file", { changes: [] }],
    ["the only comparison failed", { kind: "worktree", unavailable: true, base: undefined, changes: [] }],
    ["only the repository limit was hit", { changes: [], repositories_omitted: true }],
  ])("renders nothing when %s", (_, input) => {
    const { container } = renderWithI18n(
      <CommitChanges messages={[{ ...message, input: { ...message.input, ...input } }]} />,
    );
    expect(container).toBeEmptyDOMElement();
  });
  it("shows partial capture failure on the default comparison for the affected repository", async () => {
    const unavailableWorktree = {
      ...message,
      seq: 2,
      input: { ...message.input, kind: "worktree", unavailable: true, changes: [] },
    };
    const otherRepository = {
      ...message,
      seq: 3,
      input: { ...message.input, repository: "other", repository_id: "repo-2" },
    };
    renderWithI18n(<CommitChanges messages={[message, unavailableWorktree, otherRepository]} />);
    fireEvent.click(screen.getByRole("button", { name: "Run changes · Incomplete record" }));
    expect(await screen.findByRole("button", { name: "Commits", pressed: true })).toBeInTheDocument();
    const failure = "Working tree changes could not be recorded.";
    fireEvent.click(screen.getByRole("button", { name: failure }));
    expect(screen.getByRole("button", { name: "Working tree, start to end", pressed: true })).toHaveFocus();
    expect(screen.getByRole("status")).toHaveTextContent(failure);
    expect(screen.queryByRole("button", { name: failure })).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "other" }));
    expect(screen.queryByText(failure)).not.toBeInTheDocument();
  });
  it("does not turn ordinary tool events into commit comparisons", () => {
    const { container } = renderWithI18n(
      <CommitChanges messages={[{ ...message, tool: "patch_apply" }]} />,
    );
    expect(container).toBeEmptyDOMElement();
  });
  it("loads the file summary only after opening an uncached run", async () => {
    // Simulate a visible reply even though jsdom does not lay out the page.
    vi.stubGlobal("IntersectionObserver", class {
      constructor(private callback: IntersectionObserverCallback) {}
      observe() {
        this.callback(
          [{ isIntersecting: true } as IntersectionObserverEntry],
          this as unknown as IntersectionObserver,
        );
      }
      disconnect() {}
    });
    try {
      vi.mocked(api.listTaskMessages).mockClear().mockResolvedValue([message]);
      const client = new QueryClient({
        defaultOptions: { queries: { retry: false } },
      });
      renderWithI18n(
        <QueryClientProvider client={client}>
          <TaskCommitChanges taskId={message.task_id} />
        </QueryClientProvider>,
      );
      expect(screen.queryByText("Run changes · 2 files")).not.toBeInTheDocument();
      expect(api.listTaskMessages).not.toHaveBeenCalled();
      fireEvent.click(screen.getByRole("button", { name: "Run changes" }));
      expect(await screen.findByText("Run changes · 2 files")).toBeInTheDocument();
      expect(screen.getByText("src/main.ts, image.png")).toBeInTheDocument();
      expect(api.listTaskMessages).toHaveBeenCalledTimes(1);
    } finally {
      vi.unstubAllGlobals();
    }
  });
  it("shows an empty state when the run has no records", async () => {
    vi.mocked(api.listTaskMessages).mockClear();
    vi.mocked(api.listTaskMessages).mockResolvedValue([]);
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    renderWithI18n(
      <QueryClientProvider client={client}>
        <TaskCommitChanges taskId={message.task_id} />
      </QueryClientProvider>,
    );
    expect(api.listTaskMessages).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Run changes" }));
    await waitFor(() =>
      expect(
        screen.getByText("No changes were recorded for this run."),
      ).toBeInTheDocument(),
    );
    expect(api.listTaskMessages).toHaveBeenCalledWith(message.task_id);
  });
});

describe("IssueTaskCommitChanges", () => {
  const issueId = "issue";
  const renderForTasks = async (tasks: Partial<AgentTask>[]) => {
    vi.mocked(api.listTaskMessages).mockClear();
    vi.mocked(api.listTasksByIssue).mockClear().mockResolvedValue(tasks as AgentTask[]);
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const view = renderWithI18n(
      <QueryClientProvider client={client}>
        <IssueTaskCommitChanges issueId={issueId} taskId={message.task_id} />
      </QueryClientProvider>,
    );
    await waitFor(() => expect(api.listTasksByIssue).toHaveBeenCalled());
    return view;
  };

  it("stays hidden while the run is active or recorded no files", async () => {
    const { container } = await renderForTasks([
      { id: message.task_id, status: "running" },
    ]);
    await waitFor(() => expect(container).toBeEmptyDOMElement());
    expect(api.listTaskMessages).not.toHaveBeenCalled();
  });

  it("appears once the run list marks recorded files", async () => {
    await renderForTasks([
      { id: message.task_id, status: "completed", has_recorded_changes: true },
    ]);
    expect(await screen.findByRole("button", { name: "Run changes" })).toBeInTheDocument();
    expect(api.listTaskMessages).not.toHaveBeenCalled();
  });
});

describe("CachedTaskCommitChanges", () => {
  it("renders from the cached transcript without fetching it", () => {
    vi.mocked(api.listTaskMessages).mockClear();
    const client = new QueryClient();
    client.setQueryData(chatKeys.taskMessages(message.task_id), [message]);
    renderWithI18n(
      <QueryClientProvider client={client}>
        <CachedTaskCommitChanges taskId={message.task_id} />
      </QueryClientProvider>,
    );
    expect(screen.getByRole("button", { name: "Run changes · 2 files" })).toBeInTheDocument();
    expect(api.listTaskMessages).not.toHaveBeenCalled();
  });

  it("renders nothing before the transcript is cached", () => {
    const { container } = renderWithI18n(
      <QueryClientProvider client={new QueryClient()}>
        <CachedTaskCommitChanges taskId={message.task_id} />
      </QueryClientProvider>,
    );
    expect(container).toBeEmptyDOMElement();
    expect(api.listTaskMessages).not.toHaveBeenCalled();
  });
});
