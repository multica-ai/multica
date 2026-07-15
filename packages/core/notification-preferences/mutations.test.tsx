/**
 * @vitest-environment jsdom
 */
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";

import { setApiInstance } from "../api";
import type { ApiClient } from "../api/client";
import type {
  NotificationPreferenceResponse,
  NotificationPreferences,
} from "../types";
import { useUpdateNotificationPreferences } from "./mutations";
import { notificationPreferenceKeys } from "./queries";

vi.mock("../hooks", () => ({
  useWorkspaceId: () => "workspace-1",
}));

const WORKSPACE_ID = "workspace-1";

function createWrapper(queryClient: QueryClient) {
  return function Wrapper({ children }: { children: ReactNode }) {
    return (
      <QueryClientProvider client={queryClient}>
        {children}
      </QueryClientProvider>
    );
  };
}

describe("useUpdateNotificationPreferences", () => {
  let queryClient: QueryClient;
  let updateNotificationPreferences: ReturnType<
    typeof vi.fn<
      (
        preferences: NotificationPreferences,
      ) => Promise<NotificationPreferenceResponse>
    >
  >;

  beforeEach(() => {
    queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    updateNotificationPreferences = vi.fn(async (preferences) => ({
      workspace_id: WORKSPACE_ID,
      preferences,
    }));
    setApiInstance({ updateNotificationPreferences } as unknown as ApiClient);
  });

  afterEach(() => {
    queryClient.clear();
    vi.restoreAllMocks();
  });

  it("preserves an existing status mute when another group changes", async () => {
    queryClient.setQueryData(notificationPreferenceKeys.all(WORKSPACE_ID), {
      workspace_id: WORKSPACE_ID,
      preferences: { status_changes: "muted" },
    });
    const { result } = renderHook(
      () => useUpdateNotificationPreferences(),
      { wrapper: createWrapper(queryClient) },
    );

    act(() => {
      result.current.mutate({
        status_changes: "muted",
        comments: "muted",
      });
    });

    await waitFor(() => {
      expect(updateNotificationPreferences).toHaveBeenCalledWith({
        comments: "muted",
      });
    });
  });

  it("sends all explicitly when a muted group is enabled", async () => {
    queryClient.setQueryData(notificationPreferenceKeys.all(WORKSPACE_ID), {
      workspace_id: WORKSPACE_ID,
      preferences: {
        status_changes: "muted",
        comments: "muted",
      },
    });
    const { result } = renderHook(
      () => useUpdateNotificationPreferences(),
      { wrapper: createWrapper(queryClient) },
    );

    act(() => {
      result.current.mutate({ comments: "muted" });
    });

    await waitFor(() => {
      expect(updateNotificationPreferences).toHaveBeenCalledWith({
        status_changes: "all",
      });
    });
  });

  it("derives independent patches for rapid toggles from one render", async () => {
    queryClient.setQueryData(notificationPreferenceKeys.all(WORKSPACE_ID), {
      workspace_id: WORKSPACE_ID,
      preferences: { status_changes: "muted" },
    });
    const { result } = renderHook(
      () => useUpdateNotificationPreferences(),
      { wrapper: createWrapper(queryClient) },
    );

    act(() => {
      result.current.mutate({
        status_changes: "muted",
        comments: "muted",
      });
      result.current.mutate({
        status_changes: "muted",
        updates: "muted",
      });
    });

    await waitFor(() => {
      expect(updateNotificationPreferences).toHaveBeenCalledTimes(2);
    });
    expect(updateNotificationPreferences.mock.calls).toEqual([
      [{ comments: "muted" }],
      [{ updates: "muted" }],
    ]);
  });
});
