import { act, cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { ReactNode } from "react";
import { api } from "@multica/core/api";
import { chatKeys } from "@multica/core/chat/queries";
import type { AgentTask } from "@multica/core/types";
import type { TaskMessagePayload } from "@multica/core/types/events";
import { renderWithI18n } from "../../test/i18n";
import { InlineCommentRun } from "./inline-comment-run";

vi.mock("@multica/core/api", () => ({ api: {
  getIssue: vi.fn(), listTaskMessages: vi.fn(), cancelTask: vi.fn(), rerunIssue: vi.fn(),
}, dispatchReasonCode: () => undefined }));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "workspace" }));
vi.mock("@multica/core/workspace/hooks", () => ({ useActorName: () => ({ getActorName: () => "Reviewer" }) }));
vi.mock("../../common/actor-avatar", () => ({ ActorAvatar: () => <span /> }));
vi.mock("../../editor", () => ({ ReadonlyContent: ({ content }: { content: string }) => <div>{content}</div> }));
vi.mock("../../common/task-transcript/agent-transcript-dialog", () => ({
  AgentTranscriptDialog: ({ contentState, isLive }: { contentState?: ReactNode; isLive?: boolean }) => <div role="dialog" data-live={isLive}>{contentState ?? "Full transcript"}</div>,
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

function setup(initialTask: AgentTask, hasReply = false, presentation: "inline" | "header" = "inline") {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  const view = (current: AgentTask, reply = hasReply) => <QueryClientProvider client={client}>
    <InlineCommentRun run={{ task: current, commentId: "comment", hasReply: reply }} presentation={presentation} />
  </QueryClientProvider>;
  const rendered = renderWithI18n(view(initialTask));
  return { client, rerender: (current: AgentTask, reply = hasReply) => rendered.rerender(view(current, reply)) };
}

describe("InlineCommentRun", () => {
  it("opens completed reply logs from a compact header button and loads only on demand", async () => {
    let resolve!: (value: TaskMessagePayload[]) => void;
    vi.mocked(api.listTaskMessages).mockImplementation(() => new Promise((done) => { resolve = done; }));
    setup(task({ status: "completed", completed_at: "2026-09-07T00:01:23Z" }), true, "header");
    expect(screen.queryByText("Completed")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /View activity/ })).not.toBeInTheDocument();
    expect(api.listTaskMessages).not.toHaveBeenCalled();
    const trigger = screen.getByRole("button", { name: "Open full log" });
    expect(trigger).toHaveAttribute("aria-haspopup", "dialog");
    fireEvent.click(trigger);
    expect(screen.getByRole("dialog")).toHaveTextContent("Loading activity");
    await act(async () => resolve(messages));
    await screen.findByText("Full transcript");
    expect(api.listTaskMessages).toHaveBeenCalledTimes(1);
  });

  it("keeps published replies visually settled while their full log remains live", async () => {
    vi.mocked(api.listTaskMessages).mockResolvedValue(messages);
    const current = task();
    const { rerender } = setup(current, true, "header");
    expect(screen.queryByRole("button", { name: /View activity/ })).not.toBeInTheDocument();
    expect(screen.queryByRole("status")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Stop" })).not.toBeInTheDocument();
    const trigger = screen.getByRole("button", { name: "Open full log" });
    const icon = trigger.querySelector("svg");
    expect(icon).not.toHaveClass("animate-spin");
    fireEvent.click(trigger);
    await screen.findByText("Full transcript");
    expect(screen.getByRole("dialog")).toHaveAttribute("data-live", "true");
    rerender({ ...current, status: "completed", completed_at: "2026-09-07T00:01:23Z" });
    expect(screen.getByRole("button", { name: "Open full log" })).toBe(trigger);
    expect(screen.getByRole("dialog")).toHaveTextContent("Full transcript");
    expect(screen.getByRole("dialog")).toHaveAttribute("data-live", "false");
    expect(trigger.querySelector("svg")).toBe(icon);
    expect(screen.queryByRole("button", { name: "Stop" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /View activity/ })).not.toBeInTheDocument();
  });

  it("lets a completed header log recover when its initial fetch fails", async () => {
    vi.mocked(api.listTaskMessages).mockRejectedValueOnce(new Error("offline"));
    setup(task({ status: "completed" }), true, "header");
    fireEvent.click(screen.getByRole("button", { name: "Open full log" }));
    await screen.findByRole("alert");
    vi.mocked(api.listTaskMessages).mockResolvedValue(messages);
    fireEvent.click(screen.getByRole("button", { name: "Try again" }));
    await screen.findByText("Full transcript");
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });

  it("uses a readable issue identifier in live progress and expanded activity", async () => {
    const issueId = "01a07eca-8e82-775e-be06-e4a97ccfa299";
    vi.mocked(api.getIssue).mockResolvedValue({ id: issueId, identifier: "DEV-17" } as Awaited<ReturnType<typeof api.getIssue>>);
    vi.mocked(api.listTaskMessages).mockResolvedValue([
      { task_id: id, issue_id: issueId, seq: 1, type: "tool_use", tool: "exec_command", input: { command: `multica issue get ${issueId} --output json` } },
    ]);
    setup(task({ issue_id: issueId }));
    await screen.findByText("multica issue get DEV-17 --output json");
    fireEvent.click(screen.getByRole("button", { name: /View activity/ }));
    expect(screen.getAllByText("multica issue get DEV-17 --output json")).toHaveLength(2);
    expect(api.getIssue).toHaveBeenCalledTimes(1);
  });

  it.each(["completed", "queued"] as const)("keeps the same focused disclosure button for a %s run", async (status) => {
    vi.mocked(api.listTaskMessages).mockResolvedValue(messages);
    setup(task({ status }));
    const before = screen.getByRole("button", { name: /View activity/ });
    before.focus();
    fireEvent.click(before, { detail: 0 });
    const expanded = screen.getByRole("button", { name: /View activity/ });
    expect(expanded).toBe(before);
    expect(expanded).toHaveFocus();
    expect(expanded).toHaveAttribute("aria-expanded", "true");
    await screen.findByRole("button", { name: "Open full log" });
    fireEvent.click(expanded, { detail: 0 });
    expect(screen.getByRole("button", { name: /View activity/ })).toHaveFocus();
    expect(screen.getByRole("button", { name: /View activity/ })).toHaveAttribute("aria-expanded", "false");
  });
  it("shows real activity, expands in place, follows WS data and retains the open transcript at completion", async () => {
    vi.mocked(api.listTaskMessages).mockResolvedValue(messages);
    const current = task();
    const { client, rerender } = setup(current);
    await screen.findByText("pnpm test");
    const progress = screen.getByText("pnpm test");
    expect(progress).not.toHaveClass("animate-chat-text-shimmer");
    expect(progress.closest("[data-run-summary]")).toHaveClass("h-[1lh]", "overflow-hidden");
    expect(document.querySelector("[data-run-loading-indicator]")).toHaveClass(
      "motion-safe:animate-spin",
      "motion-safe:[animation-duration:900ms]",
    );
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    const toggle = screen.getByRole("button", { name: /View activity/ });
    expect(toggle).toHaveAttribute("aria-expanded", "false");
    fireEvent.click(toggle);
    expect(toggle).toHaveAttribute("aria-expanded", "true");
    const tail: TaskMessagePayload = { task_id: id, issue_id: "issue", seq: 3, type: "tool_result", tool: "exec_command", output: "12 passed" };
    act(() => client.setQueryData(chatKeys.taskMessages(id), [...messages, tail]));
    await waitFor(() => expect(screen.queryByText("Waiting for the agent to respond.")).not.toBeInTheDocument());
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
    await waitFor(() => expect(screen.queryByText("Waiting for the agent to respond.")).not.toBeInTheDocument());
    await receive({ type: "text", content: "  " });
    expect(screen.getByText("pnpm test")).toBeInTheDocument();
    await receive({ type: "text", content: "Tests passed. Reviewing the changes." });
    await screen.findByText("Tests passed. Reviewing the changes.");
    await waitFor(() =>
      expect(screen.queryByText("pnpm test")).not.toBeInTheDocument(),
    );
  });

  it("keeps completed replies readable without fetching or duplicating their deliverable", () => {
    setup(task({ status: "completed", result: { version: 1, summary: "Already posted", artifact_ids: [] } }), true);
    expect(api.listTaskMessages).not.toHaveBeenCalled();
    expect(screen.queryByText("Already posted")).not.toBeInTheDocument();
    expect(screen.getByText("Completed")).toBeInTheDocument();
  });

  it("shows a completed deliverable if the corresponding comment is missing", () => {
    setup(task({ status: "completed", result: { version: 1, summary: "Review complete.", artifact_ids: [] } }));
    expect(screen.getByText("Review complete.")).toBeInTheDocument();
    expect(screen.getByText("Review complete.").closest('[data-slot="card"]')).toBeNull();
    expect(screen.getByText("Completed").closest('[data-slot="card"]')).toBeNull();
    expect(api.listTaskMessages).not.toHaveBeenCalled();
  });

  it.each(["queued", "running"] as const)("confirms stopping the specific %s run before its reply", async (state) => {
    vi.mocked(api.cancelTask).mockResolvedValue(task({ status: "cancelled" }));
    setup(task({ status: state }));
    if (state === "queued") {
      expect(screen.getByText("Waiting for an available agent.")).toBeInTheDocument();
      expect(api.listTaskMessages).not.toHaveBeenCalled();
      vi.mocked(api.listTaskMessages).mockResolvedValue([]);
      fireEvent.click(screen.getByRole("button", { name: /View activity/ }));
      await screen.findByText("No activity recorded yet.");
      expect(screen.queryByText("Waiting for the agent to respond.")).not.toBeInTheDocument();
    }
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
