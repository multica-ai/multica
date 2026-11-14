import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import { useAppearanceStore, useInstalledThemesStore, useThemeFolderStatus } from "@multica/core/appearance";
import enChat from "../../locales/en/chat.json";
import enCommon from "../../locales/en/common.json";
import enSettings from "../../locales/en/settings.json";

const h = vi.hoisted(() => ({
  sendChatMessage: vi.fn(),
  createSession: vi.fn(),
  push: vi.fn(),
  seed: vi.fn(),
  agents: [
    { id: "agent-a", name: "Ada", archived_at: null },
    { id: "agent-b", name: "Lambda", archived_at: null },
  ],
}));

vi.mock("@multica/core/api", () => ({
  api: { sendChatMessage: h.sendChatMessage },
  dispatchReasonCode: () => null,
}));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({ chatSession: (id: string) => `/acme/chat?session=${id}` }),
}));
vi.mock("@multica/core/auth", () => ({
  useAuthStore: (select: (s: { user: { id: string } }) => unknown) => select({ user: { id: "user-1" } }),
}));
vi.mock("@multica/core/chat", () => ({
  useChatStore: (select: (s: { selectedAgentId: string | null }) => unknown) => select({ selectedAgentId: "agent-b" }),
}));
vi.mock("@multica/core/chat/mutations", () => ({
  useCreateChatSession: () => ({ mutateAsync: h.createSession }),
}));
vi.mock("@multica/core/chat/session-list", () => ({ refetchChatSessionsAfterCommit: vi.fn() }));
vi.mock("@multica/core/workspace/queries", () => ({
  agentListOptions: () => ({ queryKey: ["agents"], queryFn: () => h.agents }),
  memberListOptions: () => ({ queryKey: ["members"], queryFn: () => [{ user_id: "user-1", role: "member" }] }),
}));
vi.mock("../../chat/components/use-chat-controller", () => ({ seedAcceptedPendingTask: h.seed }));
vi.mock("../../issues/components/pickers/assignee-picker", () => ({ canAssignAgent: () => true }));
vi.mock("../../common/actor-avatar", () => ({ ActorAvatar: () => null }));
vi.mock("../../navigation", () => ({ useNavigation: () => ({ push: h.push }) }));

import { ThemeAgentDialog } from "./theme-agent-dialog";

const RESOURCES = { en: { common: enCommon, settings: enSettings, chat: enChat } };

function Wrapper({ children }: { children: ReactNode }) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return (
    <QueryClientProvider client={client}>
      <I18nProvider locale="en" resources={RESOURCES}>
        {children}
      </I18nProvider>
    </QueryClientProvider>
  );
}

describe("ThemeAgentDialog", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    useAppearanceStore.setState({ theme: "default" });
    useInstalledThemesStore.setState({ themes: [], folders: {}, fromStore: {} });
    useThemeFolderStatus.setState({ path: null, problems: [], awaiting: null });
    h.createSession.mockResolvedValue({ id: "session-1", agent_id: "agent-b" });
    h.sendChatMessage.mockResolvedValue({
      message_id: "message-1",
      task_id: "task-1",
      created_at: "2026-11-14T00:00:00Z",
    });
  });
  afterEach(cleanup);

  it("starts a chat with the last agent, carrying the theme contract and the themes folder", async () => {
    useThemeFolderStatus.setState({ path: "/Users/ada/Multica/themes" });
    const onOpenChange = vi.fn();
    const user = userEvent.setup();
    render(<ThemeAgentDialog open onOpenChange={onOpenChange} />, { wrapper: Wrapper });

    const submit = screen.getByRole("button", { name: "Start chat" });
    expect(submit).toBeDisabled();
    await user.type(screen.getByLabelText("What should it look like?"), "Night sky behind frosted glass");
    await waitFor(() => expect(screen.getByRole("combobox", { name: "Agent" })).toHaveTextContent("Lambda"));
    await user.click(submit);

    await waitFor(() => expect(h.push).toHaveBeenCalledWith("/acme/chat?session=session-1"));
    expect(h.createSession).toHaveBeenCalledWith({ agent_id: "agent-b", title: "Theme: night-sky-behind-frosted" });
    const [sessionId, content] = h.sendChatMessage.mock.calls[0]!;
    expect(sessionId).toBe("session-1");
    expect(content).toContain("Night sky behind frosted glass");
    expect(content).toContain("`/Users/ada/Multica/themes/night-sky-behind-frosted`");
    expect(content).toContain("scripts/theme-check.mjs");
    expect(h.seed).toHaveBeenCalledWith(expect.anything(), "session-1", expect.objectContaining({ task_id: "task-1" }));
    expect(useThemeFolderStatus.getState().awaiting).toBe("night-sky-behind-frosted");
    expect(onOpenChange).toHaveBeenCalledWith(false);
  });

  it("asks for a zip on web and waits for no folder", async () => {
    const user = userEvent.setup();
    render(<ThemeAgentDialog open onOpenChange={vi.fn()} />, { wrapper: Wrapper });

    expect(screen.getByText(/attaches the theme as a \.zip/)).toBeInTheDocument();
    await user.type(screen.getByLabelText("What should it look like?"), "Paper");
    await waitFor(() => expect(screen.getByRole("button", { name: "Start chat" })).toBeEnabled());
    await user.click(screen.getByRole("button", { name: "Start chat" }));

    await waitFor(() => expect(h.sendChatMessage).toHaveBeenCalled());
    expect(h.sendChatMessage.mock.calls[0]![1]).toContain("`paper.zip`");
    expect(useThemeFolderStatus.getState().awaiting).toBeNull();
  });
});
