import { StrictMode, type ReactNode } from "react";
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enSettings from "../../locales/en/settings.json";

// ApiError is re-exported from @multica/core/api; we mock the api module
// itself but still need a real ApiError class so `e instanceof ApiError`
// in the polling catch behaves the way it does at runtime.
const ApiError = vi.hoisted(() => {
  class ApiError extends Error {
    readonly status: number;
    readonly statusText: string;
    readonly body?: unknown;
    constructor(message: string, status: number, statusText = "", body?: unknown) {
      super(message);
      this.name = "ApiError";
      this.status = status;
      this.statusText = statusText;
      this.body = body;
    }
  }
  return ApiError;
});

const mockBeginInstall = vi.hoisted(() => vi.fn());
const mockGetStatus = vi.hoisted(() => vi.fn());
const mockDeleteInstallation = vi.hoisted(() => vi.fn());
const mockInvalidate = vi.hoisted(() => vi.fn());

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

vi.mock("@tanstack/react-query", () => ({
  useQuery: (opts: { queryKey: unknown[]; enabled?: boolean }) => {
    if (opts.enabled === false) return { data: undefined, isLoading: false };
    const key = JSON.stringify(opts.queryKey);
    if (key.includes("members")) return { data: membersRef.current, isLoading: false };
    if (key.includes("installations")) {
      return { data: installationsRef.current, isLoading: false };
    }
    return { data: undefined, isLoading: false };
  },
  useQueryClient: () => ({
    invalidateQueries: mockInvalidate,
  }),
  queryOptions: <T,>(opts: T) => opts,
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "workspace-1",
}));

vi.mock("@multica/core/workspace/queries", () => ({
  memberListOptions: () => ({ queryKey: ["members"], queryFn: vi.fn() }),
}));

const agentNameByIdRef = vi.hoisted(() => ({
  current: new Map<string, string>(),
}));
vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({
    getAgentName: (agentId: string) =>
      agentNameByIdRef.current.get(agentId) ?? "Unknown Agent",
    getMemberName: () => "Unknown",
    getSquadName: () => "Unknown Squad",
    getActorName: () => "Unknown",
    getActorInitials: () => "??",
    getActorAvatarUrl: () => null,
  }),
}));

vi.mock("../../common/actor-avatar", () => ({
  ActorAvatar: ({ actorType, actorId }: { actorType: string; actorId: string }) => (
    <span data-testid="actor-avatar" data-actor-type={actorType} data-actor-id={actorId} />
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
    beginDingTalkInstall: mockBeginInstall,
    getDingTalkInstallStatus: mockGetStatus,
    deleteDingTalkInstallation: mockDeleteInstallation,
  },
  ApiError,
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
  toast: {
    success: vi.fn(),
    error: vi.fn(),
    message: vi.fn(),
  },
}));

vi.mock("react-qr-code", () => {
  const QrStub = ({ value }: { value: string }) => (
    <span data-testid="qr-code" data-value={value} />
  );
  return { QRCode: QrStub, default: QrStub };
});

import { DingTalkAgentBindButton, DingTalkTab } from "./dingtalk-tab";
import { toast } from "sonner";

const TEST_RESOURCES = {
  en: { common: enCommon, settings: enSettings },
};

function I18nWrapper({ children }: { children: ReactNode }) {
  return (
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      {children}
    </I18nProvider>
  );
}

function StrictModeWrapper({ children }: { children: ReactNode }) {
  return (
    <StrictMode>
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        {children}
      </I18nProvider>
    </StrictMode>
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
  agentNameByIdRef.current = new Map();
}

const activeInstallation = {
  id: "inst-1",
  workspace_id: "workspace-1",
  agent_id: "agent-1",
  client_id: "dingabc",
  installer_user_id: "user-1",
  status: "active",
  installed_at: "2026-07-01T00:00:00Z",
  created_at: "2026-07-01T00:00:00Z",
  updated_at: "2026-07-01T00:00:00Z",
};

describe("DingTalkAgentBindButton (CTA gate)", () => {
  beforeEach(resetFixtures);

  it("shows the bind CTA for an owner", () => {
    render(<DingTalkAgentBindButton agentId="agent-1" agentName="Bot" />, {
      wrapper: I18nWrapper,
    });
    expect(screen.getByRole("button", { name: /Bind to DingTalk/i })).toBeTruthy();
  });

  it("hides the bind CTA for a non-admin member (matches backend admin gate)", () => {
    membersRef.current = [{ user_id: "user-1", role: "member" }];
    const { container } = render(
      <DingTalkAgentBindButton agentId="agent-1" agentName="Bot" />,
      { wrapper: I18nWrapper },
    );
    expect(container.innerHTML).toBe("");
  });

  it("hides the bind CTA when install_supported is false", () => {
    installationsRef.current = {
      installations: [],
      configured: true,
      install_supported: false,
    };
    const { container } = render(
      <DingTalkAgentBindButton agentId="agent-1" agentName="Bot" />,
      { wrapper: I18nWrapper },
    );
    expect(container.innerHTML).toBe("");
  });

  it("renders the connected badge for an already-bound agent even when install_supported is false", () => {
    installationsRef.current = {
      installations: [activeInstallation],
      configured: true,
      install_supported: false,
    };
    render(<DingTalkAgentBindButton agentId="agent-1" agentName="Bot" />, {
      wrapper: I18nWrapper,
    });
    expect(screen.getByTestId("dingtalk-agent-bot-connected")).toBeTruthy();
    expect(screen.getByText(/Connected to DingTalk/i)).toBeTruthy();
    expect(screen.queryByRole("button", { name: /Bind to DingTalk/i })).toBeNull();
  });

  it("renders the compact status row when onShowConnectedDetails is provided", async () => {
    const user = userEvent.setup();
    installationsRef.current = {
      installations: [activeInstallation],
      configured: true,
      install_supported: true,
    };
    const onShow = vi.fn();
    render(
      <DingTalkAgentBindButton
        agentId="agent-1"
        agentName="Bot"
        onShowConnectedDetails={onShow}
      />,
      { wrapper: I18nWrapper },
    );
    const row = screen.getByTestId("dingtalk-agent-bot-status");
    await user.click(row);
    expect(onShow).toHaveBeenCalledTimes(1);
  });
});

describe("DingTalkInstallDialog (device flow)", () => {
  beforeEach(() => {
    resetFixtures();
    vi.useFakeTimers({ shouldAdvanceTime: true });
    mockBeginInstall.mockResolvedValue({
      session_id: "sess-1",
      qr_code_url: "https://open-dev.dingtalk.com/fe/app-registration?user_code=MUEU",
      expires_in_seconds: 300,
      poll_interval_seconds: 2,
    });
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  async function openDialog() {
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
    render(<DingTalkAgentBindButton agentId="agent-1" agentName="Bot" />, {
      wrapper: I18nWrapper,
    });
    await user.click(screen.getByRole("button", { name: /Bind to DingTalk/i }));
    await waitFor(() => {
      expect(screen.getByTestId("qr-code")).toBeTruthy();
    });
  }

  it("renders the QR from the begin response and completes on a success poll", async () => {
    mockGetStatus.mockResolvedValue({ status: "success", installation_id: "inst-1" });

    await openDialog();
    expect(screen.getByTestId("qr-code").getAttribute("data-value")).toBe(
      "https://open-dev.dingtalk.com/fe/app-registration?user_code=MUEU",
    );
    expect(mockBeginInstall).toHaveBeenCalledWith("workspace-1", "agent-1");

    await act(async () => {
      await vi.advanceTimersByTimeAsync(2100);
    });

    await waitFor(() => {
      expect(mockInvalidate).toHaveBeenCalled();
      expect(toast.success).toHaveBeenCalled();
    });
  });

  it("surfaces install_failed with the DingTalk fail reason as diagnostics", async () => {
    mockGetStatus.mockResolvedValue({
      status: "error",
      error_reason: "install_failed",
      error_message: "registration: fail: 用户拒绝授权",
    });

    await openDialog();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(2100);
    });

    await waitFor(() => {
      expect(
        screen.getByText(/DingTalk reported the authorization failed or was cancelled/i),
      ).toBeTruthy();
    });
    expect(screen.getByText(/用户拒绝授权/)).toBeTruthy();
    expect(screen.getByRole("button", { name: /Scan again/i })).toBeTruthy();
  });

  it("falls into a terminal session_lost error state when status polling 404s", async () => {
    mockGetStatus.mockRejectedValue(
      new ApiError("install session not found", 404, "Not Found"),
    );

    await openDialog();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(2100);
    });

    await waitFor(() => {
      expect(
        screen.getByText(
          /Install session expired or was lost\. Scan again to start over\./i,
        ),
      ).toBeTruthy();
    });
    // Terminal — no follow-up poll may be scheduled.
    const callsAfterTerminal = mockGetStatus.mock.calls.length;
    await act(async () => {
      await vi.advanceTimersByTimeAsync(5000);
    });
    expect(mockGetStatus.mock.calls.length).toBe(callsAfterTerminal);
  });

  it("renders the QR after a React StrictMode double-mount (parity with the lark dialog regression)", async () => {
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
    render(<DingTalkAgentBindButton agentId="agent-1" agentName="Bot" />, {
      wrapper: StrictModeWrapper,
    });
    await user.click(screen.getByRole("button", { name: /Bind to DingTalk/i }));

    await waitFor(
      () => {
        expect(screen.getByTestId("qr-code")).toBeTruthy();
      },
      { timeout: 2000 },
    );
    expect(screen.getByTestId("qr-code").getAttribute("data-value")).toBe(
      "https://open-dev.dingtalk.com/fe/app-registration?user_code=MUEU",
    );
  });
});

describe("DingTalkTab (settings panel)", () => {
  beforeEach(resetFixtures);

  it("renders the not-enabled notice when the at-rest key is missing", () => {
    installationsRef.current = {
      installations: [],
      configured: false,
      install_supported: false,
    };
    render(<DingTalkTab />, { wrapper: I18nWrapper });
    expect(screen.getByText(/DingTalk integration not enabled/i)).toBeTruthy();
    expect(screen.getByText(/MULTICA_DINGTALK_SECRET_KEY/)).toBeTruthy();
  });

  it("renders the coming-soon notice when install is unsupported and nothing is installed", () => {
    installationsRef.current = {
      installations: [],
      configured: true,
      install_supported: false,
    };
    render(<DingTalkTab />, { wrapper: I18nWrapper });
    expect(screen.getByText(/DingTalk bot installation coming soon/i)).toBeTruthy();
  });

  it("lists installations by agent identity and disconnects via the API", async () => {
    const user = userEvent.setup();
    agentNameByIdRef.current = new Map([["agent-1", "Patcher"]]);
    installationsRef.current = {
      installations: [activeInstallation],
      configured: true,
      install_supported: true,
    };
    mockDeleteInstallation.mockResolvedValue(undefined);

    render(<DingTalkTab />, { wrapper: I18nWrapper });
    expect(screen.getByText("Patcher")).toBeTruthy();

    await user.click(screen.getByRole("button", { name: /Disconnect/i }));
    // Confirm dialog → the action button carries the same label.
    await user.click(
      screen.getAllByRole("button", { name: /^Disconnect$/i }).at(-1)!,
    );

    await waitFor(() => {
      expect(mockDeleteInstallation).toHaveBeenCalledWith("workspace-1", "inst-1");
      expect(mockInvalidate).toHaveBeenCalled();
      expect(toast.success).toHaveBeenCalled();
    });
  });
});
