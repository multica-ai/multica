// @vitest-environment jsdom

import { type ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import { createQueryClient } from "@multica/core/query-client";
import { setApiInstance } from "@multica/core/api";
import type { ApiClient } from "@multica/core/api/client";
import enCommon from "../../locales/en/common.json";
import enSettings from "../../locales/en/settings.json";

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "workspace-1" }));

vi.mock("@multica/core/workspace/queries", () => ({
  memberListOptions: () => ({
    queryKey: ["members", "workspace-1"],
    queryFn: async () => [{ user_id: "user-1", role: "owner" }],
  }),
  agentListOptions: () => ({
    queryKey: ["agents", "workspace-1"],
    queryFn: async () => [{ id: "agent-1", name: "Agent One", archived_at: null }],
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

function renderUI(children: ReactNode) {
  return render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      {children}
    </I18nProvider>,
  );
}

describe("DingTalkTab group-route query failures", () => {
  it("stops five-second polling on persistent failure, hides the empty state, and retries only on demand", async () => {
    vi.useFakeTimers();
    const listDingTalkGroupRoutes = vi.fn().mockRejectedValue(new Error("persistent 500"));
    setApiInstance({
      listDingTalkInstallations: vi.fn().mockResolvedValue({
        configured: true,
        install_supported: true,
        installations: [
          {
            id: "installation-1",
            agent_id: "agent-1",
            status: "active",
            installed_at: "2026-08-11T00:00:00Z",
          },
        ],
      }),
      listDingTalkGroupRoutes,
      deleteDingTalkInstallation: vi.fn(),
      updateDingTalkGroupRoute: vi.fn(),
    } as unknown as ApiClient);

    const queryClient = createQueryClient();
    renderUI(
      <QueryClientProvider client={queryClient}>
        <DingTalkTab />
      </QueryClientProvider>,
    );

    await act(async () => {
      await vi.advanceTimersByTimeAsync(0);
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
});
