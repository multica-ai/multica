// @vitest-environment jsdom

import { type ReactNode } from "react";
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enSettings from "../../locales/en/settings.json";

type MemberRole = "owner" | "admin" | "member";

const membersRef = vi.hoisted(() => ({
  current: [{ user_id: "user-1", role: "owner" as MemberRole }],
}));
const agentsRef = vi.hoisted(() => ({
  current: [
    { id: "agent-1" },
    { id: "agent-7" },
    { id: "agent-8" },
  ],
}));
const installationsRef = vi.hoisted(() => ({
  current: {
    installations: [] as unknown[],
    configured: true,
    install_supported: true,
  },
}));
const groupsRef = vi.hoisted(() => ({
  current: {
    data: { groups: [], group_discovery_supported: true } as {
      groups: unknown[];
      group_discovery_supported: boolean;
      inactive_group_counts?: Record<string, number>;
      bot_identities?: Record<string, unknown>;
    },
    isLoading: false,
    isError: false,
    refetch: vi.fn(),
  },
}));
const inactiveGroupsRef = vi.hoisted(() => ({
  current: {
    data: undefined as undefined | { pages: Array<{ groups: unknown[]; next_offset: number | null }> },
    isLoading: false,
    isError: false,
    refetch: vi.fn(),
    hasNextPage: false,
    isFetchingNextPage: false,
    fetchNextPage: vi.fn(),
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
    if (key.includes("agents")) return { data: agentsRef.current, isLoading: false };
    if (key.includes("groups")) return groupsRef.current;
    if (key.includes("installations")) return { data: installationsRef.current, isLoading: false };
    return { data: undefined, isLoading: false };
  },
  useQueryClient: () => ({ invalidateQueries: mockInvalidate }),
  useInfiniteQuery: () => inactiveGroupsRef.current,
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
  ActorAvatar: ({
    actorId,
    profileLink,
  }: {
    actorId: string;
    profileLink?: boolean;
  }) => (
    <span
      data-testid="actor-avatar"
      data-actor-id={actorId}
      data-profile-link={String(profileLink)}
    />
  ),
}));

vi.mock("@multica/core/dingtalk", () => ({
  dingtalkInstallationsOptions: () => ({
    queryKey: ["dingtalk", "installations"],
    queryFn: vi.fn(),
  }),
  dingtalkGroupsOptions: () => ({
    queryKey: ["dingtalk", "groups"],
    queryFn: vi.fn(),
  }),
  dingtalkKeys: {
    installations: (wsId: string) => ["dingtalk", "installations", wsId],
    groups: (wsId: string) => ["dingtalk", "groups", wsId],
    inactiveGroups: (wsId: string, installationId: string) =>
      ["dingtalk", "groups", wsId, "inactive", installationId],
    agentInactiveGroups: (wsId: string, agentId: string, installationId: string) =>
      ["dingtalk", "groups", wsId, "agent", agentId, "inactive", installationId],
  },
}));

vi.mock("@multica/core/api", () => ({
  api: {
    registerDingTalkBYO: mockRegisterBYO,
    deleteDingTalkInstallation: mockDeleteInstallation,
    forgetDingTalkGroup: vi.fn(),
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
  agentsRef.current = [
    { id: "agent-1" },
    { id: "agent-7" },
    { id: "agent-8" },
  ];
  installationsRef.current = { installations: [], configured: true, install_supported: true };
  groupsRef.current = {
    data: { groups: [], group_discovery_supported: true },
    isLoading: false,
    isError: false,
    refetch: vi.fn(),
  };
  inactiveGroupsRef.current = {
    data: undefined,
    isLoading: false,
    isError: false,
    refetch: vi.fn(),
    hasNextPage: false,
    isFetchingNextPage: false,
    fetchNextPage: vi.fn(),
  };
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

  it.each([
    { role: "owner", ownsAgent: false, canManage: true },
    { role: "owner", ownsAgent: true, canManage: true },
    { role: "admin", ownsAgent: false, canManage: true },
    { role: "admin", ownsAgent: true, canManage: true },
    { role: "member", ownsAgent: false, canManage: false },
    { role: "member", ownsAgent: true, canManage: true },
  ] as const)(
    "applies the Agent Detail permission matrix: role=$role ownsAgent=$ownsAgent",
    ({ role, ownsAgent, canManage }) => {
      membersRef.current = [{ user_id: "user-1", role }];
      renderUI(
        <DingTalkAgentBindButton
          agentId="agent-1"
          agentOwnerId={ownsAgent ? "user-1" : "user-2"}
        />,
      );
      expect(screen.queryByTestId("dingtalk-agent-connect") !== null).toBe(canManage);
    },
  );

  it("renders no management entry for a user without workspace membership", () => {
    membersRef.current = [];
    const { container } = renderUI(
      <DingTalkAgentBindButton agentId="agent-1" agentOwnerId="user-1" />,
    );
    expect(container.firstChild).toBeNull();
  });

  it("renders nothing when install is unavailable and the agent is unbound", () => {
    installationsRef.current = { installations: [], configured: true, install_supported: false };
    const { container } = renderUI(<DingTalkAgentBindButton agentId="agent-1" />);
    expect(container.firstChild).toBeNull();
  });
});

describe("DingTalkTab", () => {
  beforeEach(resetFixtures);

  it.each([
    { role: "owner", canManage: true },
    { role: "admin", canManage: true },
    { role: "member", canManage: false },
  ] as const)(
    "applies the Settings permission matrix for role=$role",
    ({ role, canManage }) => {
      // Settings intentionally has no Agent-owner input: every plain member,
      // including someone who owns one of these Agents, stays read-only here.
      membersRef.current = [{ user_id: "user-1", role }];
      installationsRef.current = {
        installations: [{
          id: "i-role-matrix",
          agent_id: "agent-7",
          status: "active",
          installed_at: "2026-08-19T00:00:00Z",
          bound_dingtalk_user_ids: ["staff-role-matrix"],
        }],
        configured: true,
        install_supported: true,
      };
      groupsRef.current.data.groups = [{
        conversation_id: "cid-role-matrix",
        conversation_title: "Role matrix group",
        bots: [{
          installation_id: "i-role-matrix",
          agent_id: "agent-7",
          bot_name: "Role matrix bot",
        }],
      }];

      renderUI(<DingTalkTab />);
      expect(screen.queryByRole("button", { name: /Disconnect/i }) !== null).toBe(canManage);
      expect(
        screen.queryByRole("button", { name: "Role matrix bot" }) !== null,
      ).toBe(canManage);
      expect(screen.queryByText(/staff-role-matrix/)).toBeNull();
      expect(screen.getByText("Role matrix bot")).toBeTruthy();
      expect(screen.getByText("Role matrix group")).toBeTruthy();
      expect(
        screen.getByText(enSettings.dingtalk.groups_overview_description),
      ).toBeTruthy();
      expect(screen.getByTestId("dingtalk-installation-metadata").textContent).toContain(
        "Installed",
      );
      expect(
        screen.queryByRole("heading", {
          name: "Connections",
          level: 2,
        }),
      ).toBeTruthy();
    },
  );

  it("surfaces the not-enabled notice when the deployment has no DingTalk key", () => {
    installationsRef.current = { installations: [], configured: false, install_supported: false };
    renderUI(<DingTalkTab />);
    expect(screen.getByText(/DingTalk integration not enabled/i)).toBeTruthy();
  });

  it("shows the empty state when configured but nothing is connected", () => {
    renderUI(<DingTalkTab />);
    expect(screen.getByText(/No bots connected yet/i)).toBeTruthy();
  });

  it("shows linked Staff IDs from the bot name without adding them to the row", async () => {
    installationsRef.current = {
      installations: [{
        id: "i1",
        agent_id: "agent-7",
        status: "active",
        bound_dingtalk_user_ids: ["staff-1001", "staff-1002"],
      }],
      configured: true,
      install_supported: true,
    };
    groupsRef.current.data.groups = [{
      conversation_id: "cid-linked",
      conversation_title: "Linked group",
      bots: [{
        installation_id: "i1",
        agent_id: "agent-7",
        bot_name: "Linked Bot",
      }],
    }];
    renderUI(<DingTalkTab />);
    expect(screen.getByText("Agent agent-7")).toBeTruthy();
    const botName = screen.getByRole("button", { name: "Linked Bot" });
    expect(screen.queryByText(/Linked staff ID:/i)).toBeNull();
    await userEvent.hover(botName);
    expect(
      await screen.findByText("Linked staff ID: staff-1001, staff-1002"),
    ).toBeTruthy();
    expect(screen.getByText(/Disconnect/i)).toBeTruthy();
  });

  it("does not render a linked identity when this member has no DingTalk binding", () => {
    installationsRef.current = {
      installations: [{ id: "i1", agent_id: "agent-7", status: "active" }],
      configured: true,
      install_supported: true,
    };
    renderUI(<DingTalkTab />);
    expect(screen.queryByText(/Linked staff ID:/i)).toBeNull();
  });

  it("hides linked DingTalk identities from a regular workspace member", () => {
    membersRef.current = [{ user_id: "user-1", role: "member" }];
    installationsRef.current = {
      installations: [{
        id: "i1",
        agent_id: "agent-7",
        status: "active",
        bound_dingtalk_user_ids: ["staff-must-stay-private"],
      }],
      configured: true,
      install_supported: true,
    };
    renderUI(<DingTalkTab />);
    expect(screen.getByText("Agent agent-7")).toBeTruthy();
    expect(screen.queryByText(/staff-must-stay-private/)).toBeNull();
    expect(screen.queryByText(/Linked staff ID:/i)).toBeNull();
    expect(screen.getByRole("heading", { name: "Connections", level: 2 })).toBeTruthy();
    const overviewDescription = screen.getByText(
      enSettings.dingtalk.groups_overview_description,
    );
    expect(overviewDescription.classList).toContain("text-caption");
  });

  it("shows every observed Agent → Bot → Group relationship to an admin", async () => {
    membersRef.current = [{ user_id: "user-1", role: "admin" }];
    installationsRef.current = {
      installations: [
        {
          id: "i1",
          agent_id: "agent-7",
          status: "active",
          installed_at: "2026-08-19T00:00:00Z",
          bound_dingtalk_user_ids: ["staff-1001"],
        },
      ],
      configured: true,
      install_supported: true,
    };
    groupsRef.current.data.groups = [
      {
        conversation_id: "cid-platform",
        conversation_title: "Platform team",
        bots: [
          {
            installation_id: "i1",
            agent_id: "agent-7",
            bot_name: "Release Bot",
            bot_identity_issue: "missing_qyapi_chat_manage",
          },
        ],
      },
    ];

    renderUI(<DingTalkTab />);
    expect(screen.getByText("Agent agent-7")).toBeTruthy();
    expect(screen.getByRole("heading", { name: "Connections", level: 2 })).toBeTruthy();
    expect(screen.getByText("Release Bot")).toBeTruthy();
    expect(
      screen.getByRole("button", { name: /qyapi_chat_manage/i }),
    ).toBeTruthy();
    const connectedLabel = screen.getByText("Connected bot:");
    const connectedStatus = connectedLabel.parentElement?.parentElement;
    expect(connectedStatus?.classList).toContain(
      "text-caption",
    );
    expect(connectedStatus?.classList).not.toContain(
      "text-micro",
    );
    const metadata = screen.getByTestId("dingtalk-installation-metadata");
    expect(metadata.contains(connectedLabel)).toBe(false);
    expect(metadata.parentElement).toBe(connectedStatus?.parentElement);
    expect(metadata.textContent).toContain("Installed");
    expect(metadata.parentElement?.textContent).toContain(
      "Connected bot:Release Bot·Installed",
    );
    expect(metadata.parentElement?.textContent).not.toContain("Linked staff ID");
    expect(metadata.textContent).not.toContain("Release Bot");
    expect(metadata.textContent).not.toMatch(/\d{1,2}:\d{2}:\d{2}/);
    expect(screen.queryByText(/Connected to Agent/i)).toBeNull();
    const groupTitle = screen.getByText("Platform team");
    const forgetButton = screen.getByRole("button", { name: "Forget" });
    expect(groupTitle.parentElement?.contains(forgetButton)).toBe(true);
    expect(forgetButton.classList).toContain("opacity-0");
    expect(forgetButton.classList).toContain("group-hover:opacity-100");
    expect(forgetButton.classList).toContain("group-focus-within:opacity-100");
    expect(
      screen.getByRole("heading", { name: "Recent groups", level: 4 }),
    ).toBeTruthy();
    const overviewDescriptions = screen.getAllByText(
      enSettings.dingtalk.groups_overview_description,
    );
    expect(overviewDescriptions).toHaveLength(1);
    expect(overviewDescriptions[0]?.classList).toContain("text-caption");
    expect(overviewDescriptions[0]?.classList).not.toContain("text-micro");
    expect(screen.getByTestId("dingtalk-bot-groups").parentElement).toBe(
      screen.getByTestId("dingtalk-installation-row"),
    );
    expect(screen.getByTestId("dingtalk-installation-row").classList).toContain("py-6");
    expect(
      screen.getByTestId("dingtalk-installation-row").closest('[data-slot="card"]')?.classList,
    ).toContain("py-0");
    const conversationId = screen.getByLabelText(
      "DingTalk group conversation ID cid-platform",
    );
    expect(conversationId.textContent).toBe("cid-platform");
    expect(conversationId.classList).toContain("text-faint-foreground");
    expect(conversationId.classList).toContain("group-hover:text-muted-foreground");
    await userEvent.hover(conversationId);
    await waitFor(() =>
      expect(document.querySelector('[data-slot="tooltip-content"]')).toBeTruthy(),
    );
    const tooltip = document.querySelector('[data-slot="tooltip-content"]');
    expect(tooltip?.textContent).toBe("DingTalk group conversation ID");
    expect(tooltip?.textContent).not.toContain("cid-platform");
    expect(screen.getAllByText("cid-platform")).toHaveLength(1);
  });

  it("sorts active groups by recency, then inactive groups by title and conversation ID", () => {
    installationsRef.current = {
      installations: [{ id: "i1", agent_id: "agent-7", status: "active" }],
      configured: true,
      install_supported: true,
    };
    groupsRef.current.data.groups = [
      {
        conversation_id: "cid-alpha-b",
        conversation_title: "Alpha",
        bots: [{ installation_id: "i1", agent_id: "agent-7" }],
      },
      {
        conversation_id: "cid-old",
        conversation_title: "Zulu",
        bots: [{
          installation_id: "i1",
          agent_id: "agent-7",
          last_active_at: "2026-08-19T08:00:00Z",
        }],
      },
      {
        conversation_id: "cid-untitled",
        conversation_title: "",
        bots: [{ installation_id: "i1", agent_id: "agent-7" }],
      },
      {
        conversation_id: "cid-recent",
        conversation_title: "Beta",
        bots: [{
          installation_id: "i1",
          agent_id: "agent-7",
          last_active_at: "2026-08-19T09:00:00Z",
        }],
      },
      {
        conversation_id: "cid-alpha-a",
        conversation_title: "Alpha",
        bots: [{ installation_id: "i1", agent_id: "agent-7" }],
      },
    ];

    renderUI(<DingTalkTab />);
    expect(
      screen.getAllByTestId("dingtalk-group-item").map((item) =>
        item.querySelector("code")?.textContent,
      ),
    ).toEqual([
      "cid-recent",
      "cid-old",
      "cid-alpha-a",
      "cid-alpha-b",
      "cid-untitled",
    ]);
  });

  it("globally sorts inactive groups after merging loaded pages", async () => {
    installationsRef.current = {
      installations: [{ id: "i1", agent_id: "agent-7", status: "active" }],
      configured: true,
      install_supported: true,
    };
    groupsRef.current.data = {
      groups: [],
      group_discovery_supported: true,
      inactive_group_counts: { i1: 4 },
    };
    inactiveGroupsRef.current.data = {
      pages: [
        {
          groups: [
            {
              conversation_id: "cid-zulu",
              conversation_title: "Zulu",
              bots: [{ installation_id: "i1", agent_id: "agent-7" }],
            },
            {
              conversation_id: "cid-bravo-b",
              conversation_title: "Bravo",
              bots: [{ installation_id: "i1", agent_id: "agent-7" }],
            },
          ],
          next_offset: 2,
        },
        {
          groups: [
            {
              conversation_id: "cid-alpha",
              conversation_title: "Alpha",
              bots: [{ installation_id: "i1", agent_id: "agent-7" }],
            },
            {
              conversation_id: "cid-bravo-a",
              conversation_title: "Bravo",
              bots: [{ installation_id: "i1", agent_id: "agent-7" }],
            },
          ],
          next_offset: null,
        },
      ],
    };

    renderUI(<DingTalkTab />);
    await userEvent.click(screen.getByRole("button", { name: /long inactive/i }));
    expect(
      screen.getAllByTestId("dingtalk-group-item").map((item) =>
        item.querySelector("code")?.textContent,
      ),
    ).toEqual(["cid-alpha", "cid-bravo-a", "cid-bravo-b", "cid-zulu"]);
  });

  it("renders discovery UI only when the backend explicitly supports it", () => {
    installationsRef.current = {
      installations: [{ id: "i1", agent_id: "agent-7", status: "active" }],
      configured: true,
      install_supported: true,
    };
    groupsRef.current.data = {
      groups: [],
      group_discovery_supported: false,
    };

    renderUI(<DingTalkTab />);
    expect(screen.getByText("Connected bot:")).toBeTruthy();
    expect(screen.queryByText("Identity unavailable")).toBeNull();
    expect(screen.queryByTestId("dingtalk-bot-groups")).toBeNull();
    expect(
      screen.queryByText(enSettings.dingtalk.groups_overview_description),
    ).toBeNull();
  });

  it("keeps loading and retryable error states visible before capability data arrives", async () => {
    installationsRef.current = {
      installations: [{ id: "i1", agent_id: "agent-7", status: "active" }],
      configured: true,
      install_supported: true,
    };
    groupsRef.current.isLoading = true;
    groupsRef.current.data = undefined as never;

    const loading = renderUI(<DingTalkTab />);
    expect(screen.getByText("Loading groups…")).toBeTruthy();
    loading.unmount();

    groupsRef.current.isLoading = false;
    groupsRef.current.isError = true;
    groupsRef.current.data = undefined as never;
    renderUI(<DingTalkTab />);

    await userEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(groupsRef.current.refetch).toHaveBeenCalledOnce();
  });

  it("shows only the server-filtered Agent, bot, groups, and install time to a regular member", () => {
    membersRef.current = [{ user_id: "user-1", role: "member" }];
    installationsRef.current = {
      installations: [
        { id: "i1", agent_id: "agent-7", status: "active", installed_at: "" },
      ],
      configured: true,
      install_supported: true,
    };
    groupsRef.current.data.groups = [
      {
        conversation_id: "cid-private",
        conversation_title: "Visible group",
        bots: [
          {
            installation_id: "i1",
            agent_id: "agent-7",
            bot_name: "Visible Bot",
            bot_identity_issue: "missing_qyapi_chat_manage",
          },
        ],
      },
    ];

    renderUI(<DingTalkTab />);
    expect(screen.getByText("Agent agent-7")).toBeTruthy();
    expect(screen.getByText("Visible Bot")).toBeTruthy();
    expect(screen.getByText("Visible group")).toBeTruthy();
    expect(screen.getByTestId("dingtalk-bot-groups")).toBeTruthy();
    expect(screen.getByTestId("dingtalk-installation-metadata").textContent).toContain(
      "Installed —",
    );
    expect(screen.queryByRole("button", { name: /Disconnect/i })).toBeNull();
    expect(screen.queryByRole("button", { name: /qyapi_chat_manage/i })).toBeNull();
  });

  it("shows an unavailable bot identity without admin remediation to a regular member", () => {
    membersRef.current = [{ user_id: "user-1", role: "member" }];
    installationsRef.current = {
      installations: [
        { id: "i1", agent_id: "agent-7", status: "active", installed_at: "" },
      ],
      configured: true,
      install_supported: true,
    };
    groupsRef.current.data.groups = [
      {
        conversation_id: "cid-visible",
        conversation_title: "Visible group",
        bots: [
          {
            installation_id: "i1",
            agent_id: "agent-7",
            bot_name: "",
            bot_identity_issue: "missing_qyapi_chat_manage",
          },
        ],
      },
    ];

    renderUI(<DingTalkTab />);
    expect(screen.getByText("Identity unavailable")).toBeTruthy();
    expect(screen.queryByRole("button", { name: /qyapi_chat_manage/i })).toBeNull();
  });

  it("filters a legacy server row when the member cannot see its Agent", () => {
    membersRef.current = [{ user_id: "user-1", role: "member" }];
    agentsRef.current = [{ id: "agent-7" }];
    installationsRef.current = {
      installations: [
        {
          id: "i-private",
          agent_id: "agent-private",
          status: "active",
          installed_at: "2026-08-19T00:00:00Z",
        },
      ],
      configured: true,
      install_supported: true,
    };

    renderUI(<DingTalkTab />);
    expect(screen.queryByText("Agent agent-private")).toBeNull();
    expect(screen.queryByTestId("dingtalk-installation-row")).toBeNull();
    expect(screen.getByText("No bots connected yet")).toBeTruthy();
  });

  it("labels an orphaned admin-only installation without linking to a missing Agent", () => {
    installationsRef.current = {
      installations: [
        {
          id: "i-orphan",
          agent_id: "agent-deleted",
          agent_available: false,
          status: "active",
          installed_at: "2026-08-19T00:00:00Z",
        },
      ],
      configured: true,
      install_supported: true,
    };

    renderUI(<DingTalkTab />);
    expect(screen.getByText("Deleted Agent")).toBeTruthy();
    expect(screen.getByTestId("actor-avatar").getAttribute("data-profile-link")).toBe(
      "false",
    );
    expect(screen.getByRole("button", { name: /Disconnect/i })).toBeTruthy();
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
