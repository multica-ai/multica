// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enSettings from "../../locales/en/settings.json";

const mocks = vi.hoisted(() => ({
  list: vi.fn(),
  begin: vi.fn(),
  poll: vi.fn(),
  cancel: vi.fn(),
  revoke: vi.fn(),
  member: { role: "admin" },
}));
vi.mock("@multica/core/api", () => ({
  api: {
    listWeixinInstallations: mocks.list,
    beginWeixinLogin: mocks.begin,
    pollWeixinLogin: mocks.poll,
    cancelWeixinLogin: mocks.cancel,
    deleteWeixinInstallation: mocks.revoke,
  },
  clientErrorMessage: () => undefined,
}));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "workspace-1" }));
vi.mock("@multica/core/permissions", () => ({
  useCurrentMember: () => ({ member: mocks.member }),
}));
vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({ getAgentName: () => "Assistant" }),
}));
vi.mock("../../common/actor-avatar", () => ({ ActorAvatar: () => null }));

import { WeixinAgentBindButton } from "./weixin-tab";

const login = {
  id: "login-1",
  status: "wait",
  qr_code_url: "https://weixin.qq.com/test-qr",
  expires_at: new Date(Date.now() + 300_000).toISOString(),
};
const active = { id: "bot-1", agent_id: "agent-1", status: "active" };

function renderButton() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  const result = render(
    <QueryClientProvider client={client}>
      <I18nProvider
        locale="en"
        resources={{ en: { common: enCommon, settings: enSettings } }}
      >
        <WeixinAgentBindButton agentId="agent-1" agentName="Assistant" />
      </I18nProvider>
    </QueryClientProvider>,
  );
  return { ...result, client };
}

describe("WeChat QR connection", () => {
  beforeEach(() => {
    cleanup();
    vi.resetAllMocks();
    mocks.member.role = "admin";
    mocks.list.mockResolvedValue({
      configured: true,
      install_supported: true,
      installations: [],
    });
    mocks.begin.mockResolvedValue(login);
    mocks.poll.mockResolvedValue(login);
    mocks.cancel.mockResolvedValue(undefined);
    mocks.revoke.mockResolvedValue(undefined);
  });

  it("renders the QR, submits the phone code, and refreshes the connection after confirmation", async () => {
    const user = userEvent.setup();
    mocks.poll
      .mockResolvedValueOnce({ ...login, status: "need_verifycode" })
      .mockImplementation(async () => {
        mocks.list.mockResolvedValue({
          configured: true,
          install_supported: true,
          installations: [active],
        });
        return {
          id: login.id,
          status: "confirmed",
          installation_id: active.id,
          expires_at: login.expires_at,
        };
      });
    renderButton();
    await user.click(
      await screen.findByRole("button", { name: "Connect WeChat" }),
    );
    expect(
      await screen.findByRole("dialog", {
        name: "Connect WeChat to Assistant",
      }),
    ).toBeVisible();
    expect(
      await screen.findByTitle("WeChat connection QR code"),
    ).toBeInTheDocument();
    await user.type(
      await screen.findByLabelText("Verification code"),
      "001234",
    );
    await user.click(screen.getByRole("button", { name: "Verify" }));
    await waitFor(() =>
      expect(mocks.poll).toHaveBeenCalledWith(
        "workspace-1",
        "login-1",
        "001234",
      ),
    );
    expect(await screen.findByText("Connected", { exact: true })).toBeVisible();
  });

  it("waits for server cancellation before closing and retains a failed cancellation", async () => {
    const user = userEvent.setup();
    mocks.cancel.mockRejectedValueOnce(new Error("offline"));
    renderButton();
    await user.click(
      await screen.findByRole("button", { name: "Connect WeChat" }),
    );
    await screen.findByTitle("WeChat connection QR code");
    await user.click(screen.getAllByRole("button", { name: "Close" })[0]!);
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Could not complete this action",
    );
    expect(screen.getByRole("dialog")).toBeVisible();
    await user.click(screen.getAllByRole("button", { name: "Close" })[0]!);
    await waitFor(() =>
      expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
    );
    expect(mocks.cancel).toHaveBeenCalledWith("workspace-1", "login-1");
  });

  it("stops on expired QR and lets the user request a new one", async () => {
    const user = userEvent.setup();
    mocks.poll.mockResolvedValue({
      ...login,
      status: "expired",
      qr_code_url: undefined,
    });
    renderButton();
    await user.click(
      await screen.findByRole("button", { name: "Connect WeChat" }),
    );
    expect(
      await screen.findByText("This QR code has expired. Generate a new one."),
    ).toBeVisible();
    expect(
      screen.queryByTitle("WeChat connection QR code"),
    ).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "New QR code" }));
    await waitFor(() => expect(mocks.begin).toHaveBeenCalledTimes(2));
  });

  it("never offers connection to workspace members", async () => {
    mocks.member.role = "member";
    renderButton();
    expect(
      await screen.findByText("A workspace administrator can connect WeChat."),
    ).toBeVisible();
    expect(
      screen.queryByRole("button", { name: "Connect WeChat" }),
    ).not.toBeInTheDocument();
    expect(mocks.begin).not.toHaveBeenCalled();
  });

  it("requires confirmation and keeps a failed disconnect visible", async () => {
    const user = userEvent.setup();
    mocks.list.mockResolvedValue({ configured: true, installations: [active] });
    mocks.revoke.mockRejectedValue(new Error("offline"));
    renderButton();
    await user.click(await screen.findByRole("button", { name: "Disconnect" }));
    expect(mocks.revoke).not.toHaveBeenCalled();
    await user.click(
      screen.getAllByRole("button", { name: "Disconnect" }).at(-1)!,
    );
    expect(await screen.findByRole("alert")).toBeVisible();
    expect(screen.getByRole("dialog")).toBeVisible();
    expect(mocks.revoke).toHaveBeenCalledWith("workspace-1", "bot-1");
  });
});
