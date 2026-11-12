import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, screen, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { chatSessionsOptions, pendingChatTasksOptions } from "@multica/core/chat/queries";
import type { ChatSession } from "@multica/core/types";
import { NavigationProvider, type NavigationAdapter } from "../../navigation";
import { renderWithI18n } from "../../test/i18n";
import { ChatBar } from "./chat-bar";

const WS = "ws-test";

const h = vi.hoisted(() => ({
  chat: {
    isOpen: false,
    activeSessionId: null as string | null,
    selectedAgentId: "agent-1" as string | null,
    chatBarKept: [] as string[],
    chatBarDismissed: {} as Record<string, string>,
    setOpen: vi.fn(),
    setActiveSession: vi.fn(),
    setSelectedAgentId: vi.fn(),
    startNewChat: vi.fn(),
    keepChatInBar: vi.fn(),
    dismissChatFromBar: vi.fn(),
  },
}));

vi.mock("@multica/core/chat", () => ({
  useChatStore: Object.assign(
    (selector: (state: typeof h.chat) => unknown) => selector(h.chat),
    { getState: () => h.chat },
  ),
}));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-test" }));
vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({ chat: () => "/acme/chat" }),
}));
vi.mock("@multica/core/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/api")>()),
  api: { getBaseUrl: () => "http://localhost" },
}));

const adapter: NavigationAdapter = {
  push: vi.fn(),
  replace: vi.fn(),
  back: vi.fn(),
  pathname: "/acme/issues",
  searchParams: new URLSearchParams(),
  hash: "",
  getShareableUrl: (path) => path,
};

function session(id: string, minute: number, overrides: Partial<ChatSession> = {}): ChatSession {
  const at = new Date(Date.UTC(2026, 10, 11, 8, minute)).toISOString();
  return {
    id,
    workspace_id: WS,
    agent_id: "agent-1",
    creator_id: "user-1",
    title: id,
    status: "active",
    has_unread: false,
    created_at: at,
    updated_at: at,
    ...overrides,
  };
}

function renderBar(sessions: ChatSession[], running: string[] = []) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  });
  qc.setQueryData(chatSessionsOptions(WS).queryKey, sessions);
  qc.setQueryData(pendingChatTasksOptions(WS).queryKey, {
    tasks: running.map((id) => ({ task_id: `task-${id}`, status: "running", chat_session_id: id })),
  });
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <NavigationProvider value={adapter}>
        <ChatBar />
      </NavigationProvider>
    </QueryClientProvider>,
  );
}

const SESSIONS = [
  session("Read and done", 1),
  session("Billing progress", 2, { agent_id: "agent-2" }),
  session("Weekly report", 3, { has_unread: true }),
];

beforeEach(() => {
  vi.clearAllMocks();
  h.chat.isOpen = false;
  h.chat.activeSessionId = null;
  h.chat.selectedAgentId = "agent-1";
  h.chat.chatBarKept = [];
  h.chat.chatBarDismissed = {};
});

describe("ChatBar", () => {
  it("shows a chip for each running or unread chat, and none for the rest", () => {
    renderBar(SESSIONS, [SESSIONS[1]!.id]);

    expect(screen.getByRole("button", { name: "Billing progress, Running" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Weekly report, Unread" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /Read and done/ })).not.toBeInTheDocument();
  });

  it("opens the chat window on the chip's chat, with that chat's agent", () => {
    renderBar(SESSIONS, [SESSIONS[1]!.id]);

    fireEvent.click(screen.getByRole("button", { name: "Billing progress, Running" }));

    expect(h.chat.setSelectedAgentId).toHaveBeenCalledWith("agent-2");
    expect(h.chat.setActiveSession).toHaveBeenCalledWith("Billing progress");
    expect(h.chat.keepChatInBar).toHaveBeenCalledWith("Billing progress");
    expect(h.chat.setOpen).toHaveBeenCalledWith(true);
  });

  it("switches the open window to another chip's chat without closing it", () => {
    h.chat.isOpen = true;
    h.chat.activeSessionId = "Billing progress";
    h.chat.selectedAgentId = "agent-2";
    renderBar(SESSIONS, [SESSIONS[1]!.id]);

    fireEvent.click(screen.getByRole("button", { name: "Weekly report, Unread" }));

    expect(h.chat.setSelectedAgentId).toHaveBeenCalledWith("agent-1");
    expect(h.chat.setActiveSession).toHaveBeenCalledWith("Weekly report");
    expect(h.chat.setOpen).toHaveBeenCalledWith(true);
  });

  it("closes the window from the chip it is open on", () => {
    h.chat.isOpen = true;
    h.chat.activeSessionId = "Weekly report";
    renderBar(SESSIONS);

    const chip = screen.getByRole("button", { name: "Weekly report, Unread" });
    expect(chip).toHaveAttribute("aria-pressed", "true");
    expect(screen.getByRole("button", { name: "Ask Multica" })).toHaveAttribute("aria-pressed", "false");

    fireEvent.click(chip);

    expect(h.chat.setOpen).toHaveBeenCalledWith(false);
    expect(h.chat.setActiveSession).not.toHaveBeenCalled();
  });

  it("keeps the chip of a chat opened here once it is read and closed", () => {
    h.chat.chatBarKept = ["Read and done"];
    renderBar(SESSIONS);

    expect(screen.getByRole("button", { name: "Read and done" })).toHaveAttribute("aria-pressed", "false");
  });

  it("gives whatever chat the window shows a chip", () => {
    h.chat.isOpen = true;
    h.chat.activeSessionId = "Read and done";
    renderBar(SESSIONS);

    expect(screen.getByRole("button", { name: "Read and done" })).toHaveAttribute("aria-pressed", "true");
    expect(h.chat.keepChatInBar).toHaveBeenCalledWith("Read and done");
  });

  it("closes a chip, and the window with it when it is open on that chat", () => {
    h.chat.isOpen = true;
    h.chat.activeSessionId = "Weekly report";
    renderBar(SESSIONS);

    fireEvent.click(screen.getByRole("button", { name: "Close Weekly report" }));

    expect(h.chat.setOpen).toHaveBeenCalledWith(false);
    expect(h.chat.dismissChatFromBar).toHaveBeenCalledWith("Weekly report", SESSIONS[2]!.updated_at);
  });

  it("closes another chat's chip without touching the window", () => {
    h.chat.isOpen = true;
    h.chat.activeSessionId = "Weekly report";
    renderBar(SESSIONS, [SESSIONS[1]!.id]);

    fireEvent.click(screen.getByRole("button", { name: "Close Billing progress" }));

    expect(h.chat.setOpen).not.toHaveBeenCalled();
    expect(h.chat.dismissChatFromBar).toHaveBeenCalledWith("Billing progress", SESSIONS[1]!.updated_at);
  });

  it("starts a new chat from Ask Multica, leaving the open chat behind", () => {
    h.chat.isOpen = true;
    h.chat.activeSessionId = "Weekly report";
    renderBar(SESSIONS);

    const ask = screen.getByRole("button", { name: "Ask Multica" });
    expect(ask).toHaveAttribute("aria-pressed", "false");
    fireEvent.click(ask);

    expect(h.chat.startNewChat).toHaveBeenCalledTimes(1);
    expect(h.chat.setOpen).not.toHaveBeenCalled();
  });

  it("closes the window from Ask Multica while it shows the new chat", () => {
    h.chat.isOpen = true;
    h.chat.activeSessionId = null;
    renderBar(SESSIONS);

    const ask = screen.getByRole("button", { name: "Ask Multica" });
    expect(ask).toHaveAttribute("aria-pressed", "true");
    fireEvent.click(ask);

    expect(h.chat.setOpen).toHaveBeenCalledWith(false);
    expect(h.chat.startNewChat).not.toHaveBeenCalled();
  });

  it("opens a chat from the history, grouped by when it last moved", async () => {
    vi.useFakeTimers({ toFake: ["Date"], now: new Date(2026, 10, 11, 20) });
    try {
      renderBar([
        session("From today", 1, { updated_at: new Date(2026, 10, 11, 9).toISOString() }),
        session("From this week", 2, { updated_at: new Date(2026, 10, 8, 9).toISOString() }),
        session("From long ago", 3, { updated_at: new Date(2026, 6, 1, 9).toISOString() }),
        session("Archived", 4, { status: "archived" }),
      ]);

      fireEvent.click(screen.getByRole("button", { name: "Chat history" }));

      const today = await screen.findByRole("group", { name: "Today" });
      expect(within(today).getByRole("button", { name: /From today/ })).toBeInTheDocument();
      expect(
        within(screen.getByRole("group", { name: "Last 7 days" })).getByRole("button", { name: /From this week/ }),
      ).toBeInTheDocument();
      expect(
        within(screen.getByRole("group", { name: "Older" })).getByRole("button", { name: /From long ago/ }),
      ).toBeInTheDocument();
      expect(screen.queryByRole("button", { name: /Archived/ })).not.toBeInTheDocument();
      expect(screen.getByRole("link", { name: "All chats" })).toHaveAttribute("href", "/acme/chat");

      fireEvent.click(screen.getByRole("button", { name: /From this week/ }));

      expect(h.chat.keepChatInBar).toHaveBeenCalledWith("From this week");
      expect(h.chat.setActiveSession).toHaveBeenCalledWith("From this week");
      expect(h.chat.setOpen).toHaveBeenCalledWith(true);
    } finally {
      vi.useRealTimers();
    }
  });
});
