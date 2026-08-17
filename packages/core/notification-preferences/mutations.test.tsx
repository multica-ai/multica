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
const WORKSPACE_SLUG = "workspace-one";
let activeWorkspaceSlug = WORKSPACE_SLUG;

vi.mock("../paths", () => ({
  useRequiredWorkspaceSlug: () => activeWorkspaceSlug,
}));

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((resolvePromise) => {
    resolve = resolvePromise;
  });
  return { promise, resolve };
}

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
        workspaceSlug?: string,
      ) => Promise<NotificationPreferenceResponse>
    >
  >;

  beforeEach(() => {
    activeWorkspaceSlug = WORKSPACE_SLUG;
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
      expect(updateNotificationPreferences).toHaveBeenCalledWith(
        { comments: "muted" },
        WORKSPACE_SLUG,
      );
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
      expect(updateNotificationPreferences).toHaveBeenCalledWith(
        { status_changes: "all" },
        WORKSPACE_SLUG,
      );
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
      [{ comments: "muted" }, WORKSPACE_SLUG],
      [{ updates: "muted" }, WORKSPACE_SLUG],
    ]);
  });

  it("serializes same-key toggles and keeps queued writes in their workspace", async () => {
    const first = deferred<NotificationPreferenceResponse>();
    const second = deferred<NotificationPreferenceResponse>();
    updateNotificationPreferences
      .mockImplementationOnce(() => first.promise)
      .mockImplementationOnce(() => second.promise);
    queryClient.setQueryData(notificationPreferenceKeys.all(WORKSPACE_ID), {
      workspace_id: WORKSPACE_ID,
      preferences: {},
    });
    const { result, rerender } = renderHook(
      () => useUpdateNotificationPreferences(),
      { wrapper: createWrapper(queryClient) },
    );

    act(() => {
      result.current.mutate({ status_changes: "muted" });
    });
    await waitFor(() => {
      expect(
        queryClient.getQueryData<NotificationPreferenceResponse>(
          notificationPreferenceKeys.all(WORKSPACE_ID),
        )?.preferences,
      ).toEqual({ status_changes: "muted" });
    });

    rerender();
    act(() => {
      result.current.mutate({});
    });

    await waitFor(() => {
      expect(
        queryClient.getQueryData<NotificationPreferenceResponse>(
          notificationPreferenceKeys.all(WORKSPACE_ID),
        )?.preferences,
      ).toEqual({});
    });
    expect(updateNotificationPreferences).toHaveBeenCalledTimes(1);

    activeWorkspaceSlug = "workspace-two";
    rerender();
    first.resolve({
      workspace_id: WORKSPACE_ID,
      preferences: { status_changes: "muted" },
    });
    await waitFor(() => {
      expect(updateNotificationPreferences).toHaveBeenCalledTimes(2);
    });
    expect(updateNotificationPreferences.mock.calls[1]).toEqual([
      { status_changes: "all" },
      WORKSPACE_SLUG,
    ]);

    second.resolve({ workspace_id: WORKSPACE_ID, preferences: {} });
    await waitFor(() => {
      expect(result.current.isPending).toBe(false);
    });
  });

  it("keeps later optimistic patches until the final mutation settles", async () => {
    const first = deferred<NotificationPreferenceResponse>();
    const second = deferred<NotificationPreferenceResponse>();
    updateNotificationPreferences
      .mockImplementationOnce(() => first.promise)
      .mockImplementationOnce(() => second.promise);
    queryClient.setQueryData(notificationPreferenceKeys.all(WORKSPACE_ID), {
      workspace_id: WORKSPACE_ID,
      preferences: {},
    });
    const invalidateQueries = vi.spyOn(queryClient, "invalidateQueries");
    const { result } = renderHook(
      () => useUpdateNotificationPreferences(),
      { wrapper: createWrapper(queryClient) },
    );

    act(() => {
      result.current.mutate({ comments: "muted" });
      result.current.mutate({ updates: "muted" });
    });
    await waitFor(() => {
      expect(
        queryClient.getQueryData<NotificationPreferenceResponse>(
          notificationPreferenceKeys.all(WORKSPACE_ID),
        )?.preferences,
      ).toEqual({ comments: "muted", updates: "muted" });
    });
    expect(updateNotificationPreferences).toHaveBeenCalledTimes(1);

    first.resolve({
      workspace_id: WORKSPACE_ID,
      preferences: { comments: "muted" },
    });
    await waitFor(() => {
      expect(updateNotificationPreferences).toHaveBeenCalledTimes(2);
    });
    expect(invalidateQueries).not.toHaveBeenCalled();
    expect(
      queryClient.getQueryData<NotificationPreferenceResponse>(
        notificationPreferenceKeys.all(WORKSPACE_ID),
      )?.preferences,
    ).toEqual({ comments: "muted", updates: "muted" });

    second.resolve({
      workspace_id: WORKSPACE_ID,
      preferences: { comments: "muted", updates: "muted" },
    });
    await waitFor(() => {
      expect(invalidateQueries).toHaveBeenCalledTimes(1);
    });
  });
});
