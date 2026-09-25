import { act, cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, describe, expect, it, vi } from "vitest";
import { api } from "@multica/core/api";
import { issueKeys } from "@multica/core/issues/queries";
import type { AgentTask, Comment, TimelineEntry } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { formatAgentNames, SteerBadge, SteerReceipts } from "./steer-receipts";

vi.mock("@multica/core/api", () => ({ api: {
  retryTaskSupplement: vi.fn(), createComment: vi.fn(), listTasksByIssue: vi.fn(),
  previewCommentTriggers: vi.fn(),
} }));

function recipients(...ids: string[]) {
  return { agents: ids.map((id) => ({ id, name: id, source: "mention_agent", reason: "" })) };
}
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "workspace" }));
vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({ getActorName: (_type: string, id: string) => (id === "orion" ? "Orion" : "Lambda") }),
}));

const turn = "4a2e8d1c-7f9b-4e2a-9c1d-123456789abc";

function task(overrides: Partial<AgentTask> = {}): AgentTask {
  return { id: turn, agent_id: "lambda", runtime_id: "runtime", issue_id: "issue", status: "running", priority: 0,
    created_at: "2026-09-07T00:00:00Z", started_at: "2026-09-07T00:00:00Z", dispatched_at: null,
    completed_at: null, result: null, error: null, ...overrides };
}

function entry(overrides: Partial<TimelineEntry> = {}): TimelineEntry {
  return { id: "steer", type: "comment", actor_type: "member", actor_id: "user", content: "Only fix web.",
    parent_id: "thread", created_at: "2026-09-07T00:00:10Z", ...overrides };
}

function render(node: React.ReactNode, tasks: AgentTask[] = [task()]) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  client.setQueryData(issueKeys.tasks("issue"), tasks);
  renderWithI18n(<QueryClientProvider client={client}>{node}</QueryClientProvider>);
  return client;
}

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

describe("SteerReceipts", () => {
  it("renders nothing, and observes nothing, for a comment that steered no turn", () => {
    const client = new QueryClient();
    renderWithI18n(<QueryClientProvider client={client}>
      <SteerReceipts issueId="issue" entry={entry()} />
      <SteerBadge issueId="issue" entry={entry()} />
    </QueryClientProvider>);
    expect(client.getQueryCache().getAll()).toHaveLength(0);
    expect(document.body).toHaveTextContent("");
  });

  it("follows one receipt per steered turn, naming each agent", () => {
    const other = { ...task(), id: "other-turn", agent_id: "orion" };
    render(<>
      <SteerBadge issueId="issue" entry={entry({ supplements: [
        { task_id: turn, agent_id: "lambda", status: "pending" },
        { task_id: "other-turn", agent_id: "orion", status: "delivered", delivered_at: "2026-09-07T00:00:12Z" },
      ] })} />
      <SteerReceipts issueId="issue" entry={entry({ supplements: [
        { task_id: turn, agent_id: "lambda", status: "pending" },
        { task_id: "other-turn", agent_id: "orion", status: "delivered", delivered_at: "2026-09-07T00:00:12Z" },
      ] })} />
    </>, [task(), other]);
    expect(screen.getByText("Added to Lambda and Orion's run")).toBeInTheDocument();
    expect(screen.getByRole("status")).toHaveTextContent("Waiting for Lambda to read it");
    expect(screen.getByText("Read by Orion")).toBeInTheDocument();
  });

  it("settles a pending receipt when its run ends first and offers a new run instead", async () => {
    vi.mocked(api.createComment).mockResolvedValue({ id: "resent" } as Comment);
    vi.mocked(api.previewCommentTriggers).mockResolvedValue(recipients("lambda"));
    const client = render(<SteerReceipts issueId="issue" entry={entry({
      supplement_task_id: turn, supplement_status: "pending",
    })} />);
    expect(screen.getByRole("status")).toHaveTextContent("Waiting for Lambda to read it");
    act(() => client.setQueryData(issueKeys.tasks("issue"), [task({ status: "completed" })]));
    await waitFor(() => expect(screen.getByRole("alert")).toHaveTextContent("Lambda didn't get it · the run ended before delivery"));
    expect(screen.queryByRole("button", { name: "Retry" })).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Send as a new run" }));
    await waitFor(() => expect(api.createComment).toHaveBeenCalledWith(
      "issue", "Only fix web.", undefined, "thread", undefined, undefined, undefined,
    ));
    await waitFor(() => expect(screen.queryByRole("button", { name: "Send as a new run" })).not.toBeInTheDocument());
  });

  it("resends a failed receipt only to its own agent", async () => {
    vi.mocked(api.createComment).mockResolvedValue({ id: "resent" } as Comment);
    vi.mocked(api.previewCommentTriggers).mockResolvedValue(recipients("lambda", "orion"));
    render(<SteerReceipts issueId="issue" entry={entry({
      content: "[@Lambda](mention://agent/lambda) fix web; [@Orion](mention://agent/orion) check desktop",
      supplements: [
        { task_id: turn, agent_id: "lambda", status: "failed", failure_reason: "turn_ended" },
        { task_id: "other-turn", agent_id: "orion", status: "delivered" },
      ],
    })} />, [task({ status: "completed" }), task({ id: "other-turn", agent_id: "orion", status: "completed" })]);
    fireEvent.click(screen.getByRole("button", { name: "Send as a new run" }));
    await waitFor(() => expect(api.createComment).toHaveBeenCalled());
    const suppressed = vi.mocked(api.createComment).mock.calls[0]![5];
    expect(suppressed).toEqual(["orion"]);
  });

  it("retries a stable delivery failure into the same running turn", async () => {
    vi.mocked(api.retryTaskSupplement).mockResolvedValue();
    render(<SteerReceipts issueId="issue" entry={entry({ supplements: [
      { task_id: turn, agent_id: "lambda", status: "failed", failure_reason: "provider_rejected" },
    ] })} />);
    expect(screen.getByRole("alert")).toHaveTextContent("Lambda didn't get it · the agent rejected the message");
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    await waitFor(() => expect(api.retryTaskSupplement).toHaveBeenCalledWith("issue", turn, "steer"));
  });
});

describe("formatAgentNames", () => {
  it("spaces Latin names from the Chinese joiner but not from the enumeration comma", () => {
    expect(formatAgentNames("zh-Hans", ["Lambda", "Orion"])).toBe("Lambda 和 Orion");
    expect(formatAgentNames("zh-Hans", ["Lambda", "Orion", "Kappa"])).toBe("Lambda、Orion 和 Kappa");
    expect(formatAgentNames("zh-Hans", ["小助手", "Orion"])).toBe("小助手和 Orion");
    expect(formatAgentNames("en", ["Lambda", "Orion"])).toBe("Lambda and Orion");
  });
});
