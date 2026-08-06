// @vitest-environment jsdom

import { StrictMode, type ReactNode } from "react";
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enSettings from "../../locales/en/settings.json";

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
const mockManualInstall = vi.hoisted(() => vi.fn());
const mockInvalidate = vi.hoisted(() => vi.fn());
const idempotencyKeysRef = vi.hoisted(() => ({ current: [] as string[] }));

type MemberRole = "owner" | "admin" | "member" | "guest";

const membersRef = vi.hoisted(() => ({
  current: [{ user_id: "user-1", role: "owner" as MemberRole }],
}));
const installationsRef = vi.hoisted(() => ({
  current: {
    installations: [] as unknown[],
    configured: true,
    install_supported: true,
    manual_install_supported: true,
  },
}));

vi.mock("@tanstack/react-query", () => ({
  useQuery: (opts: { queryKey: unknown[]; enabled?: boolean }) => {
    if (opts.enabled === false) return { data: undefined, isLoading: false };
    const key = JSON.stringify(opts.queryKey);
    if (key.includes("members")) return { data: membersRef.current, isLoading: false };
    if (key.includes("agents")) return { data: [], isLoading: false };
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
  agentListOptions: () => ({ queryKey: ["agents"], queryFn: vi.fn() }),
}));

vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({
    getAgentName: () => "Test Agent",
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

vi.mock("@multica/core/wecom", () => ({
  wecomInstallationsOptions: () => ({
    queryKey: ["wecom", "installations"],
    queryFn: vi.fn(),
  }),
  wecomKeys: { installations: (wsId: string) => ["wecom", "installations", wsId] },
}));

vi.mock("@multica/core/api", () => ({
  api: {
    beginWecomInstall: (...args: unknown[]) => {
      const key = args[2] as string;
      idempotencyKeysRef.current.push(key);
      return mockBeginInstall(...args);
    },
    getWecomInstallStatus: mockGetStatus,
    manualWecomInstall: mockManualInstall,
    deleteWecomInstallation: vi.fn(),
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

import { WecomAgentBindButton } from "./wecom-tab";

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
  idempotencyKeysRef.current = [];
  membersRef.current = [{ user_id: "user-1", role: "owner" }];
  installationsRef.current = {
    installations: [],
    configured: true,
    install_supported: true,
    manual_install_supported: true,
  };
}

describe("WecomInstallDialog", () => {
  beforeEach(() => {
    cleanup();
    resetFixtures();
    vi.useFakeTimers({ shouldAdvanceTime: true });
    mockBeginInstall.mockResolvedValue({
      session_id: "sess-1",
      status: "creating",
      poll_interval_seconds: 1,
    });
    mockGetStatus.mockResolvedValue({
      status: "pending",
      qr_code_url: "https://work.weixin.qq.com/wework_admin/frame#qr/abc",
      poll_interval_seconds: 2,
    });
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  async function openDialog() {
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
    render(<WecomAgentBindButton agentId="agent-1" agentName="Bot" />, {
      wrapper: I18nWrapper,
    });
    await user.click(screen.getByRole("button", { name: /Bind to WeCom/i }));
  }

  it("reuses the same Idempotency-Key across a React StrictMode double-mount", async () => {
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
    render(<WecomAgentBindButton agentId="agent-1" agentName="Bot" />, {
      wrapper: StrictModeWrapper,
    });
    await user.click(screen.getByRole("button", { name: /Bind to WeCom/i }));

    await waitFor(() => {
      expect(mockBeginInstall).toHaveBeenCalled();
    });

    const uniqueKeys = new Set(idempotencyKeysRef.current);
    expect(uniqueKeys.size).toBe(1);
  });

  it("shows QR only after status transitions from creating to pending", async () => {
    await openDialog();

    expect(mockBeginInstall).toHaveBeenCalled();
    const beginResult = await mockBeginInstall.mock.results[0]?.value;
    expect(beginResult?.status).toBe("creating");
    expect(beginResult).not.toHaveProperty("qr_code_url");

    await waitFor(() => {
      expect(mockGetStatus).toHaveBeenCalledWith("workspace-1", "sess-1");
    });

    await waitFor(() => {
      expect(screen.getAllByTestId("qr-code").length).toBeGreaterThan(0);
    });

    const qr = screen.getAllByTestId("qr-code")[0];
    expect(qr).toBeTruthy();
    expect(qr?.getAttribute("data-value")).toBe(
      "https://work.weixin.qq.com/wework_admin/frame#qr/abc",
    );
  });

  it("falls into session_lost when status polling returns 404", async () => {
    mockGetStatus.mockRejectedValue(
      new ApiError("install session not found", 404, "Not Found"),
    );

    await openDialog();

    await waitFor(() => {
      expect(
        screen.getByText(
          /Install session expired or was lost\. Scan again to start over\./i,
        ),
      ).toBeTruthy();
    });
    expect(screen.getByRole("button", { name: /Scan again/i })).toBeTruthy();
  });

  it("regenerates Idempotency-Key when Scan again is clicked", async () => {
    mockGetStatus.mockRejectedValue(
      new ApiError("install session not found", 404, "Not Found"),
    );

    await openDialog();

    await waitFor(() => {
      expect(screen.getByRole("button", { name: /Scan again/i })).toBeTruthy();
    });

    const firstKey = idempotencyKeysRef.current[0];
    expect(firstKey).toBeTruthy();

    mockBeginInstall.mockResolvedValue({
      session_id: "sess-2",
      status: "creating",
      poll_interval_seconds: 1,
    });
    mockGetStatus.mockResolvedValue({
      status: "pending",
      qr_code_url: "https://work.weixin.qq.com/wework_admin/frame#qr/retry",
      poll_interval_seconds: 2,
    });

    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
    await user.click(screen.getByRole("button", { name: /Scan again/i }));

    await waitFor(() => {
      expect(mockBeginInstall).toHaveBeenCalledTimes(2);
    });

    const secondKey = idempotencyKeysRef.current[1];
    expect(secondKey).toBeTruthy();
    expect(secondKey).not.toBe(firstKey);
  });
});

// The manual path exists so a bot that already lives in WeCom can be connected
// at all: scan-code always provisions a BRAND NEW bot, and it needs a
// provisioned WeCom scan source that self-hosted deployments may not have.
describe("WecomInstallDialog manual entry", () => {
  beforeEach(() => {
    cleanup();
    resetFixtures();
    vi.useFakeTimers({ shouldAdvanceTime: true });
    mockBeginInstall.mockResolvedValue({
      session_id: "sess-1",
      status: "creating",
      poll_interval_seconds: 1,
    });
    mockGetStatus.mockResolvedValue({
      status: "pending",
      qr_code_url: "https://work.weixin.qq.com/wework_admin/frame#qr/abc",
      poll_interval_seconds: 2,
    });
    mockManualInstall.mockResolvedValue({
      installation_id: "inst-1",
      bot_id: "bot-1",
    });
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  function renderBindButton() {
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
    render(<WecomAgentBindButton agentId="agent-1" agentName="Bot" />, {
      wrapper: I18nWrapper,
    });
    return user;
  }

  async function openManualForm() {
    const user = renderBindButton();
    await user.click(screen.getByRole("button", { name: /Bind to WeCom/i }));
    await waitFor(() => {
      expect(screen.getByTestId("wecom-install-switch-manual")).toBeTruthy();
    });
    await user.click(screen.getByTestId("wecom-install-switch-manual"));
    await waitFor(() => {
      expect(screen.getByTestId("wecom-manual-install-form")).toBeTruthy();
    });
    return user;
  }

  it("defaults to scanning and only shows the manual form after opting in", async () => {
    const user = renderBindButton();
    await user.click(screen.getByRole("button", { name: /Bind to WeCom/i }));

    // Scan is the default: begin runs immediately, no form is rendered.
    await waitFor(() => {
      expect(mockBeginInstall).toHaveBeenCalled();
    });
    expect(screen.queryByTestId("wecom-manual-install-form")).toBeNull();

    await user.click(screen.getByTestId("wecom-install-switch-manual"));
    await waitFor(() => {
      expect(screen.getByTestId("wecom-manual-install-form")).toBeTruthy();
    });
  });

  it("stops polling the scan session after switching to manual entry", async () => {
    const user = await openManualForm();
    const callsAtSwitch = mockGetStatus.mock.calls.length;

    // Well past the 2s poll interval the pending status asked for.
    await vi.advanceTimersByTimeAsync(6000);

    expect(mockGetStatus.mock.calls.length).toBe(callsAtSwitch);
    expect(user).toBeTruthy();
  });

  it("submits the trimmed credentials and refreshes the installation list", async () => {
    const user = await openManualForm();

    await user.type(screen.getByLabelText(/Bot ID/i), "  bot-1  ");
    await user.type(screen.getByLabelText(/^Secret$/i), " sec-1 ");
    await user.click(screen.getByRole("button", { name: /Connect bot/i }));

    await waitFor(() => {
      expect(mockManualInstall).toHaveBeenCalledWith(
        "workspace-1",
        "agent-1",
        "bot-1",
        "sec-1",
      );
    });
    await waitFor(() => {
      expect(mockInvalidate).toHaveBeenCalled();
    });
  });

  it("keeps the submit button disabled until both fields are filled", async () => {
    const user = await openManualForm();
    const submit = screen.getByRole("button", { name: /Connect bot/i });
    expect(submit.hasAttribute("disabled")).toBe(true);

    await user.type(screen.getByLabelText(/Bot ID/i), "bot-1");
    expect(screen.getByRole("button", { name: /Connect bot/i }).hasAttribute("disabled")).toBe(
      true,
    );

    await user.type(screen.getByLabelText(/^Secret$/i), "sec-1");
    await waitFor(() => {
      expect(
        screen.getByRole("button", { name: /Connect bot/i }).hasAttribute("disabled"),
      ).toBe(false);
    });
  });

  it("localizes the rejected-credentials code instead of showing the raw error", async () => {
    mockManualInstall.mockRejectedValue(
      new ApiError("invalid_bot_credentials", 400, "Bad Request"),
    );
    const user = await openManualForm();

    await user.type(screen.getByLabelText(/Bot ID/i), "bot-1");
    await user.type(screen.getByLabelText(/^Secret$/i), "wrong");
    await user.click(screen.getByRole("button", { name: /Connect bot/i }));

    await waitFor(() => {
      expect(
        screen.getByText(/WeCom rejected these credentials/i),
      ).toBeTruthy();
    });
    // The stable code is an internal contract, not user-facing copy.
    expect(screen.queryByText("invalid_bot_credentials")).toBeNull();
    // The form stays up so the user can fix the typo.
    expect(screen.getByTestId("wecom-manual-install-form")).toBeTruthy();
  });

  it("distinguishes a bot id taken in this workspace from one taken elsewhere", async () => {
    mockManualInstall.mockRejectedValue(
      new ApiError("bot_id_owned_in_this_workspace", 409, "Conflict"),
    );
    const user = await openManualForm();

    await user.type(screen.getByLabelText(/Bot ID/i), "bot-1");
    await user.type(screen.getByLabelText(/^Secret$/i), "sec-1");
    await user.click(screen.getByRole("button", { name: /Connect bot/i }));

    await waitFor(() => {
      expect(
        screen.getByText(/already connected to another agent in this workspace/i),
      ).toBeTruthy();
    });
    expect(screen.queryByText(/another Multica workspace/i)).toBeNull();
  });

  it("does not report an unreachable WeCom as a bad secret", async () => {
    mockManualInstall.mockRejectedValue(
      new ApiError("verify_unavailable", 502, "Bad Gateway"),
    );
    const user = await openManualForm();

    await user.type(screen.getByLabelText(/Bot ID/i), "bot-1");
    await user.type(screen.getByLabelText(/^Secret$/i), "sec-1");
    await user.click(screen.getByRole("button", { name: /Connect bot/i }));

    await waitFor(() => {
      expect(screen.getByText(/Could not reach WeCom to verify/i)).toBeTruthy();
    });
    expect(screen.queryByText(/WeCom rejected these credentials/i)).toBeNull();
  });

  it("opens straight into manual entry when scan install is unsupported", async () => {
    installationsRef.current = {
      installations: [],
      configured: true,
      install_supported: false,
      manual_install_supported: true,
    };
    const user = renderBindButton();
    await user.click(screen.getByRole("button", { name: /Bind to WeCom/i }));

    await waitFor(() => {
      expect(screen.getByTestId("wecom-manual-install-form")).toBeTruthy();
    });
    // No scan session may be opened against a deployment that cannot scan, and
    // there is no scan view to go back to.
    expect(mockBeginInstall).not.toHaveBeenCalled();
    expect(screen.queryByTestId("wecom-install-switch-scan")).toBeNull();
  });

  it("hides the bind entry point entirely when neither path is supported", async () => {
    installationsRef.current = {
      installations: [],
      configured: true,
      install_supported: false,
      manual_install_supported: false,
    };
    render(<WecomAgentBindButton agentId="agent-1" agentName="Bot" />, {
      wrapper: I18nWrapper,
    });
    expect(screen.queryByTestId("wecom-agent-bind-button")).toBeNull();
  });
});
