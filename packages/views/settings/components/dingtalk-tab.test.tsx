// @vitest-environment jsdom

import { type ReactNode } from "react";
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
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
    group_routing_supported: true,
  },
}));
const agentsRef = vi.hoisted(() => ({
  current: [
    { id: "agent-1", name: "Agent One", archived_at: null },
    { id: "agent-2", name: "Agent Two", archived_at: null },
  ] as Array<{ id: string; name: string; archived_at: string | null }>,
}));
const groupRoutesRef = vi.hoisted(() => ({
  current: { routes: [] as unknown[] },
}));
const mockRegisterBYO = vi.hoisted(() => vi.fn());
const mockDeleteInstallation = vi.hoisted(() => vi.fn());
const mockUpdateGroupRoute = vi.hoisted(() => vi.fn());
const mockOpenExternal = vi.hoisted(() => vi.fn());
const mockInvalidate = vi.hoisted(() => vi.fn());
const mockToastSuccess = vi.hoisted(() => vi.fn());
const mockToastError = vi.hoisted(() => vi.fn());

vi.mock("@tanstack/react-query", () => ({
  useQuery: (opts: { queryKey: unknown[]; enabled?: boolean }) => {
    if (opts.enabled === false) return { data: undefined, isLoading: false };
    const key = JSON.stringify(opts.queryKey);
    if (key.includes("members")) return { data: membersRef.current, isLoading: false };
    if (key.includes("agents")) {
      return {
        data: agentsRef.current,
        isLoading: false,
        isError: false,
        isFetching: false,
        isSuccess: true,
        refetch: vi.fn(),
      };
    }
    if (key.includes("group-routes")) return { data: groupRoutesRef.current, isLoading: false };
    if (key.includes("installations")) return { data: installationsRef.current, isLoading: false };
    return { data: undefined, isLoading: false };
  },
  useQueryClient: () => ({ invalidateQueries: mockInvalidate }),
  queryOptions: <T,>(opts: T) => opts,
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "workspace-1" }));

vi.mock("@multica/core/workspace/queries", () => ({
  memberListOptions: () => ({ queryKey: ["members"], queryFn: vi.fn() }),
  agentListOptions: () => ({ queryKey: ["agents"], queryFn: vi.fn() }),
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
  dingtalkGroupRoutesOptions: () => ({
    queryKey: ["dingtalk", "group-routes"],
    queryFn: vi.fn(),
  }),
  dingtalkKeys: {
    installations: (wsId: string) => ["dingtalk", "installations", wsId],
    groupRoutes: (wsId: string) => ["dingtalk", "group-routes", wsId],
  },
}));

vi.mock("@multica/core/api", () => ({
  api: {
    registerDingTalkBYO: mockRegisterBYO,
    deleteDingTalkInstallation: mockDeleteInstallation,
    updateDingTalkGroupRoute: mockUpdateGroupRoute,
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
  toast: { success: mockToastSuccess, error: mockToastError, message: vi.fn() },
}));

vi.mock("../../platform", () => ({ openExternal: mockOpenExternal }));

import { DingTalkAgentBindButton, DingTalkTab } from "./dingtalk-tab";

const TEST_RESOURCES = { en: { common: enCommon, settings: enSettings } };

afterEach(cleanup);

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
  installationsRef.current = {
    installations: [],
    configured: true,
    install_supported: true,
    group_routing_supported: true,
  };
  agentsRef.current = [
    { id: "agent-1", name: "Agent One", archived_at: null },
    { id: "agent-2", name: "Agent Two", archived_at: null },
  ];
  groupRoutesRef.current = { routes: [] };
}

function setConnectedGroupRoute() {
  installationsRef.current = {
    installations: [{ id: "i1", agent_id: "agent-1", status: "active" }],
    configured: true,
    install_supported: true,
    group_routing_supported: true,
  };
  groupRoutesRef.current = {
    routes: [{
      id: "route-1",
      installation_id: "i1",
      conversation_id: "cid-platform",
      conversation_title: "Platform team",
      agent_id: "agent-2",
    }],
  };
}

describe("DingTalkAgentBindButton", () => {
  beforeEach(resetFixtures);

  it("renders the DingTalk brand mark in the connect button", () => {
    renderUI(<DingTalkAgentBindButton agentId="agent-1" agentName="Bot" />);
    const button = screen.getByTestId("dingtalk-agent-connect");
    expect(button.querySelector('[data-testid="dingtalk-mark"].h-4.w-4')).toBeTruthy();
  });

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
      group_routing_supported: true,
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
    installationsRef.current = {
      installations: [],
      configured: true,
      install_supported: false,
      group_routing_supported: true,
    };
    const { container } = renderUI(<DingTalkAgentBindButton agentId="agent-1" />);
    expect(container).toBeEmptyDOMElement();
  });
});

describe("DingTalkTab", () => {
  beforeEach(resetFixtures);

  it("surfaces the not-enabled notice when the deployment has no DingTalk key", () => {
    installationsRef.current = {
      installations: [],
      configured: false,
      install_supported: false,
      group_routing_supported: false,
    };
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
      group_routing_supported: true,
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
      group_routing_supported: true,
    };
    renderUI(<DingTalkTab />);
    expect(screen.queryByText(/Invalid Date/i)).toBeNull();
  });

  it("lists a discovered group with its fixed Agent", () => {
    setConnectedGroupRoute();
    renderUI(<DingTalkTab />);
    expect(screen.getByText("Platform team")).toBeTruthy();
    expect(screen.getByText("cid-platform")).toBeTruthy();
    expect(screen.getByText("Agent Two")).toBeTruthy();
  });

  it("lets an owner reassign a group and invalidates the route query", async () => {
    setConnectedGroupRoute();
    mockUpdateGroupRoute.mockResolvedValue({ id: "route-1", agent_id: "agent-1" });
    const user = userEvent.setup();
    renderUI(<DingTalkTab />);

    await user.click(screen.getByRole("combobox", { name: "Agent for this group" }));
    await user.click(await screen.findByRole("option", { name: "Agent One" }));

    await waitFor(() => {
      expect(mockUpdateGroupRoute).toHaveBeenCalledWith(
        "workspace-1",
        "route-1",
        { agent_id: "agent-1" },
      );
    });
    expect(mockInvalidate).toHaveBeenCalledWith({
      queryKey: ["dingtalk", "group-routes", "workspace-1"],
    });
    expect(mockToastSuccess).toHaveBeenCalled();
  });

  it("disables every route selector while one route update is pending", async () => {
    setConnectedGroupRoute();
    groupRoutesRef.current = {
      routes: [
        ...groupRoutesRef.current.routes,
        {
          id: "route-2",
          installation_id: "i1",
          conversation_id: "cid-security",
          conversation_title: "Security team",
          agent_id: "agent-2",
        },
      ],
    };
    mockUpdateGroupRoute.mockImplementation(() => new Promise(() => {}));
    const user = userEvent.setup();
    renderUI(<DingTalkTab />);

    const selectors = screen.getAllByRole("combobox", { name: "Agent for this group" });
    expect(selectors).toHaveLength(2);
    await user.click(selectors[0]!);
    await user.click(await screen.findByRole("option", { name: "Agent One" }));

    await waitFor(() => expect(mockUpdateGroupRoute).toHaveBeenCalledTimes(1));
    const pendingSelectors = screen.getAllByRole("combobox", { name: "Agent for this group" });
    expect(pendingSelectors[0]).toBeDisabled();
    expect(pendingSelectors[1]).toBeDisabled();
  });

  it("renders group routing read-only for a non-manager", () => {
    setConnectedGroupRoute();
    membersRef.current = [{ user_id: "user-1", role: "member" }];
    renderUI(<DingTalkTab />);

    expect(screen.getByText("Agent Two")).toBeTruthy();
    expect(
      screen.queryByRole("combobox", { name: "Agent for this group" }),
    ).toBeNull();
  });

  it("keeps the assigned agent name visible while that agent is archived", () => {
    setConnectedGroupRoute();
    agentsRef.current = [
      { id: "agent-1", name: "Agent One", archived_at: null },
      { id: "agent-2", name: "Agent Two", archived_at: "2026-08-10T00:00:00Z" },
    ];
    renderUI(<DingTalkTab />);

    expect(
      screen.getByRole("combobox", { name: "Agent for this group" }).textContent,
    ).toContain("Agent Two");
  });

  it("reports reassignment failures without invalidating successful data", async () => {
    setConnectedGroupRoute();
    mockUpdateGroupRoute.mockRejectedValue(new Error("route update failed"));
    const user = userEvent.setup();
    renderUI(<DingTalkTab />);

    await user.click(screen.getByRole("combobox", { name: "Agent for this group" }));
    await user.click(await screen.findByRole("option", { name: "Agent One" }));

    await waitFor(() => {
      expect(mockToastError).toHaveBeenCalledWith("route update failed");
    });
    expect(mockInvalidate).not.toHaveBeenCalledWith({
      queryKey: ["dingtalk", "group-routes", "workspace-1"],
    });
  });
});
