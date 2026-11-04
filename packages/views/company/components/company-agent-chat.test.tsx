/**
 * @vitest-environment jsdom
 *
 * The conversation itself belongs to the chat controller and is tested with it;
 * it is replaced here. These tests cover what the pane decides: starting one new
 * conversation for the agent, the earlier ones on offer, and what "create issue"
 * sends.
 */
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { Agent, ChatSession } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { CompanyAgentChat } from "./company-agent-chat";

const ADA = { id: "ada", name: "Ada" } as Agent;
const BOB = { id: "bob", name: "Bob" } as Agent;

const controller = vi.hoisted(() => ({
  value: {} as Record<string, unknown>,
  startNewChat: vi.fn(),
  selectSession: vi.fn(),
  quickCreate: vi.fn(),
  toast: { success: vi.fn(), error: vi.fn() },
}));

vi.mock("sonner", () => ({ toast: controller.toast }));
vi.mock("@multica/core/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/api")>()),
  api: { quickCreateIssue: controller.quickCreate },
}));
vi.mock("@multica/core/chat/use-quick-actions-pending-timeout", () => ({ useQuickActionsPendingTimeout: () => {} }));
vi.mock("../../chat/components/use-quick-actions-failure-toast", () => ({ useQuickActionsFailureToast: () => {} }));
vi.mock("../../chat/components/use-chat-controller", () => ({ useChatController: () => controller.value }));
vi.mock("../../chat/components/chat-conversation", () => ({
  ChatConversation: ({ header }: { header: React.ReactNode }) => (
    <div>
      {header}
      <span>conversation</span>
    </div>
  ),
}));

function session(id: string, agentId: string, title: string): ChatSession {
  return { id, agent_id: agentId, title } as ChatSession;
}

function useController(overrides: Record<string, unknown> = {}) {
  controller.value = {
    availableAgents: [ADA, BOB],
    agentsSettled: true,
    sessions: [session("s1", "ada", "Plan the sprint"), session("s2", "bob", "Bob's thing"), session("s3", "ada", "")],
    activeSessionId: null,
    handleStartNewChat: controller.startNewChat,
    handleSelectSession: controller.selectSession,
    handleProjectChange: () => {},
    ...overrides,
  };
}

function setup(props: Partial<Parameters<typeof CompanyAgentChat>[0]> = {}) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={queryClient}>
      <CompanyAgentChat agent={ADA} active {...props} />
    </QueryClientProvider>,
  );
}

describe("CompanyAgentChat", () => {
  beforeEach(() => {
    controller.startNewChat.mockReset();
    controller.selectSession.mockReset();
    controller.quickCreate.mockReset().mockResolvedValue({ task_id: "t1" });
    controller.toast.success.mockReset();
    controller.toast.error.mockReset();
    useController();
  });

  afterEach(() => {
    cleanup();
  });

  it("starts one new conversation with the agent", () => {
    const { rerender } = setup();

    expect(controller.startNewChat).toHaveBeenCalledTimes(1);
    expect(controller.startNewChat).toHaveBeenCalledWith(ADA);
    expect(screen.getByText("conversation")).toBeTruthy();

    // Re-rendering does not start it over.
    rerender(
      <QueryClientProvider client={new QueryClient()}>
        <CompanyAgentChat agent={ADA} active />
      </QueryClientProvider>,
    );
    expect(controller.startNewChat).toHaveBeenCalledTimes(1);
  });

  it("says so when the agent cannot be chatted with", () => {
    useController({ availableAgents: [BOB] });

    setup();

    expect(screen.getByText("This agent can't be chatted with right now")).toBeTruthy();
    expect(controller.startNewChat).not.toHaveBeenCalled();
  });

  it("offers the agent's earlier chats, and only its own", async () => {
    setup();
    const user = userEvent.setup();

    await user.click(screen.getByRole("button", { name: /Earlier chats/ }));

    expect(await screen.findByRole("menuitem", { name: "Plan the sprint" })).toBeTruthy();
    expect(screen.queryByRole("menuitem", { name: "Bob's thing" })).toBeNull();
    // A chat with no title still has a row.
    expect(screen.getAllByRole("menuitem").length).toBeGreaterThanOrEqual(3);

    await user.click(screen.getByRole("menuitem", { name: "Plan the sprint" }));

    expect(controller.selectSession).toHaveBeenCalledWith(expect.objectContaining({ id: "s1" }));
  });

  it("starts a fresh conversation on request", () => {
    setup();
    controller.startNewChat.mockClear();

    fireEvent.click(screen.getByRole("button", { name: "New chat" }));

    expect(controller.startNewChat).toHaveBeenCalledWith(ADA);
  });

  it("sends a request for the agent to write up as an issue", async () => {
    setup({ projectId: "proj-1" });
    fireEvent.click(screen.getByRole("tab", { name: "Create issue" }));
    const box = screen.getByRole("textbox", { name: /Describe the issue/ });

    fireEvent.change(box, { target: { value: "  Fix the login bug  " } });
    fireEvent.click(screen.getByRole("button", { name: "Send" }));

    await waitFor(() =>
      expect(controller.quickCreate).toHaveBeenCalledWith({
        agent_id: "ada",
        prompt: "Fix the login bug",
        project_id: "proj-1",
      }),
    );
    await waitFor(() => expect((box as HTMLTextAreaElement).value).toBe(""));
    expect(screen.getByText("Fix the login bug")).toBeTruthy();
    expect(controller.toast.success).toHaveBeenCalled();
  });

  it("keeps the request when it cannot be sent", async () => {
    controller.quickCreate.mockRejectedValue(new Error("offline"));
    setup();
    fireEvent.click(screen.getByRole("tab", { name: "Create issue" }));
    const box = screen.getByRole("textbox", { name: /Describe the issue/ });
    fireEvent.change(box, { target: { value: "Try me" } });

    fireEvent.click(screen.getByRole("button", { name: "Send" }));

    await waitFor(() => expect(controller.toast.error).toHaveBeenCalled());
    expect((box as HTMLTextAreaElement).value).toBe("Try me");
  });

  it("does not send an empty request", () => {
    setup();
    fireEvent.click(screen.getByRole("tab", { name: "Create issue" }));

    expect(screen.getByRole("button", { name: "Send" }).hasAttribute("disabled")).toBe(true);
  });

  it("keeps the conversation mounted while creating an issue", () => {
    setup();
    fireEvent.click(screen.getByRole("tab", { name: "Create issue" }));

    expect(screen.getByText("conversation")).toBeTruthy();
    fireEvent.click(screen.getByRole("tab", { name: "Chat" }));
    expect(controller.startNewChat).toHaveBeenCalledTimes(1);
  });
});
