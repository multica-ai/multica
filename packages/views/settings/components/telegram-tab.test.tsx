// @vitest-environment jsdom

import { type ReactNode } from "react";
import { describe, it, expect, beforeEach, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enSettings from "../../locales/en/settings.json";

type MemberRole = "owner" | "admin" | "member" | "guest";

const membersRef = vi.hoisted(() => ({
  current: [{ user_id: "user-1", role: "owner" as MemberRole }],
}));
const installationsRef = vi.hoisted(() => ({
  current: {
    installations: [] as unknown[],
    configured: true,
    install_supported: true,
  },
}));
const mockRegister = vi.hoisted(() => vi.fn());
const mockDeleteInstallation = vi.hoisted(() => vi.fn());
const mockOpenExternal = vi.hoisted(() => vi.fn());
const mockInvalidate = vi.hoisted(() => vi.fn());
const mockToastError = vi.hoisted(() => vi.fn());
const telegramQueryErrorRef = vi.hoisted(() => ({ current: false }));
const telegramQueryLoadingRef = vi.hoisted(() => ({ current: false }));

vi.mock("@tanstack/react-query", () => ({
  useQuery: (opts: { queryKey: unknown[]; enabled?: boolean }) => {
    if (opts.enabled === false) return { data: undefined, isLoading: false };
    const key = JSON.stringify(opts.queryKey);
    if (key.includes("members")) return { data: membersRef.current, isLoading: false };
    if (key.includes("installations")) {
      return {
        data: telegramQueryLoadingRef.current ? undefined : installationsRef.current,
        isLoading: telegramQueryLoadingRef.current,
        isError: telegramQueryErrorRef.current,
      };
    }
    return { data: undefined, isLoading: false };
  },
  useQueryClient: () => ({ invalidateQueries: mockInvalidate }),
  queryOptions: <T,>(opts: T) => opts,
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "workspace-1" }));

vi.mock("@multica/core/workspace/queries", () => ({
  memberListOptions: () => ({ queryKey: ["members"], queryFn: vi.fn() }),
}));

vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({
    getAgentName: (agentId: string) => `Agent ${agentId}`,
    getMemberName: () => "Unknown",
    getSquadName: () => "Unknown Squad",
    getActorName: () => "Unknown",
    getActorInitials: () => "??",
    getActorAvatarUrl: () => null,
  }),
}));

vi.mock("../../common/actor-avatar", () => ({
  ActorAvatar: ({ actorId }: { actorId: string }) => (
    <span data-testid="actor-avatar" data-actor-id={actorId} />
  ),
}));

vi.mock("@multica/core/telegram", () => ({
  telegramInstallationsOptions: () => ({
    queryKey: ["telegram", "installations"],
    queryFn: vi.fn(),
  }),
  telegramKeys: { installations: (wsId: string) => ["telegram", "installations", wsId] },
}));

vi.mock("@multica/core/api", () => ({
  api: {
    registerTelegramBot: mockRegister,
    deleteTelegramInstallation: mockDeleteInstallation,
  },
}));

vi.mock("@multica/core/auth", () => {
  const useAuthStore = Object.assign(
    (sel?: (s: { user: { id: string } }) => unknown) =>
      sel ? sel({ user: { id: "user-1" } }) : { user: { id: "user-1" } },
    { getState: () => ({ user: { id: "user-1" } }) },
  );
  return { useAuthStore };
});

vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: mockToastError, message: vi.fn() },
}));

vi.mock("../../platform", () => ({ openExternal: mockOpenExternal }));

import { TelegramAgentBindButton, TelegramTab } from "./telegram-tab";

const TEST_RESOURCES = { en: { common: enCommon, settings: enSettings } };

function renderUI(children: ReactNode) {
  return render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      {children}
    </I18nProvider>,
  );
}

function resetFixtures() {
  vi.clearAllMocks();
  membersRef.current = [{ user_id: "user-1", role: "owner" }];
  installationsRef.current = { installations: [], configured: true, install_supported: true };
  telegramQueryErrorRef.current = false;
  telegramQueryLoadingRef.current = false;
}

describe("TelegramAgentBindButton", () => {
  beforeEach(resetFixtures);

  it("opens the connect dialog and submits the pasted bot token", async () => {
    mockRegister.mockResolvedValue({ id: "i1", agent_id: "agent-1", status: "active" });
    renderUI(<TelegramAgentBindButton agentId="agent-1" agentName="Bot" />);
    await userEvent.click(screen.getByTestId("telegram-agent-connect"));
    const tokenInput = await screen.findByTestId("telegram-bot-token");
    expect(tokenInput).toHaveAttribute("type", "password");
    await userEvent.type(tokenInput, "123456789:AAtesttoken");
    await userEvent.click(screen.getByTestId("telegram-connect-submit"));
    await waitFor(() =>
      expect(mockRegister).toHaveBeenCalledWith("workspace-1", "agent-1", {
        bot_token: "123456789:AAtesttoken",
      }),
    );
    expect(mockOpenExternal).not.toHaveBeenCalled();
  });

  it("opens the localized Telegram setup guide", async () => {
    renderUI(<TelegramAgentBindButton agentId="agent-1" agentName="Bot" />);
    await userEvent.click(screen.getByTestId("telegram-agent-connect"));
    await userEvent.click(await screen.findByTestId("telegram-docs-link"));
    expect(mockOpenExternal).toHaveBeenCalledWith(
      "https://multica.ai/docs/telegram-bot-integration",
    );
  });

  it("does not report success for a malformed install response", async () => {
    mockRegister.mockResolvedValue({});
    renderUI(<TelegramAgentBindButton agentId="agent-1" agentName="Bot" />);
    await userEvent.click(screen.getByTestId("telegram-agent-connect"));
    await userEvent.type(await screen.findByTestId("telegram-bot-token"), "123456789:AAtesttoken");
    await userEvent.click(screen.getByTestId("telegram-connect-submit"));
    await waitFor(() => expect(mockToastError).toHaveBeenCalled());
    expect(mockInvalidate).not.toHaveBeenCalled();
  });

  it("shows the connected badge (not the CTA) when the agent already has an active install", () => {
    installationsRef.current = {
      installations: [
        { id: "i1", agent_id: "agent-1", status: "active", bot_username: "my_bot" },
      ],
      configured: true,
      install_supported: true,
    };
    renderUI(<TelegramAgentBindButton agentId="agent-1" />);
    expect(screen.getByTestId("telegram-agent-bot-connected")).toBeTruthy();
    expect(screen.getByTestId("telegram-agent-bot-disconnect")).toBeTruthy();
    expect(screen.queryByTestId("telegram-agent-connect")).toBeNull();
  });

  it("opens the connected bot in Telegram", async () => {
    installationsRef.current = {
      installations: [
        { id: "i1", agent_id: "agent-1", status: "active", bot_username: "my_bot" },
      ],
      configured: true,
      install_supported: true,
    };
    renderUI(<TelegramAgentBindButton agentId="agent-1" />);

    await userEvent.click(screen.getByRole("button", { name: /Open in Telegram/i }));

    expect(mockOpenExternal).toHaveBeenCalledWith("https://t.me/my_bot");
  });

  it("disconnects an agent bot only after confirmation", async () => {
    mockDeleteInstallation.mockResolvedValue(undefined);
    installationsRef.current = {
      installations: [
        { id: "i1", agent_id: "agent-1", status: "active", bot_username: "my_bot" },
      ],
      configured: true,
      install_supported: true,
    };
    renderUI(<TelegramAgentBindButton agentId="agent-1" />);

    await userEvent.click(screen.getByTestId("telegram-agent-bot-disconnect"));
    expect(mockDeleteInstallation).not.toHaveBeenCalled();
    const actions = await screen.findAllByRole("button", { name: /^Disconnect$/i });
    await userEvent.click(actions.at(-1)!);

    await waitFor(() =>
      expect(mockDeleteInstallation).toHaveBeenCalledWith("workspace-1", "i1"),
    );
    expect(mockInvalidate).toHaveBeenCalled();
  });

  it("keeps an agent bot connected when disconnect fails", async () => {
    mockDeleteInstallation.mockRejectedValue(new Error("network failed"));
    installationsRef.current = {
      installations: [
        { id: "i1", agent_id: "agent-1", status: "active", bot_username: "my_bot" },
      ],
      configured: true,
      install_supported: true,
    };
    renderUI(<TelegramAgentBindButton agentId="agent-1" />);

    await userEvent.click(screen.getByTestId("telegram-agent-bot-disconnect"));
    const actions = await screen.findAllByRole("button", { name: /^Disconnect$/i });
    await userEvent.click(actions.at(-1)!);

    await waitFor(() => expect(mockToastError).toHaveBeenCalledWith("network failed"));
    expect(screen.getByTestId("telegram-agent-bot-connected")).toBeTruthy();
    expect(mockInvalidate).not.toHaveBeenCalled();
  });

  it("renders nothing for a non-manager", () => {
    membersRef.current = [{ user_id: "user-1", role: "member" }];
    const { container } = renderUI(<TelegramAgentBindButton agentId="agent-1" />);
    expect(container).toBeEmptyDOMElement();
  });

  it("renders nothing when install is unavailable and the agent is unbound", () => {
    installationsRef.current = { installations: [], configured: true, install_supported: false };
    const { container } = renderUI(<TelegramAgentBindButton agentId="agent-1" />);
    expect(container).toBeEmptyDOMElement();
  });
});

describe("TelegramTab", () => {
  beforeEach(resetFixtures);

  it("shows a loading state without claiming Telegram is disabled", () => {
    telegramQueryLoadingRef.current = true;
    renderUI(<TelegramTab />);
    expect(screen.getByText("Loading…")).toBeTruthy();
    expect(screen.queryByText(/Telegram integration not enabled/i)).toBeNull();
  });

  it("surfaces the not-enabled notice when the deployment has no Telegram key", () => {
    installationsRef.current = { installations: [], configured: false, install_supported: false };
    renderUI(<TelegramTab />);
    expect(screen.getByText(/Telegram integration not enabled/i)).toBeTruthy();
  });

  it("shows the empty state when configured but nothing is connected", () => {
    renderUI(<TelegramTab />);
    expect(screen.getByText(/No bots connected yet/i)).toBeTruthy();
  });

  it("lists a connected installation with its agent name and a disconnect control", () => {
    installationsRef.current = {
      installations: [
        { id: "i1", agent_id: "agent-7", status: "active", bot_username: "my_bot" },
      ],
      configured: true,
      install_supported: true,
    };
    renderUI(<TelegramTab />);
    expect(screen.getByText("Agent agent-7")).toBeTruthy();
    expect(screen.getByText("@my_bot")).toBeTruthy();
    expect(screen.getByText(/Disconnect/i)).toBeTruthy();
  });

  it("disconnects only after confirmation and refreshes the installation list", async () => {
    mockDeleteInstallation.mockResolvedValue(undefined);
    installationsRef.current = {
      installations: [
        { id: "i1", agent_id: "agent-7", status: "active", bot_username: "my_bot" },
      ],
      configured: true,
      install_supported: true,
    };
    renderUI(<TelegramTab />);
    await userEvent.click(screen.getByRole("button", { name: /Disconnect/i }));
    expect(mockDeleteInstallation).not.toHaveBeenCalled();
    const actions = await screen.findAllByRole("button", { name: /^Disconnect$/i });
    await userEvent.click(actions.at(-1)!);
    await waitFor(() =>
      expect(mockDeleteInstallation).toHaveBeenCalledWith("workspace-1", "i1"),
    );
    expect(mockInvalidate).toHaveBeenCalled();
  });

  it("keeps the installation visible when disconnect fails", async () => {
    mockDeleteInstallation.mockRejectedValue(new Error("network failed"));
    installationsRef.current = {
      installations: [
        { id: "i1", agent_id: "agent-7", status: "active", bot_username: "my_bot" },
      ],
      configured: true,
      install_supported: true,
    };
    renderUI(<TelegramTab />);
    await userEvent.click(screen.getByRole("button", { name: /Disconnect/i }));
    const actions = await screen.findAllByRole("button", { name: /^Disconnect$/i });
    await userEvent.click(actions.at(-1)!);
    await waitFor(() => expect(mockToastError).toHaveBeenCalledWith("network failed"));
    expect(screen.getByText("@my_bot")).toBeTruthy();
    expect(mockInvalidate).not.toHaveBeenCalled();
  });

  // Malformed-response defense (CLAUDE.md → API Compatibility): a response
  // missing `installations` must not crash the panel.
  it("tolerates a malformed installations response", () => {
    installationsRef.current = { configured: true } as never;
    renderUI(<TelegramTab />);
    expect(screen.getByText(/No bots connected yet/i)).toBeTruthy();
  });

  it("shows a load error instead of pretending Telegram is disabled", () => {
    telegramQueryErrorRef.current = true;
    renderUI(<TelegramTab />);
    expect(screen.getByText(/Failed to load Telegram installations/i)).toBeTruthy();
    expect(screen.queryByText(/Telegram integration not enabled/i)).toBeNull();
  });
});
