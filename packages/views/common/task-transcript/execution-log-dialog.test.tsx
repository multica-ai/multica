// @vitest-environment jsdom

import { cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { ReactNode } from "react";
import { renderWithI18n } from "../../test/i18n";
import { api } from "@multica/core/api";
import { useTranscriptViewStore } from "@multica/core/agents/stores";
import { WorkspaceSlugProvider } from "@multica/core/paths";
import type { AgentTask } from "@multica/core/types/agent";
import type { ExecutionLogPage, TaskMessagePayload } from "@multica/core/types/events";
import { ExecutionLogDialog } from "./execution-log-dialog";

vi.mock("@multica/core/api", () => ({
  api: {
    listTaskMessagesPage: vi.fn(),
    listRuntimes: vi.fn(),
  },
}));

vi.mock("../actor-avatar", () => ({
  ActorAvatar: () => <span data-testid="execution-log-agent-avatar" />,
}));

// react-virtuoso renders no data rows under jsdom's zero-height viewport.
// Render itemContent directly so row layout and expanded detail stay covered.
vi.mock("react-virtuoso", () => ({
  Virtuoso: ({
    data = [],
    itemContent,
  }: {
    data?: TaskMessagePayload[];
    itemContent?: (index: number, item: TaskMessagePayload) => ReactNode;
  }) => (
    <div data-testid="virtuoso-stub">
      {data.map((item, index) => (
        <div key={`${item.seq}-${index}`}>{itemContent?.(index, item)}</div>
      ))}
    </div>
  ),
}));

// base-ui Dialog portals + focus-traps; render its children inline instead so
// the header/body assert cleanly in jsdom (mirrors agent-transcript-dialog.test).
vi.mock("@multica/ui/components/ui/dialog", () => ({
  Dialog: ({ open, children }: { open: boolean; children: ReactNode }) =>
    open ? <>{children}</> : null,
  DialogContent: ({
    children,
    showCloseButton: _showCloseButton,
    ...props
  }: {
    children: ReactNode;
    showCloseButton?: boolean;
  }) => <div {...props}>{children}</div>,
  DialogTitle: ({ children }: { children: ReactNode }) => <h2>{children}</h2>,
}));

const TASK_ID = "4a2e8d1c-7f9b-4e2a-9c1d-123456789abc";

const baseTask: AgentTask = {
  id: TASK_ID,
  agent_id: "agent-1",
  runtime_id: "",
  issue_id: "issue-1",
  status: "completed",
  priority: 0,
  dispatched_at: null,
  started_at: "2026-06-08T08:00:00Z",
  completed_at: "2026-06-08T08:01:00Z",
  result: null,
  error: null,
  created_at: "2026-06-08T08:00:00Z",
};

function pageWith(overrides: Partial<ExecutionLogPage>): ExecutionLogPage {
  return {
    messages: [],
    limit: 50,
    older_cursor: null,
    latest_cursor: null,
    raw_total: 0,
    matched_total: 0,
    type_facets: [],
    tool_facets: [],
    ...overrides,
  };
}

const listTaskMessagesPage = vi.mocked(api.listTaskMessagesPage);
const listRuntimes = vi.mocked(api.listRuntimes);

function renderDialog({
  task = baseTask,
  isLive,
  liveMessages,
}: {
  task?: AgentTask;
  isLive?: boolean;
  liveMessages?: TaskMessagePayload[];
} = {}) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <WorkspaceSlugProvider slug="test">
        <ExecutionLogDialog
          open
          onOpenChange={vi.fn()}
          task={task}
          agentName="Codex"
          isLive={isLive}
          liveMessages={liveMessages}
        />
      </WorkspaceSlugProvider>
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  listTaskMessagesPage.mockReset();
  listRuntimes.mockReset();
  useTranscriptViewStore.setState({
    sortDirection: "chronological",
    selectedFilterKeys: [],
    defaultExpanded: false,
  });
  // Run-context lookups are best-effort; the dialog swallows failures. Keep them
  // resolving empty so the identity/summary rows just render without metadata.
  listRuntimes.mockResolvedValue([]);
});

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

describe("ExecutionLogDialog", () => {
  it("shows transcript-shaped skeletons while the first page is loading", () => {
    listTaskMessagesPage.mockImplementation(() => new Promise(() => {}));

    renderDialog();

    expect(screen.queryByTestId("execution-log-header-skeleton")).not.toBeInTheDocument();
    expect(screen.getByTestId("execution-log-toolbar-skeleton")).toBeInTheDocument();
    expect(screen.getByTestId("execution-log-timeline-skeleton")).toBeInTheDocument();
    expect(screen.getAllByTestId("execution-log-row-skeleton")).toHaveLength(8);
  });

  it("keeps status first and separates run context with spacing instead of dots", async () => {
    listTaskMessagesPage.mockResolvedValue(
      pageWith({
        raw_total: 1,
        matched_total: 1,
        type_facets: [{ key: "tool_use", count: 1 }],
        messages: [
          {
            task_id: TASK_ID,
            issue_id: "issue-1",
            seq: 1,
            type: "tool_use",
            tool: "Bash",
            input: { command: "pnpm test" },
            content: "Run checks",
          },
        ],
      }),
    );

    renderDialog({
      task: {
        ...baseTask,
        kind: "comment",
        trigger_comment_id: "comment-1",
        attribution: {
          source: "direct_human",
          precise: true,
          initiator: { id: "u1", name: "Ada Lovelace" },
        },
      },
    });

    const header = screen.getByTestId("execution-log-run-header");
    const status = screen.getByTestId("execution-log-status");
    expect(header.firstElementChild).toBe(status);
    expect(header).not.toHaveClass("flex-wrap");
    expect(screen.getByText("Run by Codex")).toBeInTheDocument();
    expect(screen.getByText("Comment trigger")).toBeInTheDocument();
    expect(screen.getByText("Started by Ada Lovelace")).toBeInTheDocument();

    const summary = await screen.findByTestId("execution-log-summary");
    expect(summary).toHaveTextContent("Ran for 1m 0s");
    expect(summary).toHaveTextContent("1 tool call");
    expect(summary).toHaveTextContent("Started");
    expect(screen.getByTestId("execution-log-end-time")).toHaveTextContent("Ended");
    expect(screen.queryByTestId("execution-log-in-progress")).not.toBeInTheDocument();
    expect(summary).not.toHaveTextContent("·");
    expect(summary).toHaveClass("gap-x-3");
    expect(summary).toHaveClass("whitespace-nowrap");
    const actions = screen.getByTestId("execution-log-toolbar-actions");
    expect(actions).toContainElement(
      screen.getByRole("button", { name: "Oldest first" }),
    );
    expect(actions).toContainElement(
      screen.getByRole("button", { name: "Expand all" }),
    );
    expect(actions).toContainElement(
      screen.getByRole("button", { name: "Copy all" }),
    );
  });

  it("renders the empty state when the Run has no events", async () => {
    listTaskMessagesPage.mockResolvedValue(pageWith({ raw_total: 0 }));

    renderDialog();

    expect(await screen.findByText("No execution events")).toBeInTheDocument();
  });

  it("renders the live cache in the same dialog without starting pagination", async () => {
    const liveMessage: TaskMessagePayload = {
      task_id: TASK_ID,
      issue_id: "issue-1",
      seq: 1,
      type: "text",
      content: "Live result",
    };

    renderDialog({
      // A stale completed_at must never surface while the authoritative status
      // still says the Run is active.
      task: { ...baseTask, status: "running" },
      isLive: true,
      liveMessages: [liveMessage],
    });

    expect(await screen.findByText("Live result")).toBeInTheDocument();
    expect(screen.getByText("Working")).toBeInTheDocument();
    expect(screen.queryByTestId("execution-log-end-time")).not.toBeInTheDocument();
    expect(listTaskMessagesPage).not.toHaveBeenCalled();
  });

  it("renders an error state with a Retry action when the first page fails", async () => {
    listTaskMessagesPage.mockRejectedValue(new Error("boom"));

    renderDialog();

    expect(
      await screen.findByText("Failed to load the execution log."),
    ).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Retry" })).toBeInTheDocument();
  });

  it("shows the full-Run raw total", async () => {
    listTaskMessagesPage.mockResolvedValue(
      pageWith({
        raw_total: 42,
        matched_total: 42,
        messages: [
          {
            task_id: TASK_ID,
            issue_id: "issue-1",
            seq: 1,
            type: "text",
            content: "hello",
          },
        ],
      }),
    );

    renderDialog();

    await waitFor(() =>
      expect(screen.getByTestId("execution-log-total")).toHaveTextContent("42"),
    );
  });

  it("keeps the legacy row rhythm and renders expanded Agent content as compact Markdown", async () => {
    listTaskMessagesPage.mockResolvedValue(
      pageWith({
        raw_total: 1,
        matched_total: 1,
        type_facets: [{ key: "text", count: 1 }],
        messages: [
          {
            task_id: TASK_ID,
            issue_id: "issue-1",
            seq: 13,
            type: "text",
            content: "# 结论\n\n测试**通过**。",
            created_at: "2026-06-08T08:02:12Z",
          },
        ],
      }),
    );

    renderDialog();

    await screen.findByTestId("execution-log-row");
    expect(screen.getByTestId("execution-log-row-kind")).toHaveTextContent("Agent");

    const meta = screen.getByTestId("execution-log-row-meta");
    expect(meta).toHaveTextContent("#13");
    expect(meta).toHaveClass("flex", "items-center");
    expect(meta).not.toHaveClass("flex-col");

    expect(screen.getByTestId("execution-log-toolbar")).toBeInTheDocument();
    const chronologicalToggle = screen.getByRole("button", { name: "Oldest first" });
    const expandToggle = screen.getByRole("button", { name: "Expand all" });
    expect(chronologicalToggle).toHaveAttribute("aria-pressed", "true");
    expect(expandToggle).toHaveAttribute("aria-pressed", "false");
    expect(screen.getByRole("button", { name: "Copy all" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Run information" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "More" })).not.toBeInTheDocument();

    fireEvent.click(chronologicalToggle);
    expect(chronologicalToggle).toHaveAttribute("aria-pressed", "false");

    fireEvent.click(screen.getByRole("button", { name: "结论" }));

    expect(await screen.findByRole("heading", { name: "结论" })).toBeInTheDocument();
    expect(screen.getByText("通过")).toHaveProperty("tagName", "STRONG");
    expect(
      screen
        .getByTestId("execution-log-row-detail")
        .querySelector('[data-rich-content][data-density="compact"]'),
    ).not.toBeNull();
    expect(expandToggle).toHaveAttribute("aria-pressed", "true");
    expect(useTranscriptViewStore.getState().defaultExpanded).toBe(true);
    expect(screen.queryByRole("button", { name: "Collapse all" })).not.toBeInTheDocument();

    const agentFilter = screen.getByRole("button", { name: "Agent 1" });
    expect(agentFilter).toHaveAttribute("aria-pressed", "false");
    expect(agentFilter).toHaveClass("bg-transparent");
    fireEvent.click(agentFilter);
    await waitFor(() => {
      expect(agentFilter).toHaveAttribute("aria-pressed", "true");
      expect(agentFilter).toHaveClass("bg-accent");
    });
  });

  it("preserves selected filters across dialog remounts by default", async () => {
    listTaskMessagesPage.mockResolvedValue(
      pageWith({
        raw_total: 1,
        matched_total: 1,
        type_facets: [{ key: "text", count: 1 }],
        messages: [
          {
            task_id: TASK_ID,
            issue_id: "issue-1",
            seq: 1,
            type: "text",
            content: "Agent result",
          },
        ],
      }),
    );

    const first = renderDialog();
    const agentFilter = await screen.findByRole("button", { name: "Agent 1" });
    fireEvent.click(agentFilter);

    expect(useTranscriptViewStore.getState().selectedFilterKeys).toEqual(["text"]);

    first.unmount();
    renderDialog();

    await waitFor(() => {
      expect(screen.getByRole("button", { name: "Agent 1" })).toHaveAttribute(
        "aria-pressed",
        "true",
      );
    });
  });
});
