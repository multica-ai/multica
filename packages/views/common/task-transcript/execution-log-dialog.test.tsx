// @vitest-environment jsdom

import { cleanup, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { ReactNode } from "react";
import { renderWithI18n } from "../../test/i18n";
import { api } from "@multica/core/api";
import type { AgentTask } from "@multica/core/types/agent";
import type { ExecutionLogPage } from "@multica/core/types/events";
import { ExecutionLogDialog } from "./execution-log-dialog";

vi.mock("@multica/core/api", () => ({
  api: {
    listTaskMessagesPage: vi.fn(),
    getAgent: vi.fn(),
    listRuntimes: vi.fn(),
  },
}));

// react-virtuoso renders NO data rows under jsdom's zero-height viewport, so
// these tests exercise only the chrome that lives OUTSIDE the virtualized list
// (empty / error / counts). Stubbing it keeps the render deterministic.
vi.mock("react-virtuoso", () => ({
  Virtuoso: () => <div data-testid="virtuoso-stub" />,
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
const getAgent = vi.mocked(api.getAgent);
const listRuntimes = vi.mocked(api.listRuntimes);

function renderDialog() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <ExecutionLogDialog
        open
        onOpenChange={vi.fn()}
        task={baseTask}
        agentName="Codex"
      />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  listTaskMessagesPage.mockReset();
  getAgent.mockReset();
  listRuntimes.mockReset();
  // Run-context lookups are best-effort; the dialog swallows failures. Keep them
  // resolving empty so the identity/summary rows just render without metadata.
  getAgent.mockRejectedValue(new Error("no agent in test"));
  listRuntimes.mockResolvedValue([]);
});

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

describe("ExecutionLogDialog", () => {
  it("renders the empty state when the Run has no events", async () => {
    listTaskMessagesPage.mockResolvedValue(pageWith({ raw_total: 0 }));

    renderDialog();

    expect(await screen.findByText("No execution events")).toBeInTheDocument();
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
});
