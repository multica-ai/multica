import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen } from "@testing-library/react";
import { renderWithI18n } from "../../test/i18n";

const state = vi.hoisted(() => ({
  search: "",
  connectionError: false,
  pending: false,
  // The IM channels keep revoked rows for audit, so the fixture carries a real
  // status — a row without one is not evidence of a live connection (#8496).
  installationStatus: "active" as string,
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
vi.mock("@multica/core/paths", () => ({
  useCurrentWorkspace: () => ({ id: "ws-1", name: "Acme" }),
}));
vi.mock("@multica/core/permissions", () => ({
  useCurrentMember: () => ({ member: { role: "admin" } }),
}));
vi.mock("@tanstack/react-query", () => ({
  queryOptions: <T,>(opts: T) => opts,
  useQuery: (opts: { select?: (data: unknown) => unknown }) => ({
    data: opts.select?.({
      installations: [{ id: "one", status: state.installationStatus }],
    }),
    isPending: state.pending,
    isError: state.connectionError,
  }),
}));
vi.mock("./lark-tab", () => ({ LarkTab: () => <div>Lark detail</div> }));
vi.mock("./slack-tab", () => ({ SlackTab: () => <div>Slack detail</div> }));
vi.mock("./dingtalk-tab", () => ({ DingTalkTab: () => <div>DingTalk detail</div> }));
vi.mock("./wecom-tab", () => ({ WecomTab: () => <div>WeCom detail</div> }));
vi.mock("./telegram-tab", () => ({ TelegramTab: () => <div>Telegram detail</div> }));

import { ChannelsTab } from "./channels-tab";

beforeEach(() => {
  state.search = "tab=channels";
  state.connectionError = false;
  state.pending = false;
  state.installationStatus = "active";
  state.push.mockClear();
});

describe("ChannelsTab", () => {
  it("lists only messaging channels, each with its own mark and status", () => {
    renderWithI18n(<ChannelsTab />);

    expect(screen.getByRole("link", { name: /Slack Connected/ })).toBeInTheDocument();
    expect(screen.queryByRole("link", { name: /GitHub/ })).toBeNull();
    expect(screen.queryByRole("link", { name: /Composio/ })).toBeNull();
    expect(screen.queryByText("Slack detail")).toBeNull();
    const shapes = ["lark", "slack", "dingtalk", "wecom", "telegram"].map(
      (channel) => screen.getByTestId(`integration-channel-icon-${channel}`).innerHTML,
    );
    expect(new Set(shapes).size).toBe(5);

    fireEvent.click(screen.getByRole("link", { name: /Slack Connected/ }));
    expect(state.push).toHaveBeenCalledWith("/acme/settings?tab=channels&integration=slack");
  });

  it("does not report a revoked bot as connected", () => {
    state.installationStatus = "revoked";
    renderWithI18n(<ChannelsTab />);
    expect(screen.getByRole("link", { name: /Slack Not connected/ })).toBeInTheDocument();
  });

  it.each([
    ["pending", "Checking..."],
    ["error", "Status unavailable"],
  ] as const)("never reports a %s read as disconnected", (kind, label) => {
    if (kind === "pending") state.pending = true;
    else state.connectionError = true;
    renderWithI18n(<ChannelsTab />);
    expect(screen.getByRole("link", { name: new RegExp(`Slack ${label}`) })).toBeInTheDocument();
  });

  it("opens one channel with a breadcrumb back to the list", () => {
    state.search = "tab=channels&integration=slack";
    renderWithI18n(<ChannelsTab />);

    expect(screen.getByText("Slack detail")).toBeInTheDocument();
    expect(screen.queryByText("Lark detail")).toBeNull();
    expect(screen.getByRole("link", { name: "Messaging" })).toHaveAttribute(
      "href",
      "/acme/settings?tab=channels",
    );
  });

  it("opens the retired Lark bookmark on the Lark page", () => {
    state.search = "tab=lark";
    renderWithI18n(<ChannelsTab />);
    expect(screen.getByText("Lark detail")).toBeInTheDocument();
  });
});
