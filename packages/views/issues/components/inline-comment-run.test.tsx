import { act, cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { ReactNode } from "react";
import { api } from "@multica/core/api";
import { chatKeys } from "@multica/core/chat/queries";
import { issueKeys } from "@multica/core/issues/queries";
import { useCommentDraftStore, useTaskSupplementDraftStore } from "@multica/core/issues/stores";
import type { AgentTask, TimelineEntry } from "@multica/core/types";
import type { TaskMessagePayload } from "@multica/core/types/events";
import { renderWithI18n } from "../../test/i18n";
import { InlineCommentRun, PlacedInlineCommentRun } from "./inline-comment-run";
import { SupplementReceipt } from "./comment-card";

const dispatchReasonCodeMock = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/api", () => ({ api: {
  getIssue: vi.fn(), listTaskMessages: vi.fn(), cancelTask: vi.fn(), rerunIssue: vi.fn(),
  createTaskSupplement: vi.fn(), retryTaskSupplement: vi.fn(), listTasksByIssue: vi.fn(),
}, dispatchReasonCode: dispatchReasonCodeMock }));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "workspace" }));
vi.mock("@multica/core/workspace/hooks", () => ({ useActorName: () => ({ getActorName: () => "Reviewer" }) }));
vi.mock("../../common/actor-avatar", () => ({ ActorAvatar: () => <span /> }));
vi.mock("../../editor", () => ({ ReadonlyContent: ({ content }: { content: string }) => <div>{content}</div> }));
vi.mock("../../common/task-transcript/agent-transcript-dialog", () => ({
  AgentTranscriptDialog: ({ contentState, isLive }: { contentState?: ReactNode; isLive?: boolean }) => <div role="dialog" data-live={isLive}>{contentState ?? "Full transcript"}</div>,
  StepBody: ({ item }: { item: { content?: string; output?: string } }) => <div data-testid="step-body">{item.content ?? item.output}</div>,
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
afterEach(() => {
  cleanup();
  vi.clearAllMocks();
  useTaskSupplementDraftStore.setState({ drafts: {} });
  useCommentDraftStore.setState({ drafts: {} });
});

function setup(initialTask: AgentTask, hasReply = false, presentation: "inline" | "header" = "inline") {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  const view = (current: AgentTask, reply = hasReply) => <QueryClientProvider client={client}>
    <InlineCommentRun run={{ task: current, commentId: "comment", hasReply: reply }} presentation={presentation} />
  </QueryClientProvider>;
  const rendered = renderWithI18n(view(initialTask));
  return { client, rerender: (current: AgentTask, reply = hasReply) => rendered.rerender(view(current, reply)) };
}

describe("InlineCommentRun", () => {
  it("creates no receipt observers for ordinary or delivered comments", () => {
    const client = new QueryClient();
    const entry = { id: "comment", type: "comment" } as TimelineEntry;
    renderWithI18n(<QueryClientProvider client={client}>
      <SupplementReceipt issueId="issue" entry={entry} />
      <SupplementReceipt issueId="issue" entry={{ ...entry, supplement_task_id: id, supplement_status: "delivered" }} />
    </QueryClientProvider>);
    expect(client.getQueryCache().getAll()).toHaveLength(0);
  });

  it.each(["toggle", "cancel", "escape"])("closes and clears a running draft with %s", (action) => {
    vi.mocked(api.listTaskMessages).mockResolvedValue([]);
    setup(task({ supplement_capability: "task-supplement-v1", can_supplement: true }));
    fireEvent.click(screen.getByRole("button", { name: "Add message" }));
    const input = screen.getByPlaceholderText("Add guidance for this running turn");
    fireEvent.change(input, { target: { value: "Discard this draft." } });
    if (action === "escape") fireEvent.keyDown(input, { key: "Escape" });
    else fireEvent.click(screen.getByRole("button", { name: action === "toggle" ? "Add message" : "Cancel" }));
    expect(screen.queryByPlaceholderText("Add guidance for this running turn")).not.toBeInTheDocument();
    expect(useTaskSupplementDraftStore.getState().drafts[id]).toBeUndefined();
    expect(api.createTaskSupplement).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Add message" }));
    expect(screen.getByPlaceholderText("Add guidance for this running turn")).toHaveValue("");
  });

  it.each(["completed", "cancelled", "failed"] as const)("removes an empty panel when the run becomes %s", (status) => {
    vi.mocked(api.listTaskMessages).mockResolvedValue([]);
    const current = task({ supplement_capability: "task-supplement-v1", can_supplement: true });
    const { rerender } = setup(current);
    fireEvent.click(screen.getByRole("button", { name: "Add message" }));
    rerender({ ...current, status });
    expect(screen.queryByPlaceholderText("Add guidance for this running turn")).not.toBeInTheDocument();
    expect(useTaskSupplementDraftStore.getState().drafts[id]).toBeUndefined();
  });

  it.each(["Move to new message", "Discard"])("lets a terminal draft %s and restores the reply header", (action) => {
    vi.mocked(api.listTaskMessages).mockResolvedValue([]);
    const current = task({ supplement_capability: "task-supplement-v1", can_supplement: true });
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const view = (status: AgentTask["status"], hasReply: boolean) => <QueryClientProvider client={client}>
      <PlacedInlineCommentRun run={{ task: { ...current, status }, commentId: "reply", hasReply }} presentation="header" />
      <PlacedInlineCommentRun run={{ task: { ...current, status }, commentId: "reply", hasReply }} />
    </QueryClientProvider>;
    const rendered = renderWithI18n(view("running", false));
    fireEvent.click(screen.getByRole("button", { name: "Add message" }));
    fireEvent.change(screen.getByPlaceholderText("Add guidance for this running turn"), { target: { value: "Preserve this only on move." } });
    rendered.rerender(view("completed", true));
    expect(screen.getByRole("button", { name: "Move to new message" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Discard" })).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: action }));
    expect(useTaskSupplementDraftStore.getState().drafts[id]).toBeUndefined();
    expect(useCommentDraftStore.getState().getDraft("new:issue")).toBe(action === "Discard" ? undefined : "Preserve this only on move.");
    expect(screen.queryByPlaceholderText("Add guidance for this running turn")).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Open full log" })).toBeInTheDocument();
    expect(api.createTaskSupplement).not.toHaveBeenCalled();
  });

  it("hides additional messages without negotiated support", () => {
    const { client } = setup(task());
    expect(screen.queryByRole("button", { name: "Add message" })).not.toBeInTheDocument();
    client.clear();
  });

  it("disables additional messages when support is negotiated but permission is denied", () => {
    const { client } = setup(task({ supplement_capability: "task-supplement-v1", can_supplement: false }));
    expect(screen.getByRole("button", { name: "Add message" })).toBeDisabled();
    client.clear();
  });
  it("preserves additional-message text when delivery submission fails", async () => {
    vi.mocked(api.listTaskMessages).mockResolvedValue([]);
    vi.mocked(api.createTaskSupplement).mockRejectedValue(new Error("run ended"));
    setup(task({ supplement_capability: "task-supplement-v1", can_supplement: true }));
    fireEvent.click(screen.getByRole("button", { name: "Add message" }));
    const input = screen.getByPlaceholderText("Add guidance for this running turn");
    fireEvent.change(input, { target: { value: "preserve this draft" } });
    fireEvent.click(screen.getByRole("button", { name: "Send" }));
    await waitFor(() => expect(api.createTaskSupplement).toHaveBeenCalled());
    expect(input).toHaveValue("preserve this draft");
  });

  it.each([false, true])("settles a sent draft after the run remounts (edited: %s)", async (edited) => {
    vi.mocked(api.listTaskMessages).mockResolvedValue([]);
    const comment = {
      id: "supplement-comment", issue_id: "issue", author_type: "member" as const, author_id: "user",
      content: "Create a rollback note.", type: "comment" as const, parent_id: null, reactions: [], attachments: [],
      created_at: "2026-09-07T00:00:10Z", updated_at: "2026-09-07T00:00:10Z",
      resolved_at: null, resolved_by_type: null, resolved_by_id: null,
      supplement_task_id: id, supplement_status: "pending" as const,
    };
    let resolveRequest!: (value: typeof comment) => void;
    const request = new Promise<typeof comment>((resolve) => { resolveRequest = resolve; });
    vi.mocked(api.createTaskSupplement).mockReturnValue(request);
    const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
    const current = task({ supplement_capability: "task-supplement-v1", can_supplement: true });
    const view = (commentId: string) => <QueryClientProvider client={client}>
      <PlacedInlineCommentRun key={commentId} run={{ task: current, commentId, hasReply: false }} />
    </QueryClientProvider>;
    const rendered = renderWithI18n(view("comment"));
    fireEvent.click(screen.getByRole("button", { name: "Add message" }));
    const input = screen.getByPlaceholderText("Add guidance for this running turn");
    fireEvent.change(input, { target: { value: comment.content } });
    fireEvent.click(screen.getByRole("button", { name: "Send" }));
    await waitFor(() => expect(api.createTaskSupplement).toHaveBeenCalledWith(
      "issue", id, comment.content, expect.any(String),
    ));

    // A realtime comment can re-anchor the run before the send response arrives.
    rendered.rerender(view(comment.id));
    const remountedInput = screen.getByPlaceholderText("Add guidance for this running turn");
    expect(remountedInput).not.toBe(input);
    expect(remountedInput).toHaveValue(comment.content);
    if (edited) fireEvent.change(remountedInput, { target: { value: "A newer unsent draft." } });

    await act(async () => resolveRequest(comment));
    await waitFor(() => expect(client.getMutationCache().getAll()[0]?.state.status).toBe("success"));
    if (edited) {
      expect(screen.getByPlaceholderText("Add guidance for this running turn")).toHaveValue("A newer unsent draft.");
    } else {
      expect(screen.queryByPlaceholderText("Add guidance for this running turn")).not.toBeInTheDocument();
      expect(useTaskSupplementDraftStore.getState().drafts[id]).toBeUndefined();
      fireEvent.click(screen.getByRole("button", { name: "Add message" }));
      expect(screen.getByPlaceholderText("Add guidance for this running turn")).toHaveValue("");
    }
    client.clear();
  });

  it("keeps the task draft through reply placement changes and a full remount", () => {
    vi.mocked(api.listTaskMessages).mockResolvedValue([]);
    const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
    const view = (hasReply: boolean) => <QueryClientProvider client={client}>
      <PlacedInlineCommentRun run={{ task: task({ supplement_capability: "task-supplement-v1", can_supplement: true }), commentId: "reply", hasReply }} presentation="header" />
      <PlacedInlineCommentRun run={{ task: task({ supplement_capability: "task-supplement-v1", can_supplement: true }), commentId: "reply", hasReply }} />
    </QueryClientProvider>;
    const rendered = renderWithI18n(view(false));
    fireEvent.click(screen.getByRole("button", { name: "Add message" }));
    fireEvent.change(screen.getByPlaceholderText("Add guidance for this running turn"), {
      target: { value: "Create remount-proof evidence." },
    });

    rendered.rerender(view(true));
    expect(screen.getByPlaceholderText("Add guidance for this running turn")).toHaveValue("Create remount-proof evidence.");
    rendered.unmount();

    renderWithI18n(view(true));
    expect(screen.getByPlaceholderText("Add guidance for this running turn")).toHaveValue("Create remount-proof evidence.");
    expect(screen.queryByRole("button", { name: "Open full log" })).not.toBeInTheDocument();
  });

  it("keeps a 409 draft and moves it explicitly to the new-message composer", async () => {
    vi.mocked(api.listTaskMessages).mockResolvedValue([]);
    dispatchReasonCodeMock.mockReturnValue("task_supplement_turn_ended");
    vi.mocked(api.createTaskSupplement).mockRejectedValue(new Error("ended"));
    setup(task({ supplement_capability: "task-supplement-v1", can_supplement: true }));
    fireEvent.click(screen.getByRole("button", { name: "Add message" }));
    const input = screen.getByPlaceholderText("Add guidance for this running turn");
    fireEvent.change(input, { target: { value: "Create retained.txt." } });
    fireEvent.click(screen.getByRole("button", { name: "Send" }));
    const move = await screen.findByRole("button", { name: "Move to new message" });
    expect(input).toHaveValue("Create retained.txt.");
    fireEvent.click(move);
    expect(useCommentDraftStore.getState().getDraft("new:issue")).toBe("Create retained.txt.");
    expect(useTaskSupplementDraftStore.getState().drafts[id]).toBeUndefined();
  });

  it.each(["completed", "cancelled", "failed"] as const)("settles a pending receipt immediately when the run becomes %s", async (status) => {
    const entry = {
      id: "supplement-comment", issue_id: "issue", actor_type: "member", actor_id: "user",
      content: "Create receipt evidence.", type: "comment", created_at: "2026-09-07T00:00:10Z",
      supplement_task_id: id, supplement_status: "pending",
    } as TimelineEntry;
    const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
    client.setQueryData(issueKeys.tasks("issue"), [task()]);
    renderWithI18n(<QueryClientProvider client={client}>
      <SupplementReceipt issueId="issue" entry={entry} />
    </QueryClientProvider>);
    expect(screen.getByRole("status")).toHaveTextContent("Waiting for delivery");
    act(() => client.setQueryData(issueKeys.tasks("issue"), [task({ status })]));
    await waitFor(() => expect(screen.getByRole("alert")).toHaveTextContent("Not delivered · the run ended before delivery"));
    expect(screen.queryByRole("button", { name: "Retry" })).not.toBeInTheDocument();
  });

  it("localizes a stable injection failure and retries the same bound comment", async () => {
    vi.mocked(api.retryTaskSupplement).mockResolvedValue();
    vi.mocked(api.listTasksByIssue).mockResolvedValue([task()]);
    const entry = {
      id: "supplement-comment", issue_id: "issue", actor_type: "member", actor_id: "user",
      content: "Create receipt evidence.", type: "comment", created_at: "2026-09-07T00:00:10Z",
      supplement_task_id: id, supplement_status: "failed", supplement_failure_reason: "provider_rejected",
    } as TimelineEntry;
    const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
    client.setQueryData(issueKeys.tasks("issue"), [task()]);
    renderWithI18n(<QueryClientProvider client={client}>
      <SupplementReceipt issueId="issue" entry={entry} />
    </QueryClientProvider>);

    expect(screen.getByRole("alert")).toHaveTextContent("Not delivered · the agent rejected the message");
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    await waitFor(() => expect(api.retryTaskSupplement).toHaveBeenCalledWith("issue", id, "supplement-comment"));
    await waitFor(() => expect(screen.getByRole("button", { name: "Retry" })).not.toBeDisabled());
    act(() => client.setQueryData(issueKeys.tasks("issue"), [task({ status: "completed" })]));
    await waitFor(() => expect(screen.queryByRole("button", { name: "Retry" })).not.toBeInTheDocument());
    expect(screen.getByRole("alert")).toHaveTextContent("Not delivered · the agent rejected the message");
  });

  it("previews streamed agent messages in collapsed steps and expands the full body", async () => {
    const message: TaskMessagePayload = {
      task_id: id, issue_id: "issue", seq: 1, type: "text", content: "Checking the PR",
    };
    vi.mocked(api.listTaskMessages).mockResolvedValue([message]);
    const { client } = setup(task());
    await screen.findByText("Checking the PR");
    fireEvent.click(screen.getByRole("button", { name: /View activity/ }));
    const preview = screen.getAllByText("Checking the PR").find((node) => node.closest("summary"))!;
    expect(preview).toHaveAttribute("title", "Checking the PR");
    expect(screen.queryByTestId("step-body")).not.toBeInTheDocument();
    act(() => client.setQueryData(chatKeys.taskMessages(id), [
      message,
      { ...message, seq: 2, content: " changes.\nReviewing migration safety." },
    ]));
    await waitFor(() => expect(preview).toHaveTextContent("Checking the PR changes."));
    expect(preview).toHaveAttribute("title", "Checking the PR changes.");
    fireEvent.click(preview.closest("summary")!, { detail: 0 });
    expect(await screen.findByTestId("step-body")).toHaveTextContent("Checking the PR changes. Reviewing migration safety.");
  });

  it("shows the first file in a collapsed Grok read_file group and retains its step count", async () => {
    vi.mocked(api.listTaskMessages).mockResolvedValue(
      ["queries.sql", "models.go", "schema.sql", "migrations.go"].flatMap((file, index) => [
        { task_id: id, issue_id: "issue", seq: index * 2 + 1, type: "tool_use" as const,
          tool: "read_file", input: { path: `/workdir/multica/server/db/${file}` } },
        { task_id: id, issue_id: "issue", seq: index * 2 + 2, type: "tool_result" as const,
          tool: "read_file", output: `Contents of ${file}` },
      ]),
    );
    setup(task());
    await screen.findByText(".../db/migrations.go");
    fireEvent.click(screen.getByRole("button", { name: /View activity/ }));
    const preview = screen.getByText("read_file · .../db/queries.sql");
    const summary = preview.closest("summary")!;
    expect(summary).toHaveTextContent("4 steps");
    expect(preview).toHaveAttribute("title", "read_file · .../db/queries.sql");
    fireEvent.click(summary, { detail: 0 });
    expect(await screen.findByText(".../db/queries.sql")).toBeInTheDocument();
    expect(screen.getByText(".../db/models.go")).toBeInTheDocument();
    expect(screen.getByText(".../db/schema.sql")).toBeInTheDocument();
  });

  it("redacts message previews and keeps a label for empty messages", async () => {
    const secret = `ghp_${"x".repeat(36)}`;
    vi.mocked(api.listTaskMessages).mockResolvedValue([
      { task_id: id, issue_id: "issue", seq: 1, type: "text", content: `Checking ${secret}` },
      { task_id: id, issue_id: "issue", seq: 2, type: "thinking", content: "Checking logs" },
      { task_id: id, issue_id: "issue", seq: 3, type: "text", content: " \n " },
    ]);
    setup(task());
    await screen.findByText("Checking logs");
    fireEvent.click(screen.getByRole("button", { name: /View activity/ }));
    expect(screen.getByText("Checking [REDACTED GITHUB TOKEN]")).toHaveAttribute("title", "Checking [REDACTED GITHUB TOKEN]");
    expect(screen.getByText("Agent message").closest("summary")).not.toBeNull();
    expect(document.body.innerHTML).not.toContain(secret);
  });

  it("previews streamed thinking in the header and collapsed steps, and expands its body", async () => {
    const thought: TaskMessagePayload = {
      task_id: id, issue_id: "issue", seq: 1, type: "thinking", content: "Checking the runtime",
    };
    vi.mocked(api.listTaskMessages).mockResolvedValue([thought]);
    const { client } = setup(task());
    const header = await screen.findByText("Checking the runtime");
    const textNode = header.firstChild;
    act(() => client.setQueryData(chatKeys.taskMessages(id), [
      thought,
      { ...thought, seq: 2, content: " logs.\nThe reasoning is present." },
    ]));
    await waitFor(() => expect(header).toHaveTextContent("Checking the runtime logs."));
    expect(header.firstChild).toBe(textNode);
    fireEvent.click(screen.getByRole("button", { name: /View activity/ }));
    const preview = screen.getAllByText("Checking the runtime logs.").find((node) => node.closest("summary"))!;
    expect(preview).toHaveAttribute("title", "Checking the runtime logs.");
    const details = preview.closest("details")!;
    expect(details).not.toHaveAttribute("open");
    expect(screen.queryByTestId("step-body")).not.toBeInTheDocument();
    fireEvent.click(preview.closest("summary")!, { detail: 0 });
    expect(await screen.findByTestId("step-body")).toHaveTextContent("Checking the runtime logs. The reasoning is present.");
  });

  it("redacts thinking before clipping its preview and tooltip", async () => {
    const prefix = "Reviewing ".repeat(18);
    const secret = `ghp_${"x".repeat(36)}`;
    vi.mocked(api.listTaskMessages).mockResolvedValue([
      { task_id: id, issue_id: "issue", seq: 1, type: "thinking", content: `${prefix}${secret}` },
    ]);
    setup(task());
    await screen.findByText(/REDACTED/);
    fireEvent.click(screen.getByRole("button", { name: /View activity/ }));
    const preview = screen.getAllByText(/REDACTED/).find((node) => node.closest("summary"))!;
    expect(preview.getAttribute("title")).toContain("REDACTED");
    expect(document.body.innerHTML).not.toContain("ghp_");
    expect(preview.textContent!.length).toBeLessThanOrEqual(201);
  });

  it("keeps a Thinking label when the runtime supplies no preview text", async () => {
    vi.mocked(api.listTaskMessages).mockResolvedValue([
      { task_id: id, issue_id: "issue", seq: 1, type: "thinking", content: " \n " },
    ]);
    setup(task());
    await screen.findByText("Thinking");
    fireEvent.click(screen.getByRole("button", { name: /View activity/ }));
    expect(screen.getAllByText("Thinking").some((node) => node.closest("summary"))).toBe(true);
  });

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
    expect(progress).not.toHaveClass("shimmer-text");
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

  it("updates streamed summaries without replacing their DOM nodes", async () => {
    const events: TaskMessagePayload[] = [...messages,
      { task_id: id, issue_id: "issue", seq: 3, type: "tool_result", tool: "exec_command", output: "Passed" },
    ];
    vi.mocked(api.listTaskMessages).mockResolvedValue([...events]);
    const { client } = setup(task());
    const summary = await screen.findByText("pnpm test");
    const text = summary.firstChild;
    const container = summary.closest("[data-run-summary]")!;
    const mutations: MutationRecord[] = [];
    const observer = new MutationObserver((records) => mutations.push(...records));
    observer.observe(container, { childList: true, subtree: true });
    try {
      // Cover a new seq with the same label, a changed tool argument, and prose.
      for (const next of [
        { type: "tool_use", tool: "exec_command", input: { command: "pnpm test" } },
        { type: "tool_use", tool: "exec_command", input: { command: "pnpm lint" } },
        { type: "text", content: "Reviewing the results." },
      ] as const) {
        events.push({ task_id: id, issue_id: "issue", seq: events.length + 1, ...next });
        if (next.type === "tool_use") events.push({
          task_id: id, issue_id: "issue", seq: events.length + 1,
          type: "tool_result", tool: next.tool, output: "Passed",
        });
        await act(async () => {
          client.setQueryData(chatKeys.taskMessages(id), [...events]);
          await new Promise((resolve) => setTimeout(resolve, 0));
        });
        const label = next.type === "text" ? next.content : next.input.command;
        await waitFor(() => expect(summary).toHaveTextContent(label));
        expect(summary.isConnected).toBe(true);
        expect(summary.firstChild).toBe(text);
        expect(container).toHaveAttribute("title", label);
      }
      expect([...mutations, ...observer.takeRecords()]).toHaveLength(0);
    } finally {
      observer.disconnect();
    }
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
    setup(task({ status: "completed", result: { comment: "Already posted" } }), true);
    expect(api.listTaskMessages).not.toHaveBeenCalled();
    expect(screen.queryByText("Already posted")).not.toBeInTheDocument();
    expect(screen.getByText("Completed")).toBeInTheDocument();
  });

  it("shows a completed deliverable if the corresponding comment is missing", () => {
    setup(task({ status: "completed", result: { comment: "Review complete." } }));
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

  it("shows who cancelled the run and keeps legacy rows readable", () => {
    const { rerender } = setup(task({
      status: "cancelled",
      completed_at: "2026-09-07T00:01:23Z",
      cancelled_by: { type: "member", id: "user-1", name: "Jiayuan" },
    }));
    expect(screen.getByText("Cancelled by Jiayuan")).toBeInTheDocument();

    rerender(task({ status: "cancelled", completed_at: "2026-09-07T00:01:23Z" }));
    expect(screen.getByText("Cancelled")).toBeInTheDocument();
  });
});
