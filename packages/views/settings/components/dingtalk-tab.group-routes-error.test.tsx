// @vitest-environment jsdom

import { type ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import { createQueryClient } from "@multica/core/query-client";
import { setApiInstance } from "@multica/core/api";
import type { ApiClient } from "@multica/core/api/client";
import { dingtalkKeys } from "@multica/core/dingtalk";
import enCommon from "../../locales/en/common.json";
import enSettings from "../../locales/en/settings.json";

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "workspace-1" }));

const agentListQueryFn = vi.hoisted(() => vi.fn());
const memberListQueryFn = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/workspace/queries", () => ({
  memberListOptions: () => ({
    queryKey: ["members", "workspace-1"],
    queryFn: memberListQueryFn,
  }),
  agentListOptions: () => ({
    queryKey: ["agents", "workspace-1"],
    queryFn: agentListQueryFn,
  }),
}));

vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({
    getAgentName: () => "Agent One",
    getMemberName: () => "Unknown",
    getSquadName: () => "Unknown Squad",
    getActorName: () => "Unknown",
    getActorInitials: () => "??",
    getActorAvatarUrl: () => null,
  }),
}));

vi.mock("@multica/core/auth", () => {
  const useAuthStore = Object.assign(
    (select?: (state: { user: { id: string } }) => unknown) =>
      select ? select({ user: { id: "user-1" } }) : { user: { id: "user-1" } },
    { getState: () => ({ user: { id: "user-1" } }) },
  );
  return { useAuthStore };
});

vi.mock("../../common/actor-avatar", () => ({
  ActorAvatar: () => <span data-testid="actor-avatar" />,
}));

vi.mock("../../platform", () => ({ openExternal: vi.fn() }));
vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: vi.fn(), message: vi.fn() },
}));

import { DingTalkTab } from "./dingtalk-tab";

const TEST_RESOURCES = { en: { common: enCommon, settings: enSettings } };

afterEach(() => {
  cleanup();
  vi.useRealTimers();
});

beforeEach(() => {
  agentListQueryFn.mockReset();
  agentListQueryFn.mockResolvedValue([
    { id: "agent-1", name: "Agent One", archived_at: null },
  ]);
  memberListQueryFn.mockReset();
  memberListQueryFn.mockResolvedValue([{ user_id: "user-1", role: "owner" }]);
});

function renderUI(children: ReactNode) {
  return render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      {children}
    </I18nProvider>,
  );
}

const ACTIVE_INSTALLATION_RESPONSE = {
  configured: true,
  install_supported: true,
  group_routing_supported: true,
  installations: [
    {
      id: "installation-1",
      agent_id: "agent-1",
      status: "active",
      installed_at: "2026-08-11T00:00:00Z",
    },
  ],
};

const REVOKED_INSTALLATION_RESPONSE = {
  configured: true,
  install_supported: true,
  group_routing_supported: true,
  installations: [
    {
      id: "installation-1",
      agent_id: "agent-1",
      status: "revoked",
      installed_at: "2026-08-11T00:00:00Z",
    },
  ],
};

const ACTIVE_GROUP_ROUTE_RESPONSE = {
  routes: [
    {
      id: "route-1",
      installation_id: "installation-1",
      conversation_id: "cid-platform",
      conversation_title: "Platform team",
      agent_id: "agent-1",
      revision: 1,
      discovered_at: "2026-08-11T00:00:00Z",
      updated_at: "2026-08-11T00:00:00Z",
    },
  ],
};

type InstallationResponse = {
  configured: boolean;
  install_supported: boolean;
  group_routing_supported?: boolean;
  installations: Array<{
    id: string;
    agent_id: string;
    status: string;
    installed_at: string;
  }>;
};

function installApi({
  installations = ACTIVE_INSTALLATION_RESPONSE,
  listDingTalkGroupRoutes = vi.fn().mockResolvedValue(ACTIVE_GROUP_ROUTE_RESPONSE),
}: {
  installations?: InstallationResponse;
  listDingTalkGroupRoutes?: ReturnType<typeof vi.fn>;
} = {}) {
  const listDingTalkInstallations = vi.fn().mockResolvedValue(installations);
  setApiInstance({
    listDingTalkInstallations,
    listDingTalkGroupRoutes,
    deleteDingTalkInstallation: vi.fn(),
    updateDingTalkGroupRoute: vi.fn(),
  } as unknown as ApiClient);
  return { listDingTalkInstallations, listDingTalkGroupRoutes };
}

function renderTab() {
  const queryClient = createQueryClient();
  renderUI(
    <QueryClientProvider client={queryClient}>
      <DingTalkTab />
    </QueryClientProvider>,
  );
  return queryClient;
}

describe("DingTalkTab group-route query failures", () => {
  it("stops five-second polling on persistent failure, hides the empty state, and retries only on demand", async () => {
    vi.useFakeTimers();
    const listDingTalkGroupRoutes = vi.fn().mockRejectedValue(new Error("persistent 500"));
    installApi({ listDingTalkGroupRoutes });
    const queryClient = renderTab();

    await act(async () => {
      await vi.advanceTimersByTimeAsync(1);
    });
    expect(listDingTalkGroupRoutes).toHaveBeenCalledTimes(1);

    // The shared QueryClient permits exactly one automatic retry.
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1_001);
    });
    expect(listDingTalkGroupRoutes).toHaveBeenCalledTimes(2);
    expect(screen.getByText("Could not load group routes")).toBeTruthy();
    expect(screen.queryByText("No groups discovered yet")).toBeNull();

    // Once the query is errored, advancing well beyond repeated five-second
    // intervals must not issue any background requests.
    await act(async () => {
      await vi.advanceTimersByTimeAsync(60_000);
    });
    expect(listDingTalkGroupRoutes).toHaveBeenCalledTimes(2);

    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1_001);
    });
    expect(listDingTalkGroupRoutes).toHaveBeenCalledTimes(4);

    queryClient.clear();
  });

  it("keeps routes visible while Agents load and disables reassignment", async () => {
    vi.useFakeTimers();
    agentListQueryFn.mockImplementation(() => new Promise(() => {}));
    installApi();
    const queryClient = renderTab();

    await act(async () => {
      await vi.advanceTimersByTimeAsync(1);
    });

    expect(screen.getByText("Loading Agents for group routing…")).toBeTruthy();
    expect(screen.getByText("Platform team")).toBeTruthy();
    expect(screen.getByText("cid-platform")).toBeTruthy();
    expect(screen.queryByText("No eligible Agents")).toBeNull();
    expect(screen.getByRole("combobox", { name: "Agent for this group" })).toBeDisabled();
    queryClient.clear();
  });

  it("keeps routes visible on Agent failure and disables reassignment", async () => {
    vi.useFakeTimers();
    agentListQueryFn.mockRejectedValue(new Error("agents unavailable"));
    installApi();
    const queryClient = renderTab();

    await act(async () => {
      await vi.advanceTimersByTimeAsync(1_001);
    });

    expect(screen.getByText("Could not load Agents")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Retry Agents" })).toBeTruthy();
    expect(screen.getByText("Platform team")).toBeTruthy();
    expect(screen.getByText("cid-platform")).toBeTruthy();
    expect(screen.queryByText("No eligible Agents")).toBeNull();
    expect(screen.getByRole("combobox", { name: "Agent for this group" })).toBeDisabled();
    queryClient.clear();
  });

  it("keeps routes visible for a successfully-loaded empty Agent list", async () => {
    vi.useFakeTimers();
    agentListQueryFn.mockResolvedValue([]);
    installApi();
    const queryClient = renderTab();

    await act(async () => {
      await vi.advanceTimersByTimeAsync(1);
    });

    expect(screen.getByText("No eligible Agents")).toBeTruthy();
    expect(screen.getByText("Platform team")).toBeTruthy();
    expect(screen.getByText("cid-platform")).toBeTruthy();
    expect(screen.queryByText("Loading Agents for group routing…")).toBeNull();
    expect(screen.queryByText("Could not load Agents")).toBeNull();
    expect(screen.getByRole("combobox", { name: "Agent for this group" })).toBeDisabled();
    queryClient.clear();
  });

  it("recovers the Agent selector after an explicit retry succeeds", async () => {
    vi.useFakeTimers();
    agentListQueryFn
      .mockRejectedValueOnce(new Error("agents unavailable"))
      .mockRejectedValueOnce(new Error("agents unavailable"))
      .mockResolvedValueOnce([
        { id: "agent-1", name: "Agent One", archived_at: null },
      ]);
    installApi();
    const queryClient = renderTab();

    await act(async () => {
      await vi.advanceTimersByTimeAsync(1_001);
    });
    expect(screen.getByText("Could not load Agents")).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: "Retry Agents" }));
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0);
    });

    expect(screen.queryByText("Could not load Agents")).toBeNull();
    expect(screen.getByRole("combobox", { name: "Agent for this group" })).not.toBeDisabled();
    queryClient.clear();
  });

  it("keeps a route visible when its assigned Agent is archived", async () => {
    vi.useFakeTimers();
    agentListQueryFn.mockResolvedValue([
      { id: "agent-1", name: "Agent One", archived_at: "2026-08-11T00:00:00Z" },
    ]);
    installApi();
    const queryClient = renderTab();

    await act(async () => {
      await vi.advanceTimersByTimeAsync(1);
    });

    expect(screen.getByText("Platform team")).toBeTruthy();
    expect(screen.getAllByText("Agent One").length).toBeGreaterThan(0);
    expect(screen.getByText("No eligible Agents")).toBeTruthy();
    expect(screen.getByRole("combobox", { name: "Agent for this group" })).toBeDisabled();
    queryClient.clear();
  });

  it("keeps a route visible for a member who cannot see its assigned private Agent", async () => {
    vi.useFakeTimers();
    memberListQueryFn.mockResolvedValue([{ user_id: "user-1", role: "member" }]);
    agentListQueryFn.mockResolvedValue([]);
    installApi();
    const queryClient = renderTab();

    await act(async () => {
      await vi.advanceTimersByTimeAsync(1);
    });

    expect(screen.getByText("Platform team")).toBeTruthy();
    expect(screen.getByText("cid-platform")).toBeTruthy();
    expect(screen.getByText("Unknown Agent")).toBeTruthy();
    expect(screen.queryByRole("combobox", { name: "Agent for this group" })).toBeNull();
    queryClient.clear();
  });

  it("treats an older backend without the group-routing capability as unsupported", async () => {
    vi.useFakeTimers();
    const listDingTalkGroupRoutes = vi.fn().mockRejectedValue(new Error("older backend 404"));
    installApi({
      installations: {
        configured: true,
        install_supported: true,
        installations: ACTIVE_INSTALLATION_RESPONSE.installations,
      },
      listDingTalkGroupRoutes,
    });
    const queryClient = renderTab();

    await act(async () => {
      await vi.advanceTimersByTimeAsync(60_000);
    });

    expect(listDingTalkGroupRoutes).not.toHaveBeenCalled();
    expect(screen.queryByText("Could not load group routes")).toBeNull();
    expect(screen.queryByText("No groups discovered yet")).toBeNull();
    expect(screen.queryByText("Group routing")).toBeNull();
    queryClient.clear();
  });

  it("requests group routes when the new backend advertises support", async () => {
    vi.useFakeTimers();
    const listDingTalkGroupRoutes = vi.fn().mockResolvedValue(ACTIVE_GROUP_ROUTE_RESPONSE);
    installApi({ listDingTalkGroupRoutes });
    const queryClient = renderTab();

    await act(async () => {
      await vi.advanceTimersByTimeAsync(1);
    });

    expect(listDingTalkGroupRoutes).toHaveBeenCalledTimes(1);
    expect(screen.getByText("Platform team")).toBeTruthy();
    queryClient.clear();
  });

  it("does not request group routes when every installation is revoked", async () => {
    vi.useFakeTimers();
    const listDingTalkGroupRoutes = vi.fn().mockResolvedValue(ACTIVE_GROUP_ROUTE_RESPONSE);
    installApi({
      installations: REVOKED_INSTALLATION_RESPONSE,
      listDingTalkGroupRoutes,
    });
    const queryClient = renderTab();

    await act(async () => {
      await vi.advanceTimersByTimeAsync(60_000);
    });

    expect(listDingTalkGroupRoutes).not.toHaveBeenCalled();
    expect(screen.queryByText("Group routing")).toBeNull();
    queryClient.clear();
  });

  it("stops group-route polling when the last active installation is revoked", async () => {
    vi.useFakeTimers();
    const listDingTalkGroupRoutes = vi.fn().mockResolvedValue(ACTIVE_GROUP_ROUTE_RESPONSE);
    installApi({ listDingTalkGroupRoutes });
    const queryClient = renderTab();

    await act(async () => {
      await vi.advanceTimersByTimeAsync(0);
    });
    expect(listDingTalkGroupRoutes).toHaveBeenCalledTimes(1);

    await act(async () => {
      queryClient.setQueryData(
        dingtalkKeys.installations("workspace-1"),
        REVOKED_INSTALLATION_RESPONSE,
      );
      await vi.advanceTimersByTimeAsync(60_000);
    });

    expect(listDingTalkGroupRoutes).toHaveBeenCalledTimes(1);
    expect(screen.queryByText("Group routing")).toBeNull();
    queryClient.clear();
  });
});
