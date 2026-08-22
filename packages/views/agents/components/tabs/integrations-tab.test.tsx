// @vitest-environment jsdom

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import type { ReactNode } from "react";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Agent } from "@multica/core/types";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../../locales/en/common.json";
import enAgents from "../../../locales/en/agents.json";
import enSettings from "../../../locales/en/settings.json";

// IntegrationsTab's job is to pick which copy sits beside the bind entry
// based on (configured / install_supported / role). The bind entry itself
// is the shared LarkAgentBindButton, exhaustively covered in
// lark-tab.test.tsx — here we stub it to a marker so the tests assert the
// branch selection, not the install flow.
type MemberRole = "owner" | "admin" | "member";

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
const groupsRef = vi.hoisted(() => ({
  current: {
    data: { groups: [] as unknown[], group_discovery_supported: true },
    isLoading: false,
    isError: false,
    refetch: vi.fn(),
  },
}));

vi.mock("@tanstack/react-query", () => ({
  useQuery: (opts: { queryKey: unknown[]; enabled?: boolean }) => {
    if (opts.enabled === false) return { data: undefined };
    const key = JSON.stringify(opts.queryKey);
    if (key.includes("members")) return { data: membersRef.current };
    if (key.includes("groups")) return groupsRef.current;
    if (key.includes("installations")) return { data: installationsRef.current };
    return { data: undefined };
  },
  useQueryClient: () => ({ invalidateQueries: vi.fn() }),
  useInfiniteQuery: () => ({
    data: undefined,
    isLoading: false,
    isError: false,
    refetch: vi.fn(),
    hasNextPage: false,
    isFetchingNextPage: false,
    fetchNextPage: vi.fn(),
  }),
  queryOptions: <T,>(opts: T) => opts,
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("@multica/core/workspace/queries", () => ({
  memberListOptions: () => ({ queryKey: ["members"], queryFn: vi.fn() }),
}));

vi.mock("@multica/core/lark", () => ({
  larkInstallationsOptions: () => ({
    queryKey: ["lark", "installations"],
    queryFn: vi.fn(),
  }),
}));

vi.mock("@multica/core/slack", () => ({
  slackInstallationsOptions: () => ({
    queryKey: ["slack", "installations"],
    queryFn: vi.fn(),
  }),
}));

vi.mock("@multica/core/wecom", () => ({
  wecomInstallationsOptions: () => ({
    queryKey: ["wecom", "installations"],
    queryFn: vi.fn(),
  }),
}));

vi.mock("@multica/core/telegram", () => ({
  telegramInstallationsOptions: () => ({
    queryKey: ["telegram", "installations"],
    queryFn: vi.fn(),
  }),
}));

vi.mock("@multica/core/dingtalk", () => ({
  dingtalkInstallationsOptions: () => ({
    queryKey: ["dingtalk", "installations"],
    queryFn: vi.fn(),
  }),
  dingtalkAgentGroupsOptions: (_wsId: string, agentId: string) => ({
    queryKey: ["dingtalk", "groups", "agent", agentId],
    queryFn: vi.fn(),
  }),
  dingtalkKeys: {
    groups: (wsId: string) => ["dingtalk", "groups", wsId],
    inactiveGroups: (wsId: string, installationId: string) =>
      ["dingtalk", "groups", wsId, "inactive", installationId],
    agentInactiveGroups: (wsId: string, agentId: string, installationId: string) =>
      ["dingtalk", "groups", wsId, "agent", agentId, "inactive", installationId],
  },
}));

vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({ getAgentName: (id: string) => `Agent ${id}` }),
}));

vi.mock("../../../common/actor-avatar", () => ({
  ActorAvatar: ({ actorId }: { actorId: string }) => (
    <span data-testid="actor-avatar" data-actor-id={actorId} />
  ),
}));

vi.mock("@multica/core/auth", () => {
  const useAuthStore = Object.assign(
    (sel?: (s: { user: { id: string } }) => unknown) =>
      sel ? sel({ user: { id: "user-1" } }) : { user: { id: "user-1" } },
    { getState: () => ({ user: { id: "user-1" } }) },
  );
  return { useAuthStore };
});

vi.mock("../../../settings/components/lark-tab", () => ({
  LarkAgentBindButton: ({
    agentId,
    agentOwnerId,
  }: {
    agentId: string;
    agentOwnerId?: string | null;
  }) => (
    <div
      data-testid="lark-bind-button"
      data-agent-id={agentId}
      data-agent-owner-id={agentOwnerId ?? ""}
    />
  ),
}));

// SlackAgentBindButton is the shared bind entry covered in slack-tab.test.tsx;
// here it is a marker so the tests assert branch selection, not the OAuth flow.
vi.mock("../../../settings/components/slack-tab", () => ({
  SlackAgentBindButton: ({ agentId }: { agentId: string }) => (
    <div data-testid="slack-bind-button" data-agent-id={agentId} />
  ),
}));

// Same stubbing rationale for WeCom smart-bot: the shared bind entry has
// its own coverage in wecom-tab.test.tsx (when added); here it's a marker.
vi.mock("../../../settings/components/wecom-tab", () => ({
  WecomAgentBindButton: ({ agentId }: { agentId: string }) => (
    <div data-testid="wecom-bind-button" data-agent-id={agentId} />
  ),
}));

vi.mock("../../../settings/components/telegram-tab", () => ({
  TelegramAgentBindButton: ({ agentId }: { agentId: string }) => (
    <div data-testid="telegram-bind-button" data-agent-id={agentId} />
  ),
}));

import { IntegrationsTab } from "./integrations-tab";

const TEST_RESOURCES = {
  en: { common: enCommon, agents: enAgents, settings: enSettings },
};

afterEach(() => {
  cleanup();
  vi.useRealTimers();
});

const agent: Agent = {
  id: "agent-1",
  workspace_id: "ws-1",
  runtime_id: "runtime-1",
  name: "Agent",
  description: "",
  instructions: "",
  avatar_url: null,
  runtime_mode: "local",
  runtime_config: {},
  custom_args: [],
  visibility: "workspace",
  permission_mode: "public_to",
  invocation_targets: [{ target_type: "workspace", target_id: null }],
  status: "idle",
  max_concurrent_tasks: 1,
  model: "",
  owner_id: "user-1",
  skills: [],
  created_at: "2026-04-16T00:00:00Z",
  updated_at: "2026-04-16T00:00:00Z",
  archived_at: null,
  archived_by: null,
};

function renderTab(children: ReactNode) {
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
  };
  groupsRef.current = {
    data: { groups: [], group_discovery_supported: true },
    isLoading: false,
    isError: false,
    refetch: vi.fn(),
  };
}

describe("IntegrationsTab", () => {
  beforeEach(resetFixtures);

  it.each([
    { role: "owner", ownsAgent: false, permissionMode: "private", canManage: true },
    { role: "owner", ownsAgent: true, permissionMode: "private", canManage: true },
    { role: "admin", ownsAgent: false, permissionMode: "private", canManage: true },
    { role: "admin", ownsAgent: true, permissionMode: "private", canManage: true },
    { role: "member", ownsAgent: false, permissionMode: "private", canManage: false },
    { role: "member", ownsAgent: true, permissionMode: "private", canManage: true },
    { role: "owner", ownsAgent: false, permissionMode: "public_to", canManage: true },
    { role: "owner", ownsAgent: true, permissionMode: "public_to", canManage: true },
    { role: "admin", ownsAgent: false, permissionMode: "public_to", canManage: true },
    { role: "admin", ownsAgent: true, permissionMode: "public_to", canManage: true },
    { role: "member", ownsAgent: false, permissionMode: "public_to", canManage: false },
    { role: "member", ownsAgent: true, permissionMode: "public_to", canManage: true },
  ] as const)(
    "keeps DingTalk management independent of Agent access mode: role=$role ownsAgent=$ownsAgent permissionMode=$permissionMode",
    ({ role, ownsAgent, permissionMode, canManage }) => {
      membersRef.current = [{ user_id: "user-1", role }];
      renderTab(
        <IntegrationsTab
          agent={{
            ...agent,
            owner_id: ownsAgent ? "user-1" : "user-2",
            permission_mode: permissionMode,
            visibility: permissionMode === "private" ? "private" : "workspace",
          }}
        />,
      );
      expect(screen.queryByTestId("dingtalk-agent-connect") !== null).toBe(canManage);
    },
  );

  it("does not expose Agent management when the owner is no longer a workspace member", () => {
    membersRef.current = [];
    renderTab(<IntegrationsTab agent={agent} />);
    expect(screen.queryByTestId("dingtalk-agent-connect")).toBeNull();
    expect(screen.queryByTestId("lark-bind-button")).toBeNull();
  });

  it("renders the shared bind entries for an owner when configured and supported", () => {
    renderTab(<IntegrationsTab agent={agent} />);
    expect(screen.getByText("Lark")).toBeTruthy();
    expect(screen.getByText("Slack")).toBeTruthy();
    expect(screen.getByText("Telegram")).toBeTruthy();
    expect(screen.getByText(/Telegram bot.*\/issue.*reply stream live/i)).toBeTruthy();
    expect(screen.getByTestId("lark-bind-button").getAttribute("data-agent-id")).toBe("agent-1");
    expect(screen.getByTestId("slack-bind-button").getAttribute("data-agent-id")).toBe("agent-1");
    expect(screen.getByTestId("telegram-bind-button").getAttribute("data-agent-id")).toBe(
      "agent-1",
    );
  });

  it("renders the DingTalk brand mark in the DingTalk integration card", () => {
    renderTab(<IntegrationsTab agent={agent} />);
    const section = screen.getByText("DingTalk").closest("section");
    expect(section?.querySelector('[data-testid="dingtalk-mark"].h-5.w-5')).toBeTruthy();
    expect(screen.getByText(enSettings.dingtalk.agent_page_description)).toBeTruthy();
  });

  it("shows only this Agent's 1:1 bot and its groups", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-08-19T10:00:00Z"));
    installationsRef.current = {
      installations: [
        { id: "inst-1", agent_id: "agent-1", status: "active" },
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
            installation_id: "inst-1",
            agent_id: "agent-1",
            bot_name: "Release Bot",
            bot_identity_issue: "",
            last_active_at: "2026-08-19T08:00:00Z",
            mention_count: 18,
          },
          {
            installation_id: "inst-2",
            agent_id: "agent-2",
            bot_name: "Support Bot",
            bot_identity_issue: "",
          },
        ],
      },
      {
        conversation_id: "cid-unrelated",
        conversation_title: "Unrelated group",
        bots: [
          {
            installation_id: "inst-3",
            agent_id: "agent-3",
            bot_name: "Other Bot",
            bot_identity_issue: "",
          },
        ],
      },
    ];

    renderTab(<IntegrationsTab agent={agent} />);
    expect(screen.getByText("Platform team")).toBeTruthy();
    expect(screen.getByText("Release Bot")).toBeTruthy();
    const connectedLabel = screen.getByText("Connected bot:");
    expect(connectedLabel.parentElement?.parentElement?.classList).toContain(
      "text-caption",
    );
    const groupDescription = screen.getByText(
      enSettings.dingtalk.groups_description,
    );
    expect(groupDescription.classList).toContain("text-caption");
    expect(groupDescription.classList).not.toContain("text-micro");
    const groupName = screen.getByText("Platform team");
    expect(groupName.classList).toContain("min-w-0");
    expect(groupName.parentElement?.classList).toContain("flex-1");
    const conversationId = screen.getByLabelText(
      "DingTalk group conversation ID cid-platform",
    );
    expect(conversationId.textContent).toBe("cid-platform");
    expect(conversationId.classList).toContain("text-micro");
    expect(conversationId.classList).toContain("text-faint-foreground");
    expect(conversationId.classList).toContain("group-hover:text-muted-foreground");
    const activity = screen.getByTestId("dingtalk-group-activity");
    expect(activity.textContent).toBe("Last active 2h ago·18 mentions");
    expect(activity.classList).toContain("tabular-nums");
    expect(activity.classList).not.toContain("whitespace-nowrap");
    expect(conversationId.parentElement?.parentElement).toBe(
      groupName.parentElement?.parentElement,
    );
    expect(activity.parentElement).not.toBe(screen.getByText("Platform team").parentElement);
    expect(screen.queryByText("Support Bot")).toBeNull();
    expect(screen.queryByText("Unrelated group")).toBeNull();
  });

  it("keeps the member-visible group list available to a non-owner member", () => {
    membersRef.current = [{ user_id: "user-1", role: "member" }];
    installationsRef.current = {
      installations: [
        { id: "inst-1", agent_id: "agent-1", status: "active" },
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
            installation_id: "inst-1",
            agent_id: "agent-1",
            bot_name: "Release Bot",
            bot_identity_issue: "",
          },
        ],
      },
    ];

    renderTab(<IntegrationsTab agent={{ ...agent, owner_id: "user-2" }} />);
    expect(screen.getByText("Platform team")).toBeTruthy();
    expect(screen.getByText("Release Bot")).toBeTruthy();
    expect(screen.getByText("Connected bot:")).toBeTruthy();
    expect(screen.queryByText(/Connected to Agent/i)).toBeNull();
    expect(screen.queryByText("Lark")).toBeNull();
  });

  it("shows the latest permission remediation to an Agent owner even when a cached bot name remains", async () => {
    membersRef.current = [{ user_id: "user-1", role: "member" }];
    installationsRef.current = {
      installations: [
        { id: "inst-1", agent_id: "agent-1", status: "active" },
      ],
      configured: true,
      install_supported: true,
    };
    groupsRef.current.data.groups = [
      {
        conversation_id: "cid-named",
        conversation_title: "Release room",
        bots: [
          {
            installation_id: "inst-1",
            agent_id: "agent-1",
            bot_name: "Release Bot",
            bot_identity_issue: "",
          },
        ],
      },
      {
        conversation_id: "cid-unnamed",
        conversation_title: "",
        bots: [
          {
            installation_id: "inst-1",
            agent_id: "agent-1",
            bot_name: "",
            bot_identity_issue: "missing_qyapi_chat_manage",
          },
        ],
      },
    ];
    renderTab(<IntegrationsTab agent={agent} />);
    expect(screen.getByText("Untitled DingTalk group")).toBeTruthy();
    expect(screen.getByText("Release Bot")).toBeTruthy();
    const info = screen.getByRole("button", { name: /qyapi_chat_manage/ });
    await userEvent.click(info);
    const permissionCode = await screen.findByText("qyapi_chat_manage", {
      selector: "code",
    });
    expect(permissionCode.closest('[data-slot="tooltip-content"]')?.textContent).toContain(
      "grant this bot's app the qyapi_chat_manage permission, then @mention the bot again in a DingTalk group where it has been added.",
    );
  });

  it("hides permission remediation from a read-only Agent viewer", () => {
    membersRef.current = [{ user_id: "user-1", role: "member" }];
    installationsRef.current = {
      installations: [
        { id: "inst-1", agent_id: "agent-1", status: "active" },
      ],
      configured: true,
      install_supported: true,
    };
    groupsRef.current.data.groups = [
      {
        conversation_id: "cid-unnamed",
        conversation_title: "Visible group",
        bots: [
          {
            installation_id: "inst-1",
            agent_id: "agent-1",
            bot_name: "",
            bot_identity_issue: "missing_qyapi_chat_manage",
          },
        ],
      },
    ];

    renderTab(<IntegrationsTab agent={{ ...agent, owner_id: "user-2" }} />);
    expect(screen.getByText("Identity unavailable")).toBeTruthy();
    expect(screen.queryByRole("button", { name: /qyapi_chat_manage/ })).toBeNull();
  });

  it("renders group loading, empty, and retryable error states", async () => {
    installationsRef.current = {
      installations: [
        { id: "inst-1", agent_id: "agent-1", status: "active" },
      ],
      configured: true,
      install_supported: true,
    };
    groupsRef.current.isLoading = true;
    groupsRef.current.data = undefined as never;
    const loading = renderTab(<IntegrationsTab agent={agent} />);
    expect(screen.getByText("Loading groups…")).toBeTruthy();
    loading.unmount();

    groupsRef.current.isLoading = false;
    groupsRef.current.data = {
      groups: [],
      group_discovery_supported: true,
    };
    const empty = renderTab(<IntegrationsTab agent={agent} />);
    expect(screen.getByText("No group messages observed yet.")).toBeTruthy();
    empty.unmount();

    groupsRef.current.isError = true;
    groupsRef.current.data = undefined as never;
    renderTab(<IntegrationsTab agent={agent} />);
    await userEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(groupsRef.current.refetch).toHaveBeenCalledOnce();
  });

  it("renders discovery UI only when the backend explicitly supports it", () => {
    installationsRef.current = {
      installations: [
        { id: "inst-1", agent_id: "agent-1", status: "active" },
      ],
      configured: true,
      install_supported: true,
    };
    groupsRef.current.data = {
      groups: [],
      group_discovery_supported: false,
    };

    renderTab(<IntegrationsTab agent={agent} />);
    expect(screen.getByText("Connected bot:")).toBeTruthy();
    expect(screen.queryByText("Identity unavailable")).toBeNull();
    expect(screen.queryByTestId("dingtalk-bot-groups")).toBeNull();
  });

  it("shows the coming-soon notice when the install transport is not wired", () => {
    installationsRef.current = {
      installations: [],
      configured: true,
      install_supported: false,
    };
    renderTab(<IntegrationsTab agent={agent} />);
    expect(screen.getByText(/installation coming soon/i)).toBeTruthy();
    expect(screen.queryByTestId("lark-bind-button")).toBeNull();
  });

  it("shows the not-enabled notice when the deployment has no Lark key", () => {
    installationsRef.current = {
      installations: [],
      configured: false,
      install_supported: false,
    };
    renderTab(<IntegrationsTab agent={agent} />);
    expect(screen.getByText(/Lark integration not enabled/i)).toBeTruthy();
    expect(screen.queryByTestId("lark-bind-button")).toBeNull();
  });

  it("points members at Settings when they are neither an admin nor the agent owner", () => {
    // A plain member viewing an agent owned by someone else can manage
    // neither platform, so the read-only note replaces both sections.
    membersRef.current = [{ user_id: "user-1", role: "member" }];
    renderTab(<IntegrationsTab agent={{ ...agent, owner_id: "user-2" }} />);
    expect(
      screen.getAllByText(/Only workspace owners and admins can manage this connection/i).length,
    ).toBeGreaterThanOrEqual(1);
    expect(screen.queryByTestId("lark-bind-button")).toBeNull();
    expect(screen.queryByTestId("slack-bind-button")).toBeNull();
    expect(screen.queryByTestId("wecom-bind-button")).toBeNull();
    expect(screen.queryByTestId("telegram-bind-button")).toBeNull();
  });

  it("lets a non-admin agent owner bind Lark and DingTalk", () => {
    // The agent's owner (user-1) is only a plain workspace member. Lark and
    // DingTalk both authorize the target agent's owner through canManageAgent;
    // Slack, WeCom and Telegram remain workspace owner/admin-only.
    membersRef.current = [{ user_id: "user-1", role: "member" }];
    renderTab(<IntegrationsTab agent={agent} />);
    const larkButton = screen.getByTestId("lark-bind-button");
    expect(larkButton.getAttribute("data-agent-id")).toBe("agent-1");
    expect(larkButton.getAttribute("data-agent-owner-id")).toBe("user-1");
    expect(screen.getByTestId("dingtalk-agent-connect")).toBeTruthy();
    expect(screen.queryByTestId("slack-bind-button")).toBeNull();
    expect(screen.queryByTestId("wecom-bind-button")).toBeNull();
    expect(screen.queryByTestId("telegram-bind-button")).toBeNull();
    // The Slack, WeCom and Telegram sections fall back to the shared members note.
    expect(
      screen.getAllByText(/Only workspace owners and admins can manage this connection/i),
    ).toHaveLength(3);
  });

  it("renders the bind entry (not coming-soon) when installs are unavailable but the agent is already bound", () => {
    // install_supported governs only NEW installs; an already-bound agent
    // must still surface its connected state instead of "coming soon"
    // (regression for the must-fix on MUL-2988).
    installationsRef.current = {
      installations: [{ agent_id: "agent-1", status: "active" }],
      configured: true,
      install_supported: false,
    };
    renderTab(<IntegrationsTab agent={agent} />);
    expect(screen.getByTestId("lark-bind-button")).toBeTruthy();
    expect(screen.queryByText(/installation coming soon/i)).toBeNull();
  });
});
