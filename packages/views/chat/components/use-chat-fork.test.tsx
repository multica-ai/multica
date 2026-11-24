import { act, renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import type { Agent, ChatSession } from "@multica/core/types";
import type { PropsWithChildren } from "react";
import { toast } from "sonner";
import { beforeEach, describe, expect, it, vi } from "vitest";
import enChat from "../../locales/en/chat.json";
import { useChatFork } from "./use-chat-fork";

const { getChatFork } = vi.hoisted(() => ({ getChatFork: vi.fn() }));
vi.mock("@multica/core/api", () => ({
  api: {
    listRuntimes: async () => [{
      id: "runtime",
      provider: "codex",
      runtime_mode: "local",
      metadata: { capabilities: ["codex-chat-fork-v1"] },
    }],
    createChatFork: async () => ({ task_id: "task", status: "queued" }),
    getChatFork,
  },
}));
vi.mock("sonner", () => ({
  toast: { info: vi.fn(() => "fork-toast"), error: vi.fn(), success: vi.fn() },
}));

const session: ChatSession = {
  id: "parent",
  workspace_id: "workspace",
  agent_id: "agent",
  creator_id: "user",
  title: "Source chat",
  status: "active",
  has_unread: false,
  created_at: new Date(0).toISOString(),
  updated_at: new Date(0).toISOString(),
};
const agent = { id: "agent", runtime_id: "runtime" } as Agent;

async function startFork() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const onOpen = vi.fn();
  function Wrapper({ children }: PropsWithChildren) {
    return (
      <QueryClientProvider client={queryClient}>
        <I18nProvider locale="en" resources={{ en: { chat: enChat } }}>
          {children}
        </I18nProvider>
      </QueryClientProvider>
    );
  }
  const { result } = renderHook(
    () => useChatFork(session.workspace_id, session, agent, true, onOpen),
    { wrapper: Wrapper },
  );
  await waitFor(() => expect(result.current.canFork).toBe(true));
  act(() => result.current.start());
  await waitFor(() => expect(toast.info).toHaveBeenCalledWith(enChat.header.fork_pending));
}

describe("useChatFork notifications", () => {
  beforeEach(() => vi.clearAllMocks());

  it("replaces the pending notification with the completed branch action", async () => {
    getChatFork.mockResolvedValue({ task_id: "task", status: "completed", chat_id: "child" });

    await startFork();

    await waitFor(() => expect(toast.success).toHaveBeenCalledWith(
      enChat.header.fork_ready,
      expect.objectContaining({
        id: "fork-toast",
        action: expect.objectContaining({ label: enChat.header.open_fork, onClick: expect.any(Function) }),
      }),
    ));

  });

  it("replaces the pending notification when the fork fails", async () => {
    getChatFork.mockResolvedValue({ task_id: "task", status: "failed" });

    await startFork();

    await waitFor(() => expect(toast.error).toHaveBeenCalledWith(enChat.header.fork_failed, { id: "fork-toast" }));
  });
});
