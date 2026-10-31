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
const queryErrorRef = vi.hoisted(() => ({ current: false }));
const queryLoadingRef = vi.hoisted(() => ({ current: false }));

vi.mock("@tanstack/react-query", () => ({
  useQuery: (opts: { queryKey: unknown[]; enabled?: boolean }) => {
    if (opts.enabled === false) return { data: undefined, isLoading: false };
    const key = JSON.stringify(opts.queryKey);
    if (key.includes("members")) return { data: membersRef.current, isLoading: false };
    if (key.includes("installations")) {
      return {
        data: queryLoadingRef.current ? undefined : installationsRef.current,
        isLoading: queryLoadingRef.current,
        isError: queryErrorRef.current,
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

vi.mock("@multica/core/discord", () => ({
  discordInstallationsOptions: () => ({
    queryKey: ["discord", "installations"],
    queryFn: vi.fn(),
  }),
  discordKeys: { installations: (wsId: string) => ["discord", "installations", wsId] },
}));

vi.mock("@multica/core/api", () => ({
  api: {
    registerDiscordBot: mockRegister,
    deleteDiscordInstallation: mockDeleteInstallation,
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

import { DiscordAgentBindButton, DiscordTab } from "./discord-tab";

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
  queryErrorRef.current = false;
  queryLoadingRef.current = false;
}

describe("DiscordAgentBindButton", () => {
  beforeEach(resetFixtures);

  it("opens the connect dialog and submits the pasted bot token", async () => {
    mockRegister.mockResolvedValue({ id: "i1", agent_id: "agent-1", status: "active" });
    renderUI(<DiscordAgentBindButton agentId="agent-1" agentName="Bot" />);
    await userEvent.click(screen.getByTestId("discord-agent-connect"));
    const tokenInput = await screen.findByTestId("discord-bot-token");
    expect(tokenInput).toHaveAttribute("type", "password");
    await userEvent.type(tokenInput, "MTIz.abc.def");
    await userEvent.click(screen.getByTestId("discord-connect-submit"));
    await waitFor(() =>
      expect(mockRegister).toHaveBeenCalledWith("workspace-1", "agent-1", {
        bot_token: "MTIz.abc.def",
      }),
    );
    expect(mockInvalidate).toHaveBeenCalled();
  });

  it("opens the localized Discord setup guide", async () => {
    renderUI(<DiscordAgentBindButton agentId="agent-1" agentName="Bot" />);
    await userEvent.click(screen.getByTestId("discord-agent-connect"));
    await userEvent.click(await screen.findByTestId("discord-docs-link"));
    expect(mockOpenExternal).toHaveBeenCalledWith(
      "https://multica.ai/docs/discord-bot-integration",
    );
  });

  it("does not report success for a malformed install response", async () => {
    mockRegister.mockResolvedValue({});
    renderUI(<DiscordAgentBindButton agentId="agent-1" agentName="Bot" />);
    await userEvent.click(screen.getByTestId("discord-agent-connect"));
    await userEvent.type(await screen.findByTestId("discord-bot-token"), "MTIz.abc.def");
    await userEvent.click(screen.getByTestId("discord-connect-submit"));
    await waitFor(() => expect(mockToastError).toHaveBeenCalled());
    expect(mockInvalidate).not.toHaveBeenCalled();
  });

  it("shows the connected badge instead of the CTA when the agent already has an active install", () => {
    installationsRef.current = {
      installations: [
        { id: "i1", agent_id: "agent-1", status: "active", bot_username: "my_bot" },
      ],
      configured: true,
      install_supported: true,
    };
    renderUI(<DiscordAgentBindButton agentId="agent-1" />);
    expect(screen.getByTestId("discord-agent-bot-connected")).toBeTruthy();
    expect(screen.getByTestId("discord-agent-bot-disconnect")).toBeTruthy();
    expect(screen.queryByTestId("discord-agent-connect")).toBeNull();
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
    renderUI(<DiscordAgentBindButton agentId="agent-1" />);

    await userEvent.click(screen.getByTestId("discord-agent-bot-disconnect"));
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
    renderUI(<DiscordAgentBindButton agentId="agent-1" />);

    await userEvent.click(screen.getByTestId("discord-agent-bot-disconnect"));
    const actions = await screen.findAllByRole("button", { name: /^Disconnect$/i });
    await userEvent.click(actions.at(-1)!);

    await waitFor(() => expect(mockToastError).toHaveBeenCalledWith("network failed"));
    expect(screen.getByTestId("discord-agent-bot-connected")).toBeTruthy();
    expect(mockInvalidate).not.toHaveBeenCalled();
  });

  it("renders nothing for a non-manager", () => {
    membersRef.current = [{ user_id: "user-1", role: "member" }];
    const { container } = renderUI(<DiscordAgentBindButton agentId="agent-1" />);
    expect(container).toBeEmptyDOMElement();
  });

  it("renders nothing when install is unavailable and the agent is unbound", () => {
    installationsRef.current = { installations: [], configured: true, install_supported: false };
    const { container } = renderUI(<DiscordAgentBindButton agentId="agent-1" />);
    expect(container).toBeEmptyDOMElement();
  });
});

describe("DiscordTab", () => {
  beforeEach(resetFixtures);

  it("shows a loading state without claiming Discord is disabled", () => {
    queryLoadingRef.current = true;
    renderUI(<DiscordTab />);
    expect(screen.getByText("Loading…")).toBeTruthy();
    expect(screen.queryByText(/Discord integration not enabled/i)).toBeNull();
  });

  it("surfaces the not-enabled notice when the deployment has no Discord key", () => {
    installationsRef.current = { installations: [], configured: false, install_supported: false };
    renderUI(<DiscordTab />);
    expect(screen.getByText(/Discord integration not enabled/i)).toBeTruthy();
  });

  it("shows the empty state when configured but nothing is connected", () => {
    renderUI(<DiscordTab />);
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
    renderUI(<DiscordTab />);
    expect(screen.getByText("Agent agent-7")).toBeTruthy();
    expect(screen.getByText("@my_bot")).toBeTruthy();
    expect(screen.getByText(/Disconnect/i)).toBeTruthy();
  });

  it("hides the disconnect control from non-managers", () => {
    membersRef.current = [{ user_id: "user-1", role: "member" }];
    installationsRef.current = {
      installations: [
        { id: "i1", agent_id: "agent-7", status: "active", bot_username: "my_bot" },
      ],
      configured: true,
      install_supported: true,
    };
    renderUI(<DiscordTab />);
    expect(screen.queryByText(/Disconnect/i)).toBeNull();
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
    renderUI(<DiscordTab />);
    await userEvent.click(screen.getByRole("button", { name: /Disconnect/i }));
    expect(mockDeleteInstallation).not.toHaveBeenCalled();
    const actions = await screen.findAllByRole("button", { name: /^Disconnect$/i });
    await userEvent.click(actions.at(-1)!);
    await waitFor(() =>
      expect(mockDeleteInstallation).toHaveBeenCalledWith("workspace-1", "i1"),
    );
    expect(mockInvalidate).toHaveBeenCalled();
  });

  it("tolerates a malformed installations response", () => {
    installationsRef.current = { configured: true } as never;
    renderUI(<DiscordTab />);
    expect(screen.getByText(/No bots connected yet/i)).toBeTruthy();
  });

  it("shows a load error instead of pretending Discord is disabled", () => {
    queryErrorRef.current = true;
    renderUI(<DiscordTab />);
    expect(screen.getByText(/Failed to load Discord installations/i)).toBeTruthy();
    expect(screen.queryByText(/Discord integration not enabled/i)).toBeNull();
  });
});
