/**
 * @vitest-environment jsdom
 *
 * The panel is the one place an agent is managed from. Its parts (chat, issue
 * thread, new-issue form) have their own tests, so they are replaced by stubs
 * here; these tests cover what the panel decides: what it opens on, how the work
 * list is laid out, and how a choice on one side shows on the other.
 */
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { workspaceKeys } from "@multica/core/workspace/queries";
import type { OfficeRoom } from "@multica/core/agents";
import type { Agent, Issue } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { OfficeAgentPanel } from "./office-agent-panel";

const update = vi.hoisted(() => ({ mutate: vi.fn() }));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/issues/mutations", () => ({ useUpdateIssue: () => ({ mutate: update.mutate, isPending: false }) }));
vi.mock("../../common/actor-avatar", () => ({ ActorAvatar: () => <span data-testid="avatar" /> }));
vi.mock("./office-agent-chat", () => ({
  OfficeAgentChat: ({ agent, active }: { agent: Agent; active: boolean }) => (
    <div data-testid="chat" data-active={String(active)}>{`chat with ${agent.name}`}</div>
  ),
}));
vi.mock("./office-agent-new-issue", () => ({
  OfficeAgentNewIssue: ({ onCreated, onCancel }: { onCreated: (id: string) => void; onCancel: () => void }) => (
    <div>
      new issue form
      <button type="button" onClick={() => onCreated("fresh")}>
        stub create
      </button>
      <button type="button" onClick={onCancel}>
        stub cancel
      </button>
    </div>
  ),
}));
vi.mock("../../issues/components/issue-detail", () => ({
  IssueDetail: ({ issueId, leadingAction }: { issueId: string; leadingAction?: React.ReactNode }) => (
    <div>
      {leadingAction}
      <span>{`thread ${issueId}`}</span>
    </div>
  ),
}));

const NAMES: Record<OfficeRoom, string> = {
  product: "Product development",
  research: "Market research",
  planning: "Planning",
  general: "General",
};
const OCCUPANCY: Record<OfficeRoom, number> = { product: 1, research: 4, planning: 0, general: 2 };

function makeIssue(id: string, status: string, assignee?: string): Issue {
  return {
    id,
    identifier: `MUL-${id}`,
    title: `Task ${id}`,
    status,
    priority: "none",
    assignee_type: assignee ? "agent" : null,
    assignee_id: assignee ?? null,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
  } as Issue;
}

const ISSUES = [
  makeIssue("run", "in_progress", "ada"),
  makeIssue("queued", "todo", "ada"),
  makeIssue("finished", "done", "ada"),
  makeIssue("free", "backlog"),
];

describe("OfficeAgentPanel", () => {
  let queryClient: QueryClient;

  beforeEach(() => {
    update.mutate.mockReset();
    queryClient = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
    queryClient.setQueryData(workspaceKeys.agents("ws-1"), [{ id: "ada", name: "Ada" } as Agent]);
  });

  afterEach(() => {
    cleanup();
  });

  function setup(overrides: Partial<Parameters<typeof OfficeAgentPanel>[0]> = {}) {
    const props = {
      target: { agentId: "ada" },
      issues: ISSUES,
      currentRoom: () => "general" as OfficeRoom,
      occupancy: OCCUPANCY,
      roomNames: NAMES,
      onMoveRoom: vi.fn(() => Promise.resolve()),
      onClose: vi.fn(),
      ...overrides,
    };
    renderWithI18n(
      <QueryClientProvider client={queryClient}>
        <OfficeAgentPanel {...props} />
      </QueryClientProvider>,
    );
    return props;
  }

  it("opens on the agent's chat, under its name and room", () => {
    setup();

    expect(screen.getByRole("dialog")).toBeTruthy();
    expect(screen.getByText("Ada")).toBeTruthy();
    expect(screen.getByText("chat with Ada")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Room" }).textContent).toContain("General");
  });

  it("is closed while there is no target", () => {
    setup({ target: null });

    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("lays the work out by what the agent is doing, has queued, could be given and has finished", () => {
    setup();
    const work = screen.getByRole("complementary", { name: "Work" });

    expect(within(work).getByRole("region", { name: "In progress" }).textContent).toContain("MUL-run");
    expect(within(work).getByRole("region", { name: "Queued" }).textContent).toContain("MUL-queued");
    expect(within(work).getByRole("region", { name: "Available to assign" }).textContent).toContain("MUL-free");
    // Finished work is collapsed until asked for.
    expect(within(work).queryByText("MUL-finished")).toBeNull();

    fireEvent.click(within(work).getByRole("button", { name: /Done/ }));

    expect(within(work).getByText("MUL-finished")).toBeTruthy();
  });

  it("opens an issue's thread from the list, and goes back to the same chat", () => {
    setup();

    fireEvent.click(screen.getByRole("button", { name: /MUL-run/ }));

    expect(screen.getByText("thread run")).toBeTruthy();
    // The chat is still there behind the thread, not started over.
    expect(screen.getByTestId("chat").getAttribute("data-active")).toBe("false");

    fireEvent.click(screen.getByRole("button", { name: "Back to chat" }));

    expect(screen.queryByText("thread run")).toBeNull();
    expect(screen.getByTestId("chat").getAttribute("data-active")).toBe("true");
  });

  it("opens straight on an issue's thread when asked to", () => {
    setup({ target: { agentId: "ada", issueId: "queued" } });

    expect(screen.getByText("thread queued")).toBeTruthy();
  });

  it("hands over an issue nobody has started", () => {
    setup();

    fireEvent.click(screen.getByRole("button", { name: "Assign" }));

    expect(update.mutate).toHaveBeenCalledWith(
      { id: "free", assignee_type: "agent", assignee_id: "ada" },
      expect.any(Object),
    );
  });

  it("filters the list by what is typed", () => {
    setup();

    fireEvent.change(screen.getByRole("textbox", { name: "Search issues" }), { target: { value: "queued" } });

    expect(screen.getByText("MUL-queued")).toBeTruthy();
    expect(screen.queryByText("MUL-run")).toBeNull();
    expect(screen.queryByText("MUL-free")).toBeNull();
  });

  it("opens the new-issue form, and the issue it creates on its thread", () => {
    setup();

    fireEvent.click(screen.getByRole("button", { name: "New issue" }));
    expect(screen.getByText("new issue form")).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: "stub create" }));

    expect(screen.getByText("thread fresh")).toBeTruthy();
  });

  it("cancels the new-issue form back to the chat", () => {
    setup();
    fireEvent.click(screen.getByRole("button", { name: "New issue" }));

    fireEvent.click(screen.getByRole("button", { name: "stub cancel" }));

    expect(screen.queryByText("new issue form")).toBeNull();
    expect(screen.getByTestId("chat").getAttribute("data-active")).toBe("true");
  });

  it("moves the agent to another room", async () => {
    const props = setup();
    const user = userEvent.setup();

    await user.click(screen.getByRole("button", { name: "Room" }));
    await user.click(await screen.findByRole("menuitem", { name: /Planning/ }));

    expect(props.onMoveRoom).toHaveBeenCalledWith("ada", "planning");
  });
});
