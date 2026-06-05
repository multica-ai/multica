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

import { LarkAgentBindButton, LarkTab } from "./lark-tab";

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

  it("renders the bind CTA when the viewer is a workspace owner and install is supported", () => {
    render(<LarkAgentBindButton agentId="agent-1" agentName="Bot" />, {
      wrapper: I18nWrapper,
    });
    expect(screen.getByRole("button", { name: /Bind to Lark/i })).toBeTruthy();
  });

  it("renders the bind CTA when the viewer is a workspace admin", () => {
    membersRef.current = [{ user_id: "user-1", role: "admin" }];
    render(<LarkAgentBindButton agentId="agent-1" agentName="Bot" />, {
      wrapper: I18nWrapper,
    });
    expect(screen.getByRole("button", { name: /Bind to Lark/i })).toBeTruthy();
  });

  it("hides the bind CTA for a non-admin agent owner (matches backend admin gate)", () => {
    membersRef.current = [{ user_id: "user-1", role: "member" }];
    const { container } = render(
      <LarkAgentBindButton agentId="agent-1" agentName="Bot" />,
      { wrapper: I18nWrapper },
    );
    expect(container.querySelector("button")).toBeNull();
  });

  it("hides the bind CTA when the device-flow install path is not wired on the server", () => {
    installationsRef.current.install_supported = false;
    const { container } = render(
      <LarkAgentBindButton agentId="agent-1" agentName="Bot" />,
      { wrapper: I18nWrapper },
    );
    expect(container.querySelector("button")).toBeNull();
  });

  it("swaps the bind CTA for a 'Connected + Manage in Lark' badge when this agent already has an active installation", () => {
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
    const { container } = render(
      <LarkAgentBindButton agentId="agent-1" agentName="Bot" />,
      { wrapper: I18nWrapper },
    );
    expect(container.querySelector("button")).toBeNull();
    expect(screen.getByText(/Connected to Lark/i)).toBeTruthy();
    const link = screen.getByRole("link", { name: /Manage in Lark/i }) as HTMLAnchorElement;
    expect(link.href).toBe("https://open.feishu.cn/app/cli_existing_app");
    expect(link.target).toBe("_blank");
    expect(link.rel).toContain("noopener");
  });

  it("still shows the bind CTA when an installation exists for a DIFFERENT agent (per-agent scoping)", () => {
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
    expect(screen.getByRole("button", { name: /Bind to Lark/i })).toBeTruthy();
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
    const { container } = render(
      <LarkAgentBindButton agentId="agent-1" agentName="Bot" />,
      { wrapper: I18nWrapper },
    );
    expect(container.querySelector("button")).toBeNull();
    expect(screen.getByText(/Connected to Lark/i)).toBeTruthy();
    expect(
      screen.getByRole("link", { name: /Manage in Lark/i }),
    ).toBeTruthy();
  });

  it("still shows the bind CTA when this agent's only installation is revoked (treat as not-installed for re-bind)", () => {
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
    expect(screen.getByRole("button", { name: /Bind to Lark/i })).toBeTruthy();
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
    await user.click(screen.getByRole("button", { name: /Bind to Lark/i }));
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
    await user.click(screen.getByRole("button", { name: /Bind to Lark/i }));

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
