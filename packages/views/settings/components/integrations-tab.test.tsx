import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen } from "@testing-library/react";
import { ApiError } from "@multica/core/api";
import { configStore } from "@multica/core/config";
import { COMPOSIO_MCP_APPS_FLAG } from "@multica/core/feature-flags";
import { renderWithI18n } from "../../test/i18n";

const state = vi.hoisted(() => ({
  search: "",
  error: null as Error | null,
  connectionError: false,
  pending: false,
  calls: [] as { queryKey: readonly unknown[]; enabled?: boolean }[],
  push: vi.fn(),
}));
vi.mock("../../navigation/context", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../../navigation/context")>()),
  useNavigation: () => ({
    pathname: "/acme/settings",
    searchParams: new URLSearchParams(state.search),
    push: state.push,
  }),
}));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/permissions", () => ({
  useCurrentMember: () => ({ member: { role: "admin" } }),
}));
vi.mock("@tanstack/react-query", () => ({
  queryOptions: <T,>(opts: T) => opts,
  infiniteQueryOptions: <T,>(opts: T) => opts,
  useQuery: (opts: {
    queryKey: readonly unknown[];
    enabled?: boolean;
    select?: (data: unknown) => unknown;
  }) => {
    state.calls.push(opts);
    if (opts.queryKey.includes("toolkits"))
      return { error: opts.enabled === false ? null : state.error };
    const data =
      opts.queryKey[0] === "composio"
        ? [{ status: "active" }]
        : { installations: [{ id: "one" }], connections: [] };
    return {
      data: opts.select?.(data),
      isPending: state.pending,
      isError: state.connectionError,
    };
  },
}));
vi.mock("./github-tab", () => ({ GitHubTab: () => <div>GitHub detail</div> }));
vi.mock("./lark-tab", () => ({ LarkTab: () => <div>Lark detail</div> }));
vi.mock("./composio-tab", () => ({
  ComposioTab: () => <div>Composio detail</div>,
}));
vi.mock("./slack-tab", () => ({ SlackTab: () => <div>Slack detail</div> }));
vi.mock("./dingtalk-tab", () => ({
  DingTalkTab: () => <div>DingTalk detail</div>,
}));
vi.mock("./vcs-tab", () => ({ VCSTab: () => <div>VCS detail</div> }));
vi.mock("./wecom-tab", () => ({ WecomTab: () => <div>WeCom detail</div> }));
vi.mock("./telegram-tab", () => ({
  TelegramTab: () => <div>Telegram detail</div>,
}));

import { IntegrationsTab } from "./integrations-tab";

beforeEach(() => {
  state.search = "";
  state.error = null;
  state.connectionError = false;
  state.pending = false;
  state.calls = [];
  state.push.mockClear();
  configStore.getState().setFeatureFlags({ [COMPOSIO_MCP_APPS_FLAG]: true });
  configStore
    .getState()
    .setAuthConfig({ allowSignup: true, vcsIntegrationAvailable: false });
});

describe("Integration directory", () => {
  it("shows live connection summaries without mounting configuration forms", () => {
    renderWithI18n(<IntegrationsTab />);
    expect(
      screen.getByRole("link", { name: /GitHub Connected/ }),
    ).toBeInTheDocument();
    expect(screen.queryByText("GitHub detail")).not.toBeInTheDocument();
    const shapes = ["lark", "slack", "dingtalk", "wecom", "telegram"].map(
      (channel) =>
        screen.getByTestId(`integration-channel-icon-${channel}`).innerHTML,
    );
    expect(new Set(shapes).size).toBe(5);
    fireEvent.click(screen.getByRole("link", { name: /GitHub Connected/ }));
    expect(state.push).toHaveBeenCalledWith(
      "/acme/settings?tab=integrations&integration=github",
    );
  });
  it("opens only the selected provider and offers a directory link", () => {
    state.search = "tab=integrations&integration=slack";
    renderWithI18n(<IntegrationsTab />);
    expect(screen.getByText("Slack detail")).toBeInTheDocument();
    expect(screen.queryByText("Lark detail")).not.toBeInTheDocument();
    expect(
      screen.getByRole("link", { name: "All integrations" }),
    ).toHaveAttribute("href", "/acme/settings?tab=integrations");
  });
  it("handles the existing GitHub bookmark", () => {
    state.search = "tab=github";
    renderWithI18n(<IntegrationsTab />);
    expect(screen.getByText("GitHub detail")).toBeInTheDocument();
  });
  it.each(["connected=notion", "error=composio_connect_failed"])(
    "mounts the OAuth result handler for %s",
    (callback) => {
      state.search = `tab=integrations&${callback}`;
      renderWithI18n(<IntegrationsTab />);
      expect(screen.getByText("Composio detail")).toBeInTheDocument();
    },
  );
  it("hides disabled Composio entries and disables their queries", () => {
    configStore.getState().setFeatureFlags({ [COMPOSIO_MCP_APPS_FLAG]: false });
    state.search = "tab=integrations&integration=composio";
    renderWithI18n(<IntegrationsTab />);
    expect(screen.queryByText("Composio detail")).not.toBeInTheDocument();
    expect(
      state.calls
        .filter((call) => call.queryKey[0] === "composio")
        .every((call) => call.enabled === false),
    ).toBe(true);
  });
  it("hides Composio when the server reports it unconfigured", () => {
    state.error = new ApiError("unavailable", 503, "Service Unavailable");
    renderWithI18n(<IntegrationsTab />);
    expect(
      screen.queryByRole("link", { name: /Composio/ }),
    ).not.toBeInTheDocument();
  });
  it("only offers self-hosted Git providers when the deployment enables them", () => {
    const { unmount } = renderWithI18n(<IntegrationsTab />);
    expect(
      screen.queryByRole("link", { name: /Git providers/i }),
    ).not.toBeInTheDocument();
    unmount();
    configStore
      .getState()
      .setAuthConfig({ allowSignup: true, vcsIntegrationAvailable: true });
    state.search = "tab=integrations&integration=vcs";
    renderWithI18n(<IntegrationsTab />);
    expect(screen.getByText("VCS detail")).toBeInTheDocument();
  });
  it("does not report failed status reads as disconnected", () => {
    state.connectionError = true;
    renderWithI18n(<IntegrationsTab />);
    expect(screen.getAllByText("Status unavailable").length).toBeGreaterThan(0);
    expect(screen.queryByText("Not connected")).not.toBeInTheDocument();
  });
});
