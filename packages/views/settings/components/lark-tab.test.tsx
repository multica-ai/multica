import { type ReactNode } from "react";
import { describe, it, expect, beforeEach, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enSettings from "../../locales/en/settings.json";

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
    if (key.includes("members"))
      return { data: membersRef.current, isLoading: false };
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
  ActorAvatar: ({
    actorType,
    actorId,
  }: {
    actorType: string;
    actorId: string;
  }) => (
    <span
      data-testid="actor-avatar"
      data-actor-type={actorType}
      data-actor-id={actorId}
    />
  ),
}));

vi.mock("@multica/core/lark", () => ({
  larkInstallationsOptions: () => ({
    queryKey: ["lark", "installations"],
    queryFn: vi.fn(),
  }),
  larkKeys: {
    installations: (wsId: string) => ["lark", "installations", wsId],
  },
}));

vi.mock("@multica/core/api", () => ({
  api: {
    deleteLarkInstallation: mockDeleteInstallation,
  },
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

import { LarkTab } from "./lark-tab";

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
