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
const mockRegisterBYO = vi.hoisted(() => vi.fn());
const mockDeleteInstallation = vi.hoisted(() => vi.fn());
const mockOpenExternal = vi.hoisted(() => vi.fn());
const mockInvalidate = vi.hoisted(() => vi.fn());

vi.mock("@tanstack/react-query", () => ({
  useQuery: (opts: { queryKey: unknown[]; enabled?: boolean }) => {
    if (opts.enabled === false) return { data: undefined, isLoading: false };
    const key = JSON.stringify(opts.queryKey);
    if (key.includes("members")) return { data: membersRef.current, isLoading: false };
    if (key.includes("installations")) return { data: installationsRef.current, isLoading: false };
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

vi.mock("@multica/core/dingtalk", () => ({
  dingtalkInstallationsOptions: () => ({
    queryKey: ["dingtalk", "installations"],
    queryFn: vi.fn(),
  }),
  dingtalkKeys: { installations: (wsId: string) => ["dingtalk", "installations", wsId] },
}));

vi.mock("@multica/core/api", () => ({
  api: {
    registerDingTalkBYO: mockRegisterBYO,
    deleteDingTalkInstallation: mockDeleteInstallation,
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
  toast: { success: vi.fn(), error: vi.fn(), message: vi.fn() },
}));

vi.mock("../../platform", () => ({ openExternal: mockOpenExternal }));

import { DingTalkAgentBindButton, DingTalkTab } from "./dingtalk-tab";

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
}

describe("DingTalkAgentBindButton", () => {
  beforeEach(resetFixtures);

  it("opens the BYO dialog and submits the pasted AppKey + AppSecret", async () => {
    mockRegisterBYO.mockResolvedValue({ id: "i1", agent_id: "agent-1", status: "active" });
    renderUI(<DingTalkAgentBindButton agentId="agent-1" agentName="Bot" />);
    await userEvent.click(screen.getByTestId("dingtalk-agent-connect"));
    const idInput = await screen.findByTestId("dingtalk-byo-client-id");
    await userEvent.type(idInput, "ding-appkey");
    await userEvent.type(screen.getByTestId("dingtalk-byo-client-secret"), "ding-appsecret");
    await userEvent.click(screen.getByTestId("dingtalk-byo-submit"));
    await waitFor(() =>
      expect(mockRegisterBYO).toHaveBeenCalledWith("workspace-1", "agent-1", {
        client_id: "ding-appkey",
        client_secret: "ding-appsecret",
      }),
    );
    expect(mockOpenExternal).not.toHaveBeenCalled();
  });

  it("masks both credential inputs as password fields", async () => {
    renderUI(<DingTalkAgentBindButton agentId="agent-1" agentName="Bot" />);
    await userEvent.click(screen.getByTestId("dingtalk-agent-connect"));
    const idInput = await screen.findByTestId("dingtalk-byo-client-id");
    const secretInput = screen.getByTestId("dingtalk-byo-client-secret");
    expect(idInput.getAttribute("type")).toBe("password");
    expect(secretInput.getAttribute("type")).toBe("password");
  });

  it("shows the connected badge (not the CTA) when the agent already has an active install", () => {
    installationsRef.current = {
      installations: [{ id: "i1", agent_id: "agent-1", status: "active" }],
      configured: true,
      install_supported: true,
    };
    renderUI(<DingTalkAgentBindButton agentId="agent-1" />);
    expect(screen.getByTestId("dingtalk-agent-bot-connected")).toBeTruthy();
    expect(screen.getByTestId("dingtalk-agent-bot-disconnect")).toBeTruthy();
    expect(screen.queryByTestId("dingtalk-agent-connect")).toBeNull();
  });

  it("renders nothing for a non-manager", () => {
    membersRef.current = [{ user_id: "user-1", role: "member" }];
    const { container } = renderUI(<DingTalkAgentBindButton agentId="agent-1" />);
    expect(container).toBeEmptyDOMElement();
  });

  it("renders nothing when install is unavailable and the agent is unbound", () => {
    installationsRef.current = { installations: [], configured: true, install_supported: false };
    const { container } = renderUI(<DingTalkAgentBindButton agentId="agent-1" />);
    expect(container).toBeEmptyDOMElement();
  });
});

describe("DingTalkTab", () => {
  beforeEach(resetFixtures);

  it("surfaces the not-enabled notice when the deployment has no DingTalk key", () => {
    installationsRef.current = { installations: [], configured: false, install_supported: false };
    renderUI(<DingTalkTab />);
    expect(screen.getByText(/DingTalk integration not enabled/i)).toBeTruthy();
  });

  it("shows the empty state when configured but nothing is connected", () => {
    renderUI(<DingTalkTab />);
    expect(screen.getByText(/No bots connected yet/i)).toBeTruthy();
  });

  it("lists a connected installation with its agent name and a disconnect control", () => {
    installationsRef.current = {
      installations: [{ id: "i1", agent_id: "agent-7", status: "active" }],
      configured: true,
      install_supported: true,
    };
    renderUI(<DingTalkTab />);
    expect(screen.getByText("Agent agent-7")).toBeTruthy();
    expect(screen.getByText(/Disconnect/i)).toBeTruthy();
  });

  it("shows a placeholder instead of 'Invalid Date' when installed_at is missing or malformed", () => {
    installationsRef.current = {
      installations: [
        { id: "i1", agent_id: "agent-7", status: "active", installed_at: "" },
        { id: "i2", agent_id: "agent-8", status: "active", installed_at: "not-a-date" },
      ],
      configured: true,
      install_supported: true,
    };
    renderUI(<DingTalkTab />);
    expect(screen.queryByText(/Invalid Date/i)).toBeNull();
  });
});
