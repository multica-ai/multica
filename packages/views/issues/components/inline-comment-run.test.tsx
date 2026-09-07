import { act, cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, describe, expect, it, vi } from "vitest";
import { api } from "@multica/core/api";
import { chatKeys } from "@multica/core/chat/queries";
import type { AgentTask } from "@multica/core/types";
import type { TaskMessagePayload } from "@multica/core/types/events";
import { renderWithI18n } from "../../test/i18n";
import { InlineCommentRun } from "./inline-comment-run";

vi.mock("@multica/core/api", () => ({ api: {
  listTaskMessages: vi.fn(), cancelTask: vi.fn(), rerunIssue: vi.fn(),
}, dispatchReasonCode: () => undefined }));
vi.mock("@multica/core/workspace/hooks", () => ({ useActorName: () => ({ getActorName: () => "Reviewer" }) }));
vi.mock("../../common/actor-avatar", () => ({ ActorAvatar: () => <span /> }));
vi.mock("../../editor", () => ({ ReadonlyContent: ({ content }: { content: string }) => <div>{content}</div> }));
vi.mock("../../common/task-transcript/agent-transcript-dialog", () => ({
  AgentTranscriptDialog: () => <div role="dialog">Full transcript</div>,
  StepBody: ({ item }: { item: { output?: string } }) => <div>{item.output}</div>,
}));

const id = "4a2e8d1c-7f9b-4e2a-9c1d-123456789abc";
function task(overrides: Partial<AgentTask> = {}): AgentTask {
  return { id, agent_id: "agent", runtime_id: "runtime", issue_id: "issue", status: "running", priority: 0,
    created_at: "2026-09-07T00:00:00Z", started_at: "2026-09-07T00:00:00Z", dispatched_at: null,
    completed_at: null, result: null, error: null, ...overrides };
}
const messages: TaskMessagePayload[] = [
  { task_id: id, issue_id: "issue", seq: 1, type: "text", content: "Checking navigation." },
  { task_id: id, issue_id: "issue", seq: 2, type: "tool_use", tool: "exec_command", input: { command: "pnpm test" } },
];
afterEach(() => { cleanup(); vi.clearAllMocks(); });

function setup(initialTask: AgentTask, hasReply = false) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  const view = (current: AgentTask, reply = hasReply) => <QueryClientProvider client={client}>
    <InlineCommentRun run={{ task: current, commentId: "comment", hasReply: reply }} />
  </QueryClientProvider>;
  const rendered = renderWithI18n(view(initialTask));
  return { client, rerender: (current: AgentTask, reply = hasReply) => rendered.rerender(view(current, reply)) };
}

describe("InlineCommentRun", () => {
  it("shows real activity, expands in place, follows WS data and retains the open transcript at completion", async () => {
    vi.mocked(api.listTaskMessages).mockResolvedValue(messages);
    const current = task();
    const { client, rerender } = setup(current);
    await screen.findByText("pnpm test");
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    const toggle = screen.getByRole("button", { name: /View activity/ });
    expect(toggle).toHaveAttribute("aria-expanded", "false");
    fireEvent.click(toggle);
    expect(toggle).toHaveAttribute("aria-expanded", "true");
    const tail: TaskMessagePayload = { task_id: id, issue_id: "issue", seq: 3, type: "tool_result", tool: "exec_command", output: "12 passed" };
    act(() => client.setQueryData(chatKeys.taskMessages(id), [...messages, tail]));
    expect(screen.queryByText("Waiting for the agent to respond.")).not.toBeInTheDocument();
    vi.mocked(api.listTaskMessages).mockResolvedValue([...messages, tail]);
    rerender({ ...current, status: "completed", completed_at: "2026-09-07T00:01:23Z" }, true);
    await screen.findByText("Completed");
    expect(screen.getByRole("button", { name: /View activity/ })).toHaveAttribute("aria-expanded", "true");
    expect(screen.queryByRole("button", { name: "Stop" })).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Open full log" }));
    expect(screen.getByRole("dialog")).toHaveTextContent("Full transcript");
  });

  it("retains the last meaningful activity between tool results and the next agent message", async () => {
    vi.mocked(api.listTaskMessages).mockResolvedValue([]);
    const { client } = setup(task());
    await screen.findByText("Waiting for the agent to respond.");
    await waitFor(() => expect(client.isFetching()).toBe(0));
    const events: TaskMessagePayload[] = [];
    async function receive(event: Omit<TaskMessagePayload, "task_id" | "issue_id" | "seq">) {
      events.push({ task_id: id, issue_id: "issue", seq: events.length + 1, ...event });
      await act(async () => {
        client.setQueryData(chatKeys.taskMessages(id), [...events]);
        await new Promise((resolve) => setTimeout(resolve, 0));
      });
    }
    await receive({ type: "text", content: "Checking navigation." });
    await screen.findByText("Checking navigation.");
    await receive({ type: "tool_use", tool: "exec_command", input: { command: "pnpm test" } });
    await screen.findByText("pnpm test");
    await receive({ type: "tool_result", tool: "exec_command", output: "12 passed" });
    expect(screen.getByText("pnpm test")).toBeInTheDocument();
    expect(screen.queryByText("Waiting for the agent to respond.")).not.toBeInTheDocument();
    await receive({ type: "text", content: "  " });
    expect(screen.getByText("pnpm test")).toBeInTheDocument();
    await receive({ type: "text", content: "Tests passed. Reviewing the changes." });
    await screen.findByText("Tests passed. Reviewing the changes.");
    expect(screen.queryByText("pnpm test")).not.toBeInTheDocument();
  });

  it("keeps completed replies readable without fetching or duplicating their deliverable", () => {
    setup(task({ status: "completed", result: { comment: "Already posted" } }), true);
    expect(api.listTaskMessages).not.toHaveBeenCalled();
    expect(screen.queryByText("Already posted")).not.toBeInTheDocument();
    expect(screen.getByText("Completed")).toBeInTheDocument();
  });

  it("shows a completed deliverable if the corresponding comment is missing", () => {
    setup(task({ status: "completed", result: { comment: "Review complete." } }));
    expect(screen.getByText("Review complete.")).toBeInTheDocument();
    expect(screen.getByText("Review complete.").closest('[data-slot="card"]')).toBeNull();
    expect(screen.getByText("Completed").closest('[data-slot="card"]')).not.toBeNull();
    expect(api.listTaskMessages).not.toHaveBeenCalled();
  });

  it("explains queued runs and confirms stopping the specific run", async () => {
    vi.mocked(api.cancelTask).mockResolvedValue(task({ status: "cancelled" }));
    setup(task({ status: "queued" }));
    expect(screen.getByText("Waiting for an available agent.")).toBeInTheDocument();
    expect(api.listTaskMessages).not.toHaveBeenCalled();
    vi.mocked(api.listTaskMessages).mockResolvedValue([]);
    fireEvent.click(screen.getByRole("button", { name: /View activity/ }));
    await screen.findByText("No activity recorded yet.");
    expect(screen.queryByText("Waiting for the agent to respond.")).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Stop" }));
    expect(api.cancelTask).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Stop run" }));
    await waitFor(() => expect(api.cancelTask).toHaveBeenCalledWith("issue", id));
  });

  it("keeps a failed transcript fetch recoverable inside the comment", async () => {
    vi.mocked(api.listTaskMessages).mockRejectedValueOnce(new Error("offline"));
    setup(task({ status: "failed" }));
    fireEvent.click(screen.getByRole("button", { name: /View activity/ }));
    await screen.findByRole("alert");
    vi.mocked(api.listTaskMessages).mockResolvedValue(messages);
    fireEvent.click(screen.getByRole("button", { name: "Try again" }));
    await waitFor(() => expect(screen.queryByRole("alert")).not.toBeInTheDocument());
  });
});
