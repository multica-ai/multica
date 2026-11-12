import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { Agent } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { api, ApiError } from "@multica/core/api";
import {
  AgentChannelsSection,
  AgentChannelLinks,
} from "../../agents/components/agent-channels-section";
import { ChannelsTab } from "./channels-tab";
import { ChannelConnectDialog } from "./channel-connect-dialog";

const state = vi.hoisted(() => ({
  role: "admin",
  member: true,
  search: "tab=channels",
  replace: vi.fn(),
  open: vi.fn(),
}));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/permissions", () => ({
  useCurrentMember: () => ({
    role: state.role,
    userId: "user-1",
    member: state.member ? { role: state.role } : null,
  }),
}));
vi.mock("@multica/core/paths", () => ({
  useCurrentWorkspace: () => ({ id: "ws-1", name: "Acme" }),
  useWorkspacePaths: () => ({
    agentDetail: (id: string) => `/acme/agents/${id}`,
  }),
}));
vi.mock("../../navigation/context", async (original) => ({
  ...(await original<typeof import("../../navigation/context")>()),
  useNavigation: () => ({
    pathname: "/acme/settings",
    searchParams: new URLSearchParams(state.search),
    replace: state.replace,
    push: vi.fn(),
  }),
}));
vi.mock("../../platform", () => ({ openExternal: state.open }));
vi.mock("../../common/actor-avatar", () => ({
  ActorAvatar: ({ actorId }: { actorId: string }) => (
    <span data-testid={`avatar-${actorId}`} />
  ),
}));
vi.mock("@multica/core/api", async (original) => ({
  ...(await original<typeof import("@multica/core/api")>()),
  api: Object.fromEntries(
    [
      "listLarkInstallations",
      "listSlackInstallations",
      "listDingTalkInstallations",
      "listWecomInstallations",
      "listTelegramInstallations",
      "registerSlackBYO",
      "registerDingTalkBYO",
      "registerWecomBYO",
      "registerTelegramBot",
      "deleteLarkInstallation",
      "deleteSlackInstallation",
      "deleteDingTalkInstallation",
      "deleteWecomInstallation",
      "deleteTelegramInstallation",
      "listAgentDingTalkGroups",
      "listAgents",
      "beginLarkInstall",
      "getLarkInstallStatus",
      "beginWecomInstall",
      "getWecomInstallStatus",
    ].map((name) => [name, vi.fn()]),
  ),
}));

const agent = {
  id: "agent-1",
  name: "Mika",
  owner_id: "user-1",
  archived_at: null,
} as Agent;
const otherAgent = {
  ...agent,
  id: "agent-2",
  name: "Support",
  owner_id: "user-2",
};
const active = {
  id: "i1",
  workspace_id: "ws-1",
  agent_id: agent.id,
  status: "active",
};
const listing = (installations: unknown[] = []) => ({
  configured: true,
  install_supported: true,
  installations,
});

function show(ui: React.ReactElement) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  });
  return {
    ...renderWithI18n(
      <QueryClientProvider client={qc}>{ui}</QueryClientProvider>,
    ),
    qc,
  };
}

beforeEach(() => {
  vi.clearAllMocks();
  state.role = "admin";
  state.member = true;
  state.search = "tab=channels";
  for (const name of [
    "listLarkInstallations",
    "listSlackInstallations",
    "listDingTalkInstallations",
    "listWecomInstallations",
    "listTelegramInstallations",
  ] as const) {
    vi.mocked(api[name]).mockResolvedValue(listing() as never);
  }
  vi.mocked(api.listAgents).mockResolvedValue([agent, otherAgent]);
  vi.mocked(api.listAgentDingTalkGroups).mockResolvedValue({
    groups: [],
    group_discovery_supported: false,
  });
  vi.mocked(api.beginLarkInstall).mockResolvedValue({
    session_id: "s1",
    qr_code_url: "https://accounts.feishu.cn/example",
    expires_in_seconds: 300,
    poll_interval_seconds: 2,
  });
});

describe("agent messaging channels", () => {
  it.each([
    ["feishu", "open.feishu.cn", "Manage in Feishu"],
    ["lark", "open.larksuite.com", "Manage in Lark"],
    [undefined, "open.feishu.cn", "Manage in Feishu"],
  ])(
    "keeps management and troubleshooting available for an existing %s bot",
    async (region, host, label) => {
      vi.mocked(api.listLarkInstallations).mockResolvedValue({
        ...listing([{ ...active, app_id: "cli_a&b", region }]),
        install_supported: false,
      } as never);
      show(<AgentChannelsSection agent={agent} />);
      fireEvent.click(
        await screen.findByRole("button", { name: "Manage Lark" }),
      );
      fireEvent.click(await screen.findByRole("menuitem", { name: label }));
      expect(state.open).toHaveBeenCalledWith(`https://${host}/app/cli_a%26b`);
      fireEvent.click(screen.getByRole("button", { name: "Manage Lark" }));
      fireEvent.click(
        await screen.findByRole("menuitem", { name: "Troubleshooting" }),
      );
      expect(state.open).toHaveBeenCalledWith(
        "https://multica.ai/docs/lark-bot-integration",
      );
    },
  );

  it("shows actionable DingTalk permission help instead of a healthy status", async () => {
    vi.mocked(api.listDingTalkInstallations).mockResolvedValue(
      listing([active]) as never,
    );
    vi.mocked(api.listAgentDingTalkGroups).mockResolvedValue({
      groups: [],
      group_discovery_supported: true,
      bot_identities: {
        i1: {
          installation_id: "i1",
          agent_id: agent.id,
          bot_name: "",
          bot_identity_issue: "missing_qyapi_chat_manage",
        },
      },
    });
    show(<AgentChannelsSection agent={agent} />);
    expect(await screen.findByText("qyapi_chat_manage")).toBeInTheDocument();
    expect(screen.getByText("Identity unavailable")).toBeInTheDocument();
    expect(screen.getByText(/then @mention the bot again/)).toBeInTheDocument();
    expect(screen.queryByText("Connected", { exact: true })).toBeNull();
  });

  it("keeps recovery controls hidden for members who cannot manage the bot", async () => {
    state.role = "member";
    vi.mocked(api.listLarkInstallations).mockResolvedValue(
      listing([
        { ...active, agent_id: otherAgent.id, app_id: "cli_readonly" },
      ]) as never,
    );
    vi.mocked(api.listDingTalkInstallations).mockResolvedValue(
      listing([{ ...active, id: "d1", agent_id: otherAgent.id }]) as never,
    );
    vi.mocked(api.listAgentDingTalkGroups).mockResolvedValue({
      groups: [],
      group_discovery_supported: true,
      bot_identities: {
        d1: {
          installation_id: "d1",
          agent_id: otherAgent.id,
          bot_name: "",
          bot_identity_issue: "missing_qyapi_chat_manage",
        },
      },
    });
    show(<AgentChannelsSection agent={otherAgent} />);
    expect(await screen.findByText("Identity unavailable")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Manage Lark" })).toBeNull();
    expect(screen.queryByText("qyapi_chat_manage")).toBeNull();
  });
  it("does not offer management to someone without workspace membership", async () => {
    state.member = false;
    const { container } = show(<AgentChannelsSection agent={agent} />);
    expect(
      screen.queryByRole("button", { name: "Add messaging channel" }),
    ).toBeNull();
    expect(container).toBeEmptyDOMElement();
    expect(api.listLarkInstallations).not.toHaveBeenCalled();
  });
  it("hides the empty section from a member who cannot manage the agent", async () => {
    state.role = "member";
    const { container } = show(<AgentChannelsSection agent={otherAgent} />);
    await waitFor(() =>
      expect(api.listTelegramInstallations).toHaveBeenCalled(),
    );
    expect(container).toBeEmptyDOMElement();
  });

  it("gives an agent owner Lark and DingTalk, with admin-only platforms labelled", async () => {
    state.role = "member";
    show(<AgentChannelsSection agent={agent} />);
    await waitFor(() =>
      expect(
        screen.getAllByRole("button", { name: "Add messaging channel" }).length,
      ).toBeGreaterThan(0),
    );
    fireEvent.click(
      screen.getAllByRole("button", { name: "Add messaging channel" })[0]!,
    );
    const dialog = await screen.findByRole("dialog");
    expect(
      within(dialog).getByRole("button", { name: /^Feishu$/ }),
    ).toBeEnabled();
    expect(
      within(dialog).getByRole("button", { name: /^DingTalk$/ }),
    ).toBeEnabled();
    expect(
      within(dialog).getByRole("button", { name: /Slack Ask an admin/ }),
    ).toBeDisabled();
    expect(
      within(dialog).getByRole("button", { name: /Telegram Ask an admin/ }),
    ).toBeDisabled();
    expect(
      within(dialog).getByRole("button", { name: /WeCom Ask an admin/ }),
    ).toBeDisabled();
  });

  it("hides unconfigured platforms from members", async () => {
    state.role = "member";
    vi.mocked(api.listSlackInstallations).mockResolvedValue({
      configured: false,
      installations: [],
    });
    show(
      <ChannelConnectDialog
        agents={[agent]}
        initialAgent={agent}
        open
        onOpenChange={vi.fn()}
      />,
    );
    await screen.findByRole("button", { name: /^DingTalk$/ });
    expect(screen.queryByRole("button", { name: /Slack/ })).toBeNull();
  });

  it("shows connected channels and a chat link to a read-only member", async () => {
    state.role = "member";
    vi.mocked(api.listTelegramInstallations).mockResolvedValue(
      listing([{ ...active, bot_username: "mika_bot" }]) as never,
    );
    show(
      <>
        <AgentChannelLinks agent={otherAgent} />
        <AgentChannelsSection agent={otherAgent} />
        <AgentChannelsSection agent={agent} />
        <AgentChannelLinks agent={agent} />
      </>,
    );
    fireEvent.click(
      await screen.findByRole("button", { name: "Find me on Telegram" }),
    );
    expect(state.open).toHaveBeenCalledWith("https://t.me/mika_bot");
    expect(
      screen.queryByRole("button", { name: "Manage Telegram" }),
    ).toBeNull();
  });

  it("updates Lark from waiting to connected when inbound evidence arrives", async () => {
    vi.mocked(api.listLarkInstallations).mockResolvedValue(
      listing([{ ...active, app_id: "cli_mika" }]) as never,
    );
    const { qc } = show(<AgentChannelsSection agent={agent} />);
    expect(
      await screen.findByText("Waiting for first message"),
    ).toBeInTheDocument();
    qc.setQueryData(
      ["lark", "ws-1", "installations"],
      listing([
        {
          ...active,
          app_id: "cli_mika",
          first_message_received_at: "2026-11-10T00:00:00Z",
        },
      ]),
    );
    await waitFor(() =>
      expect(screen.queryByText("Waiting for first message")).toBeNull(),
    );
    expect(screen.getByText("Connected")).toBeInTheDocument();
  });

  it("does not display revoked connections or another agent's connection", async () => {
    vi.mocked(api.listTelegramInstallations).mockResolvedValue(
      listing([
        { ...active, status: "revoked" },
        { ...active, id: "i2", agent_id: "agent-2" },
      ]) as never,
    );
    show(<AgentChannelsSection agent={agent} />);
    await screen.findAllByRole("button", { name: "Add messaging channel" });
    expect(screen.queryByText("Telegram")).toBeNull();
  });

  it.each(["lark", "slack", "dingtalk", "wecom", "telegram"] as const)(
    "confirms %s disconnect and preserves the connection on failure",
    async (channel) => {
      const methods = {
        lark: ["listLarkInstallations", "deleteLarkInstallation"],
        slack: ["listSlackInstallations", "deleteSlackInstallation"],
        dingtalk: ["listDingTalkInstallations", "deleteDingTalkInstallation"],
        wecom: ["listWecomInstallations", "deleteWecomInstallation"],
        telegram: ["listTelegramInstallations", "deleteTelegramInstallation"],
      } as const;
      const [list, remove] = methods[channel];
      vi.mocked(api[list]).mockResolvedValue(listing([active]) as never);
      vi.mocked(api[remove])
        .mockRejectedValueOnce(new Error("Disconnect failed"))
        .mockResolvedValueOnce(undefined);
      show(<AgentChannelsSection agent={agent} />);
      fireEvent.click(await screen.findByRole("button", { name: /^Manage / }));
      fireEvent.click(
        await screen.findByRole("menuitem", { name: "Disconnect" }),
      );
      const dialog = await screen.findByRole("alertdialog");
      expect(api[remove]).not.toHaveBeenCalled();
      fireEvent.click(
        within(dialog).getByRole("button", { name: "Disconnect" }),
      );
      await waitFor(() =>
        expect(api[remove]).toHaveBeenCalledWith("ws-1", "i1"),
      );
      await waitFor(() =>
        expect(
          within(dialog).getByRole("button", { name: "Disconnect" }),
        ).toBeEnabled(),
      );
      expect(dialog).toBeInTheDocument();
      fireEvent.click(
        within(dialog).getByRole("button", { name: "Disconnect" }),
      );
      await waitFor(() => expect(screen.queryByRole("alertdialog")).toBeNull());
    },
  );
});

const platformName = {
  slack: "Slack",
  dingtalk: "DingTalk",
  wecom: "WeCom",
  telegram: "Telegram",
} as const;

describe("shared connection dialog", () => {
  it("offers only Feishu for new connections while international Lark is disabled", async () => {
    show(
      <ChannelConnectDialog
        agents={[agent]}
        initialAgent={agent}
        initialChannel="lark"
        open
        onOpenChange={vi.fn()}
      />,
    );
    const feishu = await screen.findByRole("button", { name: "Feishu" });
    await waitFor(() => expect(feishu).toBeEnabled());
    expect(screen.queryByRole("button", { name: "Lark" })).toBeNull();
    expect(
      screen.queryByRole("option", { name: "Lark (International)" }),
    ).toBeNull();
    fireEvent.click(feishu);
    await waitFor(() =>
      expect(api.beginLarkInstall).toHaveBeenCalledWith(
        "ws-1",
        "agent-1",
        "feishu",
      ),
    );
  });

  it("offers the WeCom QR flow only when the server supports it", async () => {
    vi.mocked(api.listWecomInstallations).mockResolvedValue({
      configured: true,
      install_supported: true,
      scan_install_supported: true,
      installations: [],
    });
    vi.mocked(api.beginWecomInstall).mockResolvedValue({
      session_id: "wecom-session",
      status: "creating",
      poll_interval_seconds: 1,
    });
    show(
      <ChannelConnectDialog
        agents={[agent]}
        initialAgent={agent}
        initialChannel="wecom"
        open
        onOpenChange={vi.fn()}
      />,
    );
    expect(screen.queryByLabelText("Connection method")).toBeNull();
    const wecom = await screen.findByRole("button", { name: "WeCom" });
    await waitFor(() => expect(wecom).toBeEnabled());
    fireEvent.click(wecom);
    await screen.findByTestId("wecom-scan-starting");
    expect(screen.getByLabelText("Connection method")).toHaveValue("scan");
    expect(api.beginWecomInstall).toHaveBeenCalledWith(
      "ws-1",
      "agent-1",
      expect.any(String),
    );
    fireEvent.change(screen.getByLabelText("Connection method"), {
      target: { value: "credentials" },
    });
    expect(screen.queryByTestId("wecom-scan-starting")).toBeNull();
    expect(screen.getByLabelText("Bot ID")).toBeInTheDocument();
    expect(api.registerWecomBYO).not.toHaveBeenCalled();
  });

  it("starts the connection as soon as a platform is clicked", async () => {
    show(
      <ChannelConnectDialog
        agents={[agent]}
        initialAgent={agent}
        open
        onOpenChange={vi.fn()}
      />,
    );
    expect(screen.queryByRole("button", { name: "Continue" })).toBeNull();
    const telegram = await screen.findByRole("button", { name: "Telegram" });
    await waitFor(() => expect(telegram).toBeEnabled());
    fireEvent.click(telegram);
    expect(screen.getByLabelText("Bot token")).toBeInTheDocument();
    expect(screen.getByText("Connect Mika to Telegram.")).toBeInTheDocument();
  });

  it.each([
    [
      "slack",
      "registerSlackBYO",
      ["Bot token (xoxb-)", "App-level token (xapp-)"],
      { bot_token: "first", app_token: "second" },
    ],
    [
      "dingtalk",
      "registerDingTalkBYO",
      ["AppKey (client id)", "AppSecret (client secret)"],
      { client_id: "first", client_secret: "second" },
    ],
    [
      "wecom",
      "registerWecomBYO",
      ["Bot ID", "Long-connection secret"],
      { bot_id: "first", secret: "second", bot_name: undefined },
    ],
    ["telegram", "registerTelegramBot", ["Bot token"], { bot_token: "first" }],
  ] as const)(
    "submits %s through the existing API and closes after success",
    async (channel, method, labels, payload) => {
      const close = vi.fn();
      vi.mocked(api[method]).mockResolvedValue(active as never);
      show(
        <ChannelConnectDialog
          agents={[agent]}
          initialAgent={agent}
          initialChannel={channel}
          open
          onOpenChange={close}
        />,
      );
      const row = await screen.findByRole("button", {
        name: new RegExp(`^${platformName[channel]}$`),
      });
      await waitFor(() => expect(row).toBeEnabled());
      fireEvent.click(row);
      labels.forEach((label, index) =>
        fireEvent.change(screen.getByLabelText(label), {
          target: { value: index === 0 ? " first " : " second " },
        }),
      );
      expect(screen.getByLabelText(labels.at(-1)!)).toHaveAttribute(
        "type",
        "password",
      );
      fireEvent.click(screen.getByRole("button", { name: "Connect" }));
      await waitFor(() =>
        expect(api[method]).toHaveBeenCalledWith("ws-1", "agent-1", payload),
      );
      await waitFor(() => expect(close).toHaveBeenCalledWith(false));
    },
  );

  it("keeps inputs and localized WeCom errors after rejected credentials", async () => {
    vi.mocked(api.registerWecomBYO).mockRejectedValue(
      new ApiError("English server detail", 400, "Bad Request", {
        code: "wecom_credentials_rejected",
      }),
    );
    show(
      <ChannelConnectDialog
        agents={[agent]}
        initialAgent={agent}
        initialChannel="wecom"
        open
        onOpenChange={vi.fn()}
      />,
    );
    const wecom = await screen.findByRole("button", { name: "WeCom" });
    await waitFor(() => expect(wecom).toBeEnabled());
    fireEvent.click(wecom);
    fireEvent.change(screen.getByLabelText("Bot ID"), {
      target: { value: "bot" },
    });
    fireEvent.change(screen.getByLabelText("Long-connection secret"), {
      target: { value: "secret" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Connect" }));
    expect(await screen.findByRole("alert")).not.toHaveTextContent(
      "English server detail",
    );
    expect(screen.getByLabelText("Long-connection secret")).toHaveValue(
      "secret",
    );
  });

  it("requires an agent selection when opened from the overview", async () => {
    show(
      <ChannelConnectDialog
        agents={[agent, otherAgent]}
        initialChannel="telegram"
        open
        onOpenChange={vi.fn()}
      />,
    );
    const telegram = await screen.findByRole("button", { name: "Telegram" });
    expect(telegram).toBeDisabled();
    fireEvent.change(screen.getByLabelText("Agent"), {
      target: { value: "agent-2" },
    });
    await waitFor(() => expect(telegram).toBeEnabled());
    fireEvent.click(telegram);
    expect(screen.getByLabelText("Bot token")).toBeInTheDocument();
  });
});

describe("workspace overview", () => {
  it("lets admins disconnect an installation whose agent was deleted", async () => {
    vi.mocked(api.listTelegramInstallations).mockResolvedValue(
      listing([{ ...active, agent_id: "deleted-agent" }]) as never,
    );
    vi.mocked(api.deleteTelegramInstallation).mockResolvedValue(undefined);
    show(<ChannelsTab />);
    expect(await screen.findByText("Agent unavailable")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Disconnect" }));
    const dialog = await screen.findByRole("alertdialog");
    expect(api.deleteTelegramInstallation).not.toHaveBeenCalled();
    fireEvent.click(within(dialog).getByRole("button", { name: "Disconnect" }));
    await waitFor(() =>
      expect(api.deleteTelegramInstallation).toHaveBeenCalledWith("ws-1", "i1"),
    );
  });

  it("links each connected agent to their channels, and redirects old detail links", async () => {
    state.search = "tab=channels&integration=slack";
    vi.mocked(api.listSlackInstallations).mockResolvedValue(
      listing([active]) as never,
    );
    show(<ChannelsTab />);
    expect(await screen.findByRole("link", { name: "Mika" })).toHaveAttribute(
      "href",
      "/acme/agents/agent-1?view=channels",
    );
    expect(state.replace).toHaveBeenCalledWith("/acme/settings?tab=channels");
    expect(screen.getByText("Who can connect")).toBeInTheDocument();
    expect(screen.getAllByRole("button", { name: "Connect" })).toHaveLength(5);
  });

  it("reports failed reads as unavailable and keeps a retry control", async () => {
    vi.mocked(api.listTelegramInstallations).mockRejectedValue(
      new Error("offline"),
    );
    show(<ChannelsTab />);
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Status unavailable",
    );
    expect(
      screen.getByRole("button", { name: "Scan again" }),
    ).toBeInTheDocument();
  });
});
