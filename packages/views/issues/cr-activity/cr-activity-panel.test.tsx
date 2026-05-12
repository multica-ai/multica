import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { CRAttempt, CRSignal } from "@multica/core/types";
import { CRActivityPanel } from "./cr-activity-panel";

const mockApi = vi.hoisted(() => ({
  listCRAttempts: vi.fn(),
  listCRSignals: vi.fn(),
}));

vi.mock("@multica/core/api", () => ({
  api: mockApi,
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

function renderPanel(issueStatus = "coderabbit") {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={client}>
      <CRActivityPanel issueId="issue-1" issueStatus={issueStatus} />
    </QueryClientProvider>,
  );
}

function attempt(id: string, outcome: CRAttempt["outcome"], round: number): CRAttempt {
  return {
    id,
    issue_id: "issue-1",
    workspace_id: "ws-1",
    cr_round: round,
    pr_url: "https://github.com/acme/repo/pull/7",
    head_sha: "abc123",
    started_at: "2026-05-12T10:00:00.000Z",
    review_submitted_at: null,
    review_state: null,
    findings_count: 0,
    outcome,
    outcome_reason: null,
    closed_at: null,
    first_signal_at: null,
    first_signal_kind: null,
  };
}

describe("CRActivityPanel", () => {
  beforeEach(() => {
    mockApi.listCRAttempts.mockReset();
    mockApi.listCRSignals.mockReset();
  });

  it("renders attempts with outcome badges for all six outcomes", async () => {
    mockApi.listCRAttempts.mockResolvedValue([
      attempt("a1", "completed_clean", 1),
      attempt("a2", "completed_with_findings", 2),
      attempt("a3", "silent_partial", 3),
      attempt("a4", "silent_total", 4),
      attempt("a5", "failed", 5),
      attempt("a6", "skipped", 6),
    ]);

    renderPanel();

    expect(await screen.findByText("completed clean")).toBeInTheDocument();
    expect(screen.getByText("completed with findings")).toBeInTheDocument();
    expect(screen.getByText("silent partial")).toBeInTheDocument();
    expect(screen.getByText("silent total")).toBeInTheDocument();
    expect(screen.getByText("failed")).toBeInTheDocument();
    expect(screen.getByText("skipped")).toBeInTheDocument();
  });

  it("expanding a row fetches and displays signal stream entries", async () => {
    mockApi.listCRAttempts.mockResolvedValue([attempt("a1", "completed_clean", 1)]);
    mockApi.listCRSignals.mockResolvedValue([
      {
        id: "s1",
        attempt_id: "a1",
        signal_kind: "check_run",
        signal_action: "completed",
        received_at: "2026-05-12T10:01:00.000Z",
        payload_summary: { name: "CodeRabbit", conclusion: "success" },
      } satisfies CRSignal,
    ]);

    renderPanel();
    fireEvent.click(await screen.findByRole("button", { name: /round 1/i }));

    await waitFor(() => expect(mockApi.listCRSignals).toHaveBeenCalledWith("issue-1", "a1"));
    expect(await screen.findByText("check run")).toBeInTheDocument();
    expect(screen.getByText("CodeRabbit · success")).toBeInTheDocument();
  });

  it("renders null for an empty attempt list", async () => {
    mockApi.listCRAttempts.mockResolvedValue([]);
    const { container } = renderPanel();
    await waitFor(() => expect(mockApi.listCRAttempts).toHaveBeenCalledWith("issue-1"));
    await waitFor(() => expect(container.firstChild).toBeNull());
  });
});
