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

// useActorName is the workspace-wide identity helper. The Installation
// row uses it to render the Multica agent's name in place of the raw
// Lark app_id. Stubbing it here decouples LarkTab tests from the agent
// list query plumbing.
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

// ActorAvatar pulls in a deep tree (hover cards, presence query, paths).
// In LarkTab tests we only care that the row identifies the correct
// agent — render a tiny stub that surfaces actorId in the DOM so the
// agent-identity assertion can read it directly.
vi.mock("../../common/actor-avatar", () => ({
  ActorAvatar: ({ actorType, actorId }: { actorType: string; actorId: string }) => (
    <span data-testid="actor-avatar" data-actor-type={actorType} data-actor-id={actorId} />
  ),
}));

vi.mock("@multica/core/lark", () => ({
  larkInstallationsOptions: () => ({
    queryKey: ["lark", "installations"],
    queryFn: vi.fn(),
  }),
  larkKeys: { installations: (wsId: string) => ["lark", "installations", wsId] },
}));

vi.mock("@multica/core/api", () => ({
  api: {
    beginLarkInstall: mockBeginInstall,
    getLarkInstallStatus: mockGetStatus,
    deleteLarkInstallation: mockDeleteInstallation,
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

// react-qr-code renders SVG that jsdom doesn't fully support — a stub
// keeps the dialog DOM compact and lets us assert on the surrounding
// chrome (status text, buttons) without QR mechanics.
// Expose the stub as BOTH the named `QRCode` export (what lark-tab now
// imports — see the named-import interop fix) and `default`, so the mock
// stays correct regardless of how the component pulls it in. The stub is
// defined inside the factory because vi.mock is hoisted above any
// top-level variable.
vi.mock("react-qr-code", () => {
  const QrStub = ({ value }: { value: string }) => (
    <span data-testid="qr-code" data-value={value} />
  );
  return { QRCode: QrStub, default: QrStub };
});

vi.mock("./lark-notification-events", () => ({
  LarkNotificationEvents: ({
    installation,
    canManage,
  }: {
    installation: { id: string };
    canManage: boolean;
  }) => (
    <div
      data-testid="lark-notification-events"
      data-installation-id={installation.id}
      data-can-manage={String(canManage)}
    />
  ),
}));

import { LarkAgentBindButton, LarkTab } from "./lark-tab";
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

// StrictMode wrapper used to reproduce the dev-mode mount → unmount →
// remount cycle. React 19 dev runs this on every component, which
// surfaces effect cleanup bugs that don't show in production builds.
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

describe("LarkAgentBindButton (CTA gate)", () => {
  beforeEach(resetFixtures);

  it("shows the Feishu bind CTA but hides the Lark CTA for an owner (MUL-3083)", () => {
    // Mainland Feishu binding stays available; the Lark (international)
    // entry is temporarily hidden via LARK_INTL_CONNECT_ENABLED while its
    // install→inbound pipeline is stabilized (MUL-3083).
    render(<LarkAgentBindButton agentId="agent-1" agentName="Bot" />, {
      wrapper: I18nWrapper,
    });
    expect(screen.getByRole("button", { name: /Bind to Feishu/i })).toBeTruthy();
    expect(screen.queryByRole("button", { name: /Bind to Lark/i })).toBeNull();
  });

  it("shows the Feishu bind CTA but hides the Lark CTA for an admin (MUL-3083)", () => {
    membersRef.current = [{ user_id: "user-1", role: "admin" }];
    render(<LarkAgentBindButton agentId="agent-1" agentName="Bot" />, {
      wrapper: I18nWrapper,
    });
    expect(screen.getByRole("button", { name: /Bind to Feishu/i })).toBeTruthy();
    expect(screen.queryByRole("button", { name: /Bind to Lark/i })).toBeNull();
  });

  it("hides both bind CTAs for a non-admin agent owner (matches backend admin gate)", () => {
    membersRef.current = [{ user_id: "user-1", role: "member" }];
    const { container } = render(
      <LarkAgentBindButton agentId="agent-1" agentName="Bot" />,
      { wrapper: I18nWrapper },
    );
    expect(container.querySelector("button")).toBeNull();
  });

  it("hides both bind CTAs when the device-flow install path is not wired on the server", () => {
    installationsRef.current.install_supported = false;
    const { container } = render(
      <LarkAgentBindButton agentId="agent-1" agentName="Bot" />,
      { wrapper: I18nWrapper },
    );
    expect(container.querySelector("button")).toBeNull();
  });

  it("clicking Bind to Feishu begins an install with region='feishu'", async () => {
    // Pin the routing wire-up: each split CTA must pass its own region
    // string to the API client (which threads it onto the
    // /lark/install/begin?region=… query param), so the device-flow
    // begins on the matching accounts host. A regression here would
    // silently send Lark users to a Feishu QR — the exact bug this
    // refactor addresses.
    const user = userEvent.setup();
    mockBeginInstall.mockResolvedValue({
      session_id: "sess-feishu",
      qr_code_url: "https://accounts.feishu.cn/oauth/v1/device?u=feishu",
      expires_in_seconds: 300,
      poll_interval_seconds: 2,
    });
    mockGetStatus.mockResolvedValue({ status: "pending" });
    render(<LarkAgentBindButton agentId="agent-1" agentName="Bot" />, {
      wrapper: I18nWrapper,
    });
    await user.click(screen.getByRole("button", { name: /Bind to Feishu/i }));
    await waitFor(() => {
      expect(mockBeginInstall).toHaveBeenCalledTimes(1);
    });
    expect(mockBeginInstall).toHaveBeenCalledWith(
      "workspace-1",
      "agent-1",
      "feishu",
    );
  });

  // NOTE (MUL-3083): the "clicking Bind to Lark begins an install with
  // region='lark'" test was removed alongside the temporarily-hidden Lark
  // (international) CTA — there is no Lark button to click while
  // LARK_INTL_CONNECT_ENABLED is false. The Feishu region routing is still
  // pinned by the "clicking Bind to Feishu …" test above; restore the Lark
  // case when the entry is re-enabled.

  it("swaps the bind CTAs for a 'Connected + Manage in Lark' badge when this agent already has an active installation", () => {
    // Anti-zombie guard: re-scanning the same agent upserts the row
    // and orphans the previously-created Lark PersonalAgent. The badge
    // closes the install entry point and links the user to the Bot's
    // dev console page where scopes / display name / additional
    // permissions are actually managed.
    installationsRef.current.installations = [
      {
        id: "inst-1",
        workspace_id: "ws-1",
        agent_id: "agent-1",
        app_id: "cli_existing_app",
        bot_open_id: "ou_existing_bot",
        installer_user_id: "user-1",
        status: "active",
        installed_at: "2026-06-03T00:00:00Z",
        created_at: "2026-06-03T00:00:00Z",
        updated_at: "2026-06-03T00:00:00Z",
      },
    ];
    render(
      <LarkAgentBindButton agentId="agent-1" agentName="Bot" />,
      { wrapper: I18nWrapper },
    );
    // Both Bind CTAs must be gone — re-scanning would orphan the
    // PersonalAgent (see badge comment in lark-tab.tsx).
    expect(screen.queryByRole("button", { name: /Bind to Feishu/i })).toBeNull();
    expect(screen.queryByRole("button", { name: /Bind to Lark/i })).toBeNull();
    // The fixture omits `region`, which the listings DTO defaults to
    // Feishu (mainland). After the #3830 badge restructure the cloud is
    // shown as a "Feishu" chip (not baked into the connected label) and a
    // Disconnect action appears; the region-aware Manage link still points
    // at the mainland host.
    expect(screen.getByText("Feishu")).toBeTruthy();
    expect(screen.getByTestId("lark-agent-bot-disconnect")).toBeTruthy();
    const link = screen.getByRole("link", { name: /Manage in Feishu/i }) as HTMLAnchorElement;
    expect(link.href).toBe("https://open.feishu.cn/app/cli_existing_app");
    expect(link.target).toBe("_blank");
    expect(link.rel).toContain("noopener");
  });

  it("renders region-aware badge text and Manage link for a Lark-international (region=lark) installation", () => {
    // Dual-region: a bot installed against the Lark international cloud
    // must show "Connected to Lark" + "Manage in Lark" copy, with the
    // Manage link pointing at open.larksuite.com (not the Feishu
    // default). Without region-aware copy a user who clicked
    // "Bind to Feishu" and saw "Connected to Lark" would (rightly) be
    // confused — the labels must match the cloud the bot lives on.
    installationsRef.current.installations = [
      {
        id: "inst-lark",
        workspace_id: "ws-1",
        agent_id: "agent-1",
        app_id: "cli_lark_app",
        bot_open_id: "ou_lark_bot",
        installer_user_id: "user-1",
        status: "active",
        region: "lark",
        installed_at: "2026-06-03T00:00:00Z",
        created_at: "2026-06-03T00:00:00Z",
        updated_at: "2026-06-03T00:00:00Z",
      },
    ];
    render(<LarkAgentBindButton agentId="agent-1" agentName="Bot" />, {
      wrapper: I18nWrapper,
    });
    expect(screen.getByText(/Connected to Lark/i)).toBeTruthy();
    const link = screen.getByRole("link", { name: /Manage in Lark/i }) as HTMLAnchorElement;
    expect(link.href).toBe("https://open.larksuite.com/app/cli_lark_app");
  });

  it("shows the Feishu CTA (Lark hidden) for an agent without its own installation, per-agent scoping (MUL-3083)", () => {
    installationsRef.current.installations = [
      {
        id: "inst-other",
        workspace_id: "ws-1",
        agent_id: "agent-other",
        app_id: "cli_other",
        bot_open_id: "ou_other",
        installer_user_id: "user-1",
        status: "active",
        installed_at: "2026-06-03T00:00:00Z",
        created_at: "2026-06-03T00:00:00Z",
        updated_at: "2026-06-03T00:00:00Z",
      },
    ];
    render(<LarkAgentBindButton agentId="agent-1" agentName="Bot" />, {
      wrapper: I18nWrapper,
    });
    expect(screen.getByRole("button", { name: /Bind to Feishu/i })).toBeTruthy();
    expect(screen.queryByRole("button", { name: /Bind to Lark/i })).toBeNull();
  });

  it("keeps the Connected + Manage badge for an already-installed agent even when new installs are unavailable (install_supported=false)", () => {
    // install_supported governs only NEW scan-installs — an already-installed
    // bot stays manageable when the device-flow transport is unwired
    // (server/internal/handler/lark.go: "already-installed bots still appear
    // and remain manageable"). Regression: the install_supported gate used to
    // run before the existing-installation check and hid the bound state.
    installationsRef.current.install_supported = false;
    installationsRef.current.installations = [
      {
        id: "inst-1",
        workspace_id: "ws-1",
        agent_id: "agent-1",
        app_id: "cli_existing_app",
        bot_open_id: "ou_existing_bot",
        installer_user_id: "user-1",
        status: "active",
        installed_at: "2026-06-03T00:00:00Z",
        created_at: "2026-06-03T00:00:00Z",
        updated_at: "2026-06-03T00:00:00Z",
      },
    ];
    render(
      <LarkAgentBindButton agentId="agent-1" agentName="Bot" />,
      { wrapper: I18nWrapper },
    );
    // Both Bind CTAs must be gone even when install_supported=false,
    // since the existing-installation check runs first.
    expect(screen.queryByRole("button", { name: /Bind to Feishu/i })).toBeNull();
    expect(screen.queryByRole("button", { name: /Bind to Lark/i })).toBeNull();
    // Fixture omits region → defaults to Feishu: the cloud shows as a
    // "Feishu" chip (post-#3830 badge restructure), the Disconnect action
    // is present, and the Manage link stays Feishu-aware.
    expect(screen.getByText("Feishu")).toBeTruthy();
    expect(screen.getByTestId("lark-agent-bot-disconnect")).toBeTruthy();
    expect(
      screen.getByRole("link", { name: /Manage in Feishu/i }),
    ).toBeTruthy();
  });

  it("shows the Feishu CTA (Lark hidden) when this agent's only installation is revoked (MUL-3083)", () => {
    installationsRef.current.installations = [
      {
        id: "inst-revoked",
        workspace_id: "ws-1",
        agent_id: "agent-1",
        app_id: "cli_revoked",
        bot_open_id: "ou_revoked",
        installer_user_id: "user-1",
        status: "revoked",
        installed_at: "2026-06-03T00:00:00Z",
        created_at: "2026-06-03T00:00:00Z",
        updated_at: "2026-06-03T00:00:00Z",
      },
    ];
    render(<LarkAgentBindButton agentId="agent-1" agentName="Bot" />, {
      wrapper: I18nWrapper,
    });
    expect(screen.getByRole("button", { name: /Bind to Feishu/i })).toBeTruthy();
    expect(screen.queryByRole("button", { name: /Bind to Lark/i })).toBeNull();
  });
});

// The Connected badge surfaces an Unbind affordance for owners/admins
// (parent gate keeps non-admins out of this component entirely). The
// disconnect path is the recovery handle for the install_supported=false
// re-scan zombie-bot trap and the dual-bot conflict — these tests pin
// the contract: confirm gating, deleteLarkInstallation wiring, cache
// invalidation, and toast feedback on success / failure.
describe("LarkAgentBotConnectedBadge (Unbind / Disconnect)", () => {
  beforeEach(() => {
    resetFixtures();
    installationsRef.current.installations = [
      {
        id: "inst-1",
        workspace_id: "ws-1",
        agent_id: "agent-1",
        app_id: "cli_existing_app",
        bot_open_id: "ou_existing_bot",
        installer_user_id: "user-1",
        status: "active",
        installed_at: "2026-06-03T00:00:00Z",
        created_at: "2026-06-03T00:00:00Z",
        updated_at: "2026-06-03T00:00:00Z",
      },
    ];
  });

  it("renders a Disconnect affordance alongside the Manage link when the agent is bound", () => {
    render(<LarkAgentBindButton agentId="agent-1" agentName="Bot" />, {
      wrapper: I18nWrapper,
    });
    // The badge surfaces three siblings: the green-dot status pill,
    // the Manage link, and the Unbind action. We assert by test-id so
    // we don't trip over /Disconnect/i copy that also appears in the
    // (closed) AlertDialog.
    expect(screen.getByTestId("lark-agent-bot-disconnect")).toBeTruthy();
    // Fixture omits region → Feishu copy.
    expect(screen.getByRole("link", { name: /Manage in Feishu/i })).toBeTruthy();
  });

  it("opens the confirm dialog and does NOT call the API until the user confirms", async () => {
    const user = userEvent.setup();
    render(<LarkAgentBindButton agentId="agent-1" agentName="Bot" />, {
      wrapper: I18nWrapper,
    });
    await user.click(screen.getByTestId("lark-agent-bot-disconnect"));
    // Confirm dialog must mount with the correct copy.
    await waitFor(() => {
      expect(
        screen.getByText(/Disconnect this Lark bot\?/i),
      ).toBeTruthy();
    });
    // Critically: clicking the trigger alone must NOT have deleted the
    // installation — confirmation is mandatory.
    expect(mockDeleteInstallation).not.toHaveBeenCalled();
  });

  it("calls deleteLarkInstallation with (workspaceId, installationId), invalidates the cache, and toasts on confirm", async () => {
    mockDeleteInstallation.mockResolvedValue(undefined);
    const user = userEvent.setup();
    render(<LarkAgentBindButton agentId="agent-1" agentName="Bot" />, {
      wrapper: I18nWrapper,
    });
    await user.click(screen.getByTestId("lark-agent-bot-disconnect"));
    // Wait for the dialog to mount, then click the destructive action
    // (the AlertDialogAction's accessible name is the same "Disconnect"
    // label as the trigger button — but we're now inside the dialog
    // role, so role+name is unambiguous).
    const confirmButton = await screen.findByRole("button", {
      name: /^Disconnect$/i,
    });
    await user.click(confirmButton);

    await waitFor(() => {
      expect(mockDeleteInstallation).toHaveBeenCalledTimes(1);
    });
    expect(mockDeleteInstallation).toHaveBeenCalledWith("workspace-1", "inst-1");
    // Listings cache must be invalidated so the parent re-renders the
    // Bind CTA in place of the now-stale Connected badge.
    expect(mockInvalidate).toHaveBeenCalledWith({
      queryKey: ["lark", "installations", "workspace-1"],
    });
    expect(toast.success).toHaveBeenCalledTimes(1);
  });

  it("toasts an error and keeps the badge mounted when the API call rejects", async () => {
    mockDeleteInstallation.mockRejectedValue(
      new Error("upstream 500"),
    );
    const user = userEvent.setup();
    render(<LarkAgentBindButton agentId="agent-1" agentName="Bot" />, {
      wrapper: I18nWrapper,
    });
    await user.click(screen.getByTestId("lark-agent-bot-disconnect"));
    const confirmButton = await screen.findByRole("button", {
      name: /^Disconnect$/i,
    });
    await user.click(confirmButton);

    await waitFor(() => {
      expect(toast.error).toHaveBeenCalledTimes(1);
    });
    // Cache must NOT be invalidated on failure — invalidating would
    // round-trip a refetch, momentarily flicker the row away even
    // though the install is still active server-side.
    expect(mockInvalidate).not.toHaveBeenCalled();
    // Badge stays mounted so the user can retry.
    expect(screen.getByTestId("lark-agent-bot-connected")).toBeTruthy();
  });

  it("disables the Cancel button while the request is in-flight (prevents racing the close)", async () => {
    let resolveDelete: () => void = () => {};
    mockDeleteInstallation.mockImplementation(
      () =>
        new Promise<void>((resolve) => {
          resolveDelete = resolve;
        }),
    );
    const user = userEvent.setup();
    render(<LarkAgentBindButton agentId="agent-1" agentName="Bot" />, {
      wrapper: I18nWrapper,
    });
    await user.click(screen.getByTestId("lark-agent-bot-disconnect"));
    const confirmButton = await screen.findByRole("button", {
      name: /^Disconnect$/i,
    });
    await user.click(confirmButton);

    // Cancel is disabled while disconnecting — closing mid-flight
    // would orphan the in-flight invalidate + toast.
    const cancel = screen.getByRole("button", { name: /Cancel/i });
    await waitFor(() => {
      expect((cancel as HTMLButtonElement).disabled).toBe(true);
    });

    // Resolve and let the request finish so jsdom doesn't carry a
    // dangling promise into the next test.
    resolveDelete();
    await waitFor(() => {
      expect(toast.success).toHaveBeenCalled();
    });
  });
});

describe("LarkInstallDialog (polling terminal errors)", () => {
  beforeEach(() => {
    resetFixtures();
    vi.useFakeTimers({ shouldAdvanceTime: true });
    mockBeginInstall.mockResolvedValue({
      session_id: "sess-1",
      qr_code_url: "https://accounts.feishu.cn/oauth/v1/device?u=abc",
      expires_in_seconds: 300,
      poll_interval_seconds: 2,
    });
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  async function openDialog() {
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
    render(<LarkAgentBindButton agentId="agent-1" agentName="Bot" />, {
      wrapper: I18nWrapper,
    });
    // The Lark CTA is hidden (MUL-3083); open the dialog via the Feishu CTA
    // — the polling-error behavior under test is region-agnostic.
    await user.click(screen.getByRole("button", { name: /Bind to Feishu/i }));
    // Let the begin-session promise resolve and the QR render.
    await waitFor(() => {
      expect(screen.getByTestId("qr-code")).toBeTruthy();
    });
  }

  it("falls into a terminal session_lost error state when status polling 404s instead of looping forever", async () => {
    mockGetStatus.mockRejectedValue(
      new ApiError("install session not found", 404, "Not Found"),
    );

    await openDialog();
    // Drive the polling timer (intervalMs = max(2000, 2*1000)) and let
    // the rejected promise propagate into the catch.
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
    expect(screen.getByRole("button", { name: /Scan again/i })).toBeTruthy();
    // The dialog renders multiple Close affordances (footer button + the
    // built-in dialog dismiss); we only need to confirm at least one is
    // mounted alongside the retry button.
    expect(screen.getAllByRole("button", { name: /Close/i }).length).toBeGreaterThan(0);
  });

  it("treats 403 as a terminal forbidden error state (no infinite retry on revoked permission)", async () => {
    mockGetStatus.mockRejectedValue(
      new ApiError("forbidden", 403, "Forbidden"),
    );

    await openDialog();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(2100);
    });

    await waitFor(() => {
      expect(
        screen.getByText(
          /You no longer have permission to install Lark Bots in this workspace/i,
        ),
      ).toBeTruthy();
    });
    // Drive another full poll interval — the polling loop must NOT
    // schedule a follow-up fetch after a terminal 4xx.
    const callsAfterTerminal = mockGetStatus.mock.calls.length;
    await act(async () => {
      await vi.advanceTimersByTimeAsync(5000);
    });
    expect(mockGetStatus.mock.calls.length).toBe(callsAfterTerminal);
  });

  // Regression test for the empty-dialog bug Bohan hit on PR #3277:
  // the QR area was completely blank after opening the dialog. React 19
  // StrictMode dev mounts every component twice. The mount/cleanup/mount
  // cycle preserves the component's useRef across the simulated remount,
  // so the cleanup's `closedRef.current = true` survived into the
  // second mount. Both beginSession() promises then saw closedRef=true
  // at the post-await guard and skipped setSession(), leaving the dialog
  // body with no QR, no error, no loading text — just empty. Resetting
  // closedRef.current at the START of the effect re-arms the guard on
  // every mount.
  it("renders the QR after a React StrictMode double-mount (regression for empty dialog body)", async () => {
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
    render(<LarkAgentBindButton agentId="agent-1" agentName="Bot" />, {
      wrapper: StrictModeWrapper,
    });
    // The Lark CTA is hidden (MUL-3083); the StrictMode regression is about
    // the dialog mount cycle, so open it via the Feishu CTA.
    await user.click(screen.getByRole("button", { name: /Bind to Feishu/i }));

    // The QR must appear even though the dialog mounted, unmounted, and
    // mounted again under StrictMode. The previous bug left the body
    // empty here.
    await waitFor(
      () => {
        expect(screen.getByTestId("qr-code")).toBeTruthy();
      },
      { timeout: 2000 },
    );

    // And the QR's value should match what the (latest) begin call
    // returned — not be empty / undefined.
    const qr = screen.getByTestId("qr-code");
    expect(qr.getAttribute("data-value")).toBe(
      "https://accounts.feishu.cn/oauth/v1/device?u=abc",
    );
  });
});

// The Connected bots list used to surface Lark's raw cli_… app_id and
// ou_… bot_open_id, which are meaningless to product users. The row now
// renders the Multica agent's avatar + name (joined via inst.agent_id),
// since the binding is 1:1 with an Agent. These tests pin that identity
// rendering so the row never regresses to leaking the cli_ prefix.
describe("LarkTab connected bots list (agent identity rendering)", () => {
  beforeEach(resetFixtures);

  it("renders the Multica agent's name and avatar instead of the raw Lark app_id / bot_open_id", () => {
    agentNameByIdRef.current = new Map([["agent-1", "Bohan's Helper"]]);
    installationsRef.current.installations = [
      {
        id: "inst-1",
        workspace_id: "ws-1",
        agent_id: "agent-1",
        app_id: "cli_aa941499d4f95cd9",
        bot_open_id: "ou_abc123",
        installer_user_id: "user-1",
        status: "active",
        installed_at: "2026-06-03T00:00:00Z",
        created_at: "2026-06-03T00:00:00Z",
        updated_at: "2026-06-03T00:00:00Z",
      },
    ];

    render(<LarkTab />, { wrapper: I18nWrapper });

    // The agent's display name is the primary identifier.
    expect(screen.getByText("Bohan's Helper")).toBeTruthy();

    // The ActorAvatar stub records the actor it was asked to render —
    // confirms we joined on agent_id (and didn't accidentally pass the
    // bot_open_id or installation id).
    const avatar = screen.getByTestId("actor-avatar");
    expect(avatar.getAttribute("data-actor-type")).toBe("agent");
    expect(avatar.getAttribute("data-actor-id")).toBe("agent-1");

    // The raw Lark IDs are explicitly absent — the row must not leak
    // the cli_ / ou_ prefixes anymore.
    expect(screen.queryByText(/cli_aa941499d4f95cd9/)).toBeNull();
    expect(screen.queryByText(/ou_abc123/)).toBeNull();
  });

  it("falls back to a stable placeholder when the agent has been deleted (so the row is still actionable for cleanup)", () => {
    // Empty map → useActorName.getAgentName returns "Unknown Agent".
    // The row must still render so admins can hit Disconnect.
    installationsRef.current.installations = [
      {
        id: "inst-orphan",
        workspace_id: "ws-1",
        agent_id: "agent-deleted",
        app_id: "cli_orphan",
        bot_open_id: "ou_orphan",
        installer_user_id: "user-1",
        status: "active",
        installed_at: "2026-06-03T00:00:00Z",
        created_at: "2026-06-03T00:00:00Z",
        updated_at: "2026-06-03T00:00:00Z",
      },
    ];

    render(<LarkTab />, { wrapper: I18nWrapper });

    expect(screen.getByText(/Unknown Agent/)).toBeTruthy();
    // Disconnect stays reachable so the orphan row can be cleaned up.
    expect(screen.getByRole("button", { name: /Disconnect/i })).toBeTruthy();
  });
});

describe("LarkTab Runtime Bot visibility", () => {
  beforeEach(resetFixtures);

  it("hides the temporary Runtime Bot settings entry", () => {
    render(<LarkTab />, { wrapper: I18nWrapper });

    expect(screen.queryByText("Runtime Bot")).toBeNull();
    expect(screen.queryByRole("combobox", { name: "Select local runtime" })).toBeNull();
  });
});

describe("LarkTab notification event settings", () => {
  beforeEach(resetFixtures);

  it("renders event settings for an active Notification Bot", () => {
    installationsRef.current.installations = [
      {
        id: "notification-installation-1",
        workspace_id: "workspace-1",
        app_id: "cli_notification",
        bot_open_id: "ou_notification",
        installer_user_id: "user-1",
        status: "active",
        installation_kind: "notification",
        installed_at: "2026-07-13T00:00:00Z",
        created_at: "2026-07-13T00:00:00Z",
        updated_at: "2026-07-13T00:00:00Z",
      },
    ];

    render(<LarkTab />, { wrapper: I18nWrapper });

    expect(screen.getByTestId("lark-notification-events")).toHaveAttribute(
      "data-installation-id",
      "notification-installation-1",
    );
    expect(screen.getByTestId("lark-notification-events")).toHaveAttribute(
      "data-can-manage",
      "true",
    );
  });

  it("passes canManage=false for a regular workspace member", () => {
    membersRef.current = [{ user_id: "user-1", role: "member" }];
    installationsRef.current.installations = [
      {
        id: "notification-installation-1",
        workspace_id: "workspace-1",
        app_id: "cli_notification",
        bot_open_id: "ou_notification",
        installer_user_id: "user-1",
        status: "active",
        installation_kind: "notification",
        installed_at: "2026-07-13T00:00:00Z",
        created_at: "2026-07-13T00:00:00Z",
        updated_at: "2026-07-13T00:00:00Z",
      },
    ];

    render(<LarkTab />, { wrapper: I18nWrapper });

    expect(screen.getByTestId("lark-notification-events")).toHaveAttribute(
      "data-can-manage",
      "false",
    );
  });

  it("does not render event settings without an active Notification Bot", () => {
    installationsRef.current.installations = [
      {
        id: "notification-installation-revoked",
        workspace_id: "workspace-1",
        app_id: "cli_notification",
        bot_open_id: "ou_notification",
        installer_user_id: "user-1",
        status: "revoked",
        installation_kind: "notification",
        installed_at: "2026-07-13T00:00:00Z",
        created_at: "2026-07-13T00:00:00Z",
        updated_at: "2026-07-13T00:00:00Z",
      },
    ];

    render(<LarkTab />, { wrapper: I18nWrapper });

    expect(screen.queryByTestId("lark-notification-events")).toBeNull();
  });
});
