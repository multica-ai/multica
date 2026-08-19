// @vitest-environment jsdom

import { type ReactNode } from "react";
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enSettings from "../../locales/en/settings.json";

type MemberRole = "owner" | "admin" | "member" | "guest";

const membersRef = vi.hoisted(() => ({
  current: [{ user_id: "user-1", role: "owner" as MemberRole }],
}));
const installationsRef = vi.hoisted(() => ({
  current: {
    installations: [] as unknown[],
    configured: true,
    install_supported: true,
    group_routing_supported: true,
  },
}));
const groupRoutesRef = vi.hoisted(() => ({
  current: { routes: [] as unknown[] },
}));
const mockDeleteInstallation = vi.hoisted(() => vi.fn());
const mockOpenExternal = vi.hoisted(() => vi.fn());

vi.mock("@tanstack/react-query", () => ({
  useQuery: (opts: { queryKey: unknown[]; enabled?: boolean }) => {
    if (opts.enabled === false) return { data: undefined, isLoading: false };
    const key = JSON.stringify(opts.queryKey);
    if (key.includes("members"))
      return { data: membersRef.current, isLoading: false };
    if (key.includes("group-routes"))
      return { data: groupRoutesRef.current, isLoading: false };
    if (key.includes("installations"))
      return { data: installationsRef.current, isLoading: false };
    return { data: undefined, isLoading: false };
  },
  useQueryClient: () => ({ invalidateQueries: vi.fn() }),
  queryOptions: <T,>(opts: T) => opts,
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "workspace-1" }));

vi.mock("@multica/core/workspace/queries", () => ({
  memberListOptions: () => ({ queryKey: ["members"], queryFn: vi.fn() }),
}));

vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({
    getAgentName: (agentId: string) =>
      agentId === "agent-2" ? "Agent Two" : `Agent ${agentId}`,
    getMemberName: () => "Unknown",
    getSquadName: () => "Unknown Squad",
    getActorName: () => "Unknown",
    getActorInitials: () => "??",
    getActorAvatarUrl: () => null,
  }),
}));

vi.mock("../../common/actor-avatar", () => ({
  ActorAvatar: ({ actorId }: { actorId: string }) => (
    <span data-testid="actor-avatar" data-actor-id={actorId} />
  ),
}));

vi.mock("@multica/core/dingtalk", () => ({
  dingtalkInstallationsOptions: () => ({
    queryKey: ["dingtalk", "installations"],
    queryFn: vi.fn(),
  }),
  dingtalkGroupRoutesOptions: () => ({
    queryKey: ["dingtalk", "group-routes"],
    queryFn: vi.fn(),
  }),
  dingtalkKeys: {
    installations: (wsId: string) => ["dingtalk", "installations", wsId],
    groupRoutes: (wsId: string) => ["dingtalk", "group-routes", wsId],
  },
}));

vi.mock("@multica/core/api", () => ({
  api: {
    deleteDingTalkInstallation: mockDeleteInstallation,
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
  toast: { success: vi.fn(), error: vi.fn(), message: vi.fn() },
}));

vi.mock("../../platform", () => ({ openExternal: mockOpenExternal }));

import { DingTalkTab } from "./dingtalk-tab";

const TEST_RESOURCES = { en: { common: enCommon, settings: enSettings } };

afterEach(cleanup);

function renderUI(children: ReactNode) {
  return render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      {children}
    </I18nProvider>,
  );
}

function resetFixtures() {
  vi.clearAllMocks();
  membersRef.current = [{ user_id: "user-1", role: "owner" }];
  installationsRef.current = {
    installations: [],
    configured: true,
    install_supported: true,
    group_routing_supported: true,
  };
  groupRoutesRef.current = { routes: [] };
}

function setConnectedGroupRoute() {
  installationsRef.current = {
    installations: [{ id: "i1", agent_id: "agent-1", status: "active" }],
    configured: true,
    install_supported: true,
    group_routing_supported: true,
  };
  groupRoutesRef.current = {
    routes: [
      {
        id: "route-1",
        installation_id: "i1",
        conversation_id: "cid-platform",
        conversation_title: "Platform team",
        agent_id: "agent-2",
      },
    ],
  };
}

describe("DingTalkTab", () => {
  beforeEach(resetFixtures);

  it("surfaces the not-enabled notice when the deployment has no DingTalk key", () => {
    installationsRef.current = {
      installations: [],
      configured: false,
      install_supported: false,
      group_routing_supported: false,
    };
    renderUI(<DingTalkTab />);
    expect(screen.getByText(/DingTalk integration not enabled/i)).toBeTruthy();
  });

  it("shows the empty state when configured but nothing is connected", () => {
    renderUI(<DingTalkTab />);
    expect(screen.getByText(/No bots connected yet/i)).toBeTruthy();
  });

  it("lists a connected installation with its agent name and a disconnect control", () => {
    installationsRef.current = {
      installations: [{ id: "i1", agent_id: "agent-7", status: "active" }],
      configured: true,
      install_supported: true,
      group_routing_supported: true,
    };
    renderUI(<DingTalkTab />);
    expect(screen.getByText("Agent agent-7")).toBeTruthy();
    expect(screen.getByText(/Disconnect/i)).toBeTruthy();
  });

  it("shows a placeholder instead of 'Invalid Date' when installed_at is missing or malformed", () => {
    installationsRef.current = {
      installations: [
        { id: "i1", agent_id: "agent-7", status: "active", installed_at: "" },
        {
          id: "i2",
          agent_id: "agent-8",
          status: "active",
          installed_at: "not-a-date",
        },
      ],
      configured: true,
      install_supported: true,
      group_routing_supported: true,
    };
    renderUI(<DingTalkTab />);
    expect(screen.queryByText(/Invalid Date/i)).toBeNull();
  });

  it("lists a discovered group with its fixed Agent", () => {
    setConnectedGroupRoute();
    renderUI(<DingTalkTab />);
    expect(screen.getByText("Platform team")).toBeTruthy();
    expect(screen.getByText("cid-platform")).toBeTruthy();
    expect(screen.getByText("Agent Two")).toBeTruthy();
  });

  it("renders retained group routing read-only without an Agent writer", () => {
    setConnectedGroupRoute();
    renderUI(<DingTalkTab />);

    expect(screen.getByText("Agent Two")).toBeTruthy();
    expect(
      screen.queryByRole("combobox", { name: "Agent for this group" }),
    ).toBeNull();
  });

});
