// @vitest-environment jsdom

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Agent, WeixinInstallation } from "@multica/core/types";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enSettings from "../../locales/en/settings.json";

type MemberRole = "owner" | "admin" | "member";

const membersRef = vi.hoisted(() => ({
  current: [{ user_id: "user-1", role: "member" as MemberRole }],
}));

const apiMock = vi.hoisted(() => ({
  listWeixinInstallations: vi.fn(),
  startWeixinLogin: vi.fn(),
  getWeixinLogin: vi.fn(),
  submitWeixinVerifyCode: vi.fn(),
  completeWeixinLogin: vi.fn(),
  deleteWeixinInstallation: vi.fn(),
}));

vi.mock("@multica/core/api", () => ({ api: apiMock }));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));

vi.mock("@multica/core/workspace/queries", () => ({
  memberListOptions: () => ({
    queryKey: ["members"],
    queryFn: async () => membersRef.current,
  }),
}));

vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({ getMemberName: (id: string) => `Member ${id}` }),
}));

vi.mock("@multica/core/auth", () => {
  const state = { user: { id: "user-1" } };
  const useAuthStore = Object.assign(
    (sel?: (s: typeof state) => unknown) => (sel ? sel(state) : state),
    { getState: () => state },
  );
  return { useAuthStore };
});

vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

import { WeixinAgentSection } from "./weixin-tab";

const agent = { id: "agent-1", name: "Mika" } as Agent;

function installation(overrides: Partial<WeixinInstallation> = {}): WeixinInstallation {
  return {
    id: "inst-1",
    workspace_id: "ws-1",
    agent_id: "agent-1",
    account_id: "bot-im-bot",
    installer_user_id: "user-1",
    status: "active",
    last_error: null,
    created_at: "2026-09-27T00:00:00Z",
    ...overrides,
  };
}

function renderSection(children: ReactNode) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <I18nProvider locale="en" resources={{ en: { common: enCommon, settings: enSettings } }}>
        {children}
      </I18nProvider>
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  vi.clearAllMocks();
  membersRef.current = [{ user_id: "user-1", role: "member" }];
  apiMock.listWeixinInstallations.mockResolvedValue({
    installations: [],
    configured: true,
    install_supported: true,
  });
});

afterEach(cleanup);

describe("WeixinAgentSection", () => {
  it("tells the member when the deployment has no Weixin host", async () => {
    apiMock.listWeixinInstallations.mockResolvedValue({ installations: [], configured: false });
    renderSection(<WeixinAgentSection agent={agent} />);
    expect(await screen.findByText("WeChat integration not enabled")).toBeTruthy();
    expect(screen.queryByTestId("weixin-agent-connect")).toBeNull();
  });

  it("explains an unreachable host instead of offering a connect button", async () => {
    apiMock.listWeixinInstallations.mockResolvedValue({
      installations: [],
      configured: true,
      install_supported: false,
    });
    renderSection(<WeixinAgentSection agent={agent} />);
    expect(await screen.findByText(/isn't reachable right now/)).toBeTruthy();
    expect(screen.queryByTestId("weixin-agent-connect")).toBeNull();
  });

  it("lets a plain member disconnect only their own Weixin", async () => {
    apiMock.listWeixinInstallations.mockResolvedValue({
      installations: [
        installation({ id: "theirs", installer_user_id: "user-2" }),
        installation({ id: "mine" }),
        installation({ id: "other-agent", agent_id: "agent-2" }),
      ],
      configured: true,
      install_supported: true,
    });
    renderSection(<WeixinAgentSection agent={agent} />);

    const rows = await screen.findAllByTestId("weixin-installation");
    expect(rows).toHaveLength(2);
    // Own connection first, then others'.
    expect(rows[0]!.textContent).toContain("Your WeChat is connected");
    expect(rows[1]!.textContent).toContain("Member user-2's WeChat is connected");
    expect(screen.getAllByTestId("weixin-installation-disconnect")).toHaveLength(1);
    // Already connected: no second connect button.
    expect(screen.queryByTestId("weixin-agent-connect")).toBeNull();
  });

  it("lets an admin disconnect anyone's Weixin", async () => {
    membersRef.current = [{ user_id: "user-1", role: "admin" }];
    apiMock.listWeixinInstallations.mockResolvedValue({
      installations: [installation({ id: "theirs", installer_user_id: "user-2" })],
      configured: true,
      install_supported: true,
    });
    renderSection(<WeixinAgentSection agent={agent} />);
    await waitFor(() =>
      expect(screen.getAllByTestId("weixin-installation-disconnect")).toHaveLength(1),
    );
  });

  it("shows why a relay is not running", async () => {
    apiMock.listWeixinInstallations.mockResolvedValue({
      installations: [installation({ status: "error", last_error: "Weixin credentials are gone" })],
      configured: true,
      install_supported: true,
    });
    renderSection(<WeixinAgentSection agent={agent} />);
    expect(await screen.findByText("Weixin credentials are gone")).toBeTruthy();
  });

  it("scans, then completes the login exactly once", async () => {
    const user = userEvent.setup();
    apiMock.startWeixinLogin.mockResolvedValue({
      id: "login-1",
      state: "waiting",
      qr_content: "https://weixin.example/qr",
      message: "",
      verify_code_invalid: false,
    });
    apiMock.getWeixinLogin.mockResolvedValue({
      id: "login-1",
      state: "connected",
      qr_content: "https://weixin.example/qr",
      message: "",
      verify_code_invalid: false,
    });
    apiMock.completeWeixinLogin.mockResolvedValue(installation());
    renderSection(<WeixinAgentSection agent={agent} />);

    await user.click(await screen.findByTestId("weixin-agent-connect"));

    expect(apiMock.startWeixinLogin).toHaveBeenCalledWith("ws-1", "agent-1");
    await waitFor(() => expect(apiMock.completeWeixinLogin).toHaveBeenCalledWith("ws-1", "login-1"));
    await waitFor(() => expect(screen.queryByTestId("weixin-connect-dialog")).toBeNull());
    expect(apiMock.startWeixinLogin).toHaveBeenCalledTimes(1);
    expect(apiMock.completeWeixinLogin).toHaveBeenCalledTimes(1);
  });

  it("renders the QR code while waiting for a scan", async () => {
    const user = userEvent.setup();
    const waiting = {
      id: "login-1",
      state: "waiting",
      qr_content: "https://weixin.example/qr",
      message: "",
      verify_code_invalid: false,
    };
    apiMock.startWeixinLogin.mockResolvedValue(waiting);
    apiMock.getWeixinLogin.mockResolvedValue(waiting);
    renderSection(<WeixinAgentSection agent={agent} />);

    await user.click(await screen.findByTestId("weixin-agent-connect"));

    expect(await screen.findByTestId("weixin-qr")).toBeTruthy();
    expect(screen.getByText(/Only this WeChat can message Mika/)).toBeTruthy();
  });

  it("asks for the number on the phone and forwards it", async () => {
    const user = userEvent.setup();
    const needCode = {
      id: "login-1",
      state: "need_verify_code",
      qr_content: "https://weixin.example/qr",
      message: "",
      verify_code_invalid: true,
    };
    apiMock.startWeixinLogin.mockResolvedValue(needCode);
    apiMock.getWeixinLogin.mockResolvedValue(needCode);
    apiMock.submitWeixinVerifyCode.mockResolvedValue({ ...needCode, state: "scanned" });
    renderSection(<WeixinAgentSection agent={agent} />);

    await user.click(await screen.findByTestId("weixin-agent-connect"));
    expect(await screen.findByText("That number didn't match. Try again.")).toBeTruthy();
    await user.type(screen.getByTestId("weixin-verify-code"), "42");
    await user.click(screen.getByRole("button", { name: "Continue" }));

    expect(apiMock.submitWeixinVerifyCode).toHaveBeenCalledWith("ws-1", "login-1", "42");
  });

  it("offers a fresh QR code after a failed login", async () => {
    const user = userEvent.setup();
    apiMock.startWeixinLogin.mockRejectedValueOnce(new Error("could not get a Weixin QR code"));
    apiMock.startWeixinLogin.mockResolvedValue({
      id: "login-2",
      state: "waiting",
      qr_content: "https://weixin.example/qr2",
      message: "",
      verify_code_invalid: false,
    });
    apiMock.getWeixinLogin.mockImplementation(async () => ({
      id: "login-2",
      state: "waiting",
      qr_content: "https://weixin.example/qr2",
      message: "",
      verify_code_invalid: false,
    }));
    renderSection(<WeixinAgentSection agent={agent} />);

    await user.click(await screen.findByTestId("weixin-agent-connect"));
    expect(await screen.findByText("could not get a Weixin QR code")).toBeTruthy();
    await user.click(screen.getByRole("button", { name: "Get a new QR code" }));

    expect(await screen.findByTestId("weixin-qr")).toBeTruthy();
    expect(apiMock.startWeixinLogin).toHaveBeenCalledTimes(2);
  });
});
